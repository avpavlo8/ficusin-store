package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/jackc/pgx/v5"
)

type existingOrderEditLine struct {
	id       int64
	sku      string
	quantity int
}

// Match checkout's sorted stock lock order before taking realization locks.
func lockOrderEditStock(ctx context.Context, tx pgx.Tx, orderID int64, requested []OrderEditLine) error {
	skus := make([]string, 0, len(requested))
	for _, line := range requested {
		skus = append(skus, line.identity())
	}
	rows, err := tx.Query(ctx, `SELECT variant_id FROM order_items WHERE order_id=$1 AND variant_id IS NOT NULL UNION SELECT id FROM product_variants WHERE sku=ANY($2::TEXT[]) ORDER BY variant_id`, orderID, skus)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if id > 0 {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-stock:'||$1::BIGINT::TEXT,0))`, id); err != nil {
			return err
		}
	}
	return nil
}

// Replace only the changed live composition. Frozen realization snapshots keep
// removed IDs in their audit; retained rows keep their actual IDs and agreed
// price/name/packing, rather than being deleted and reconstructed from catalogue.
func replaceOrderEditLines(ctx context.Context, tx pgx.Tx, orderID int64, customerID *int64, feePending bool, requested []OrderEditLine) error {
	rows, err := tx.Query(ctx, `SELECT id,COALESCE(sku,''),quantity FROM order_items WHERE order_id=$1 ORDER BY id FOR UPDATE`, orderID)
	if err != nil {
		return err
	}
	existing := []existingOrderEditLine{}
	bySKU := map[string][]existingOrderEditLine{}
	for rows.Next() {
		var row existingOrderEditLine
		if err = rows.Scan(&row.id, &row.sku, &row.quantity); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, row)
		bySKU[row.sku] = append(bySKU[row.sku], row)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if err = order.RecordMovement(ctx, tx, orderID, order.MovementRelease); err != nil {
		return err
	}
	if err = releaseOrderReservationsForEdit(ctx, tx, orderID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE procurement_requests SET status='cancelled',updated_at=CURRENT_TIMESTAMP WHERE customer_order_id=$1 AND kind='customer_order' AND status='open'`, orderID); err != nil {
		return err
	}
	discountBPS := 0
	if customerID != nil {
		if err = tx.QueryRow(ctx, `SELECT COALESCE(retail_discount_bps,0) FROM customers WHERE id=$1`, *customerID).Scan(&discountBPS); err != nil {
			return err
		}
	}
	keep := map[int64]bool{}
	hasPreorder := false
	for _, want := range requested {
		var line editableOrderLine
		var priceMinor int64
		var packing []byte
		err = tx.QueryRow(ctx, `SELECT pv.sku,pv.id,p.id,p.name,pv.label,pv.base_price_minor,COALESCE(pv.saby_id,''),
   variant_numeric_attribute(pv.id,'height_cm')::INTEGER,variant_numeric_attribute(pv.id,'pot_diameter_cm')::INTEGER,
   jsonb_build_object('packageLengthCm',COALESCE(shipping_package_dimension(pv.id,'package_length_cm'),0)::INTEGER,
    'packageWidthCm',COALESCE(shipping_package_dimension(pv.id,'package_width_cm'),0)::INTEGER,
    'packageHeightCm',COALESCE(shipping_package_dimension(pv.id,'package_height_cm'),0)::INTEGER,
    'packageWeightGrams',COALESCE(variant_numeric_attribute(pv.id,'package_weight_grams'),0)::INTEGER)
   FROM product_variants pv JOIN products p ON p.id=pv.product_id
   WHERE pv.sku=$1 AND pv.is_active=1 AND pv.archived_at IS NULL AND p.status='published' LIMIT 1`, want.identity()).Scan(&line.sku, &line.variantID, &line.productID, &line.name, &line.variantLabel, &priceMinor, &line.sabyID, &line.heightCM, &line.potDiameterCM, &packing)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("товар %s больше не доступен", want.identity())
		}
		if err != nil {
			return err
		}
		_, preorder, err := reserveVariantForEdit(ctx, tx, orderID, line.variantID, want.Quantity)
		if err != nil {
			return err
		}
		hasPreorder = hasPreorder || preorder
		previous := bySKU[line.sku]
		if len(previous) > 0 {
			remaining := want.Quantity
			for i, old := range previous {
				quantity := min(old.quantity, remaining)
				if i == len(previous)-1 {
					quantity = remaining
				}
				if quantity <= 0 {
					continue
				}
				tag, err := tx.Exec(ctx, `UPDATE order_items SET quantity=$3,is_preorder=$4,reserved_qty=0 WHERE id=$1 AND order_id=$2`, old.id, orderID, quantity, boolToSmallInt(preorder))
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return ErrInvalidInput
				}
				keep[old.id] = true
				remaining -= quantity
			}
		} else {
			if discountBPS > 0 {
				priceMinor = (priceMinor*int64(10000-min(9000, discountBPS)) + 5000) / 10000
			}
			if _, err = tx.Exec(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty)
    VALUES($1,$2,$3,$4,$5,$6,jsonb_strip_nulls(jsonb_build_object('heightCm',$7::INTEGER,'potDiameterCm',$8::INTEGER))||$13::JSONB,$9::NUMERIC/100,$10,$11,$12)`, orderID, line.productID, line.variantID, line.sku, line.name, line.variantLabel, line.heightCM, line.potDiameterCM, priceMinor, want.Quantity, boolToSmallInt(preorder), 0, packing); err != nil {
				return err
			}
		}
		if preorder {
			if _, err = tx.Exec(ctx, `INSERT INTO procurement_requests(kind,saby_id,requested_name,quantity,customer_order_id,status,notes)VALUES('customer_order',NULLIF($1,''),$2,$3,$4,'open','Предзаказ после изменения заказа')`, line.sabyID, line.name, want.Quantity, orderID); err != nil {
				return err
			}
		}
	}
	for _, old := range existing {
		if keep[old.id] {
			continue
		}
		tag, err := tx.Exec(ctx, `DELETE FROM order_items WHERE id=$1 AND order_id=$2`, old.id, orderID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrInvalidInput
		}
	}
	if err = order.RecordMovement(ctx, tx, orderID, order.MovementReserve); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET has_preorder=$2,delivery_fee_pending=$3,stock_released_at=NULL WHERE id=$1`, orderID, boolToSmallInt(hasPreorder), boolToSmallInt(feePending))
	return err
}
