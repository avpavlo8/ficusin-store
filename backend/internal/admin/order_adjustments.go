package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/jackc/pgx/v5"
)

type OrderEditLine struct {
	// SKU опознаёт строку заказа. Продаётся конкретный размер, поэтому одна
	// карточка товара ещё не говорит, что именно уходит покупателю.
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type OrderEdit struct {
	RecheckAvailability bool `json:"recheckAvailability"`
	// Items is the complete desired composition. nil means "do not change
	// composition"; an explicitly empty array is rejected because an empty
	// order must be cancelled instead of becoming a zero-ruble ghost.
	Items       *[]OrderEditLine `json:"items"`
	DeliveryFee *float64         `json:"deliveryFee"`
	Discount    *OrderDiscount   `json:"discount"`
}

type editableOrderLine struct {
	sku           string
	variantID     int64
	productID     int64
	name          string
	variantLabel  string
	heightCM      *int
	potDiameterCM *int
	sabyID        string
}

func normalizeOrderEditLines(lines []OrderEditLine) ([]OrderEditLine, error) {
	result := make([]OrderEditLine, 0, len(lines))
	positions := map[string]int{}
	for _, line := range lines {
		sku := strings.TrimSpace(line.SKU)
		if sku == "" || line.Quantity <= 0 || line.Quantity > 100 {
			return nil, fmt.Errorf("некорректный товар или количество")
		}
		if index, ok := positions[sku]; ok {
			result[index].Quantity += line.Quantity
			if result[index].Quantity > 100 {
				return nil, fmt.Errorf("слишком большое количество товара")
			}
			continue
		}
		positions[sku] = len(result)
		result = append(result, OrderEditLine{SKU: sku, Quantity: line.Quantity})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("заказ не может быть пустым — отмените его")
	}
	return result, nil
}

func releaseOrderReservationsForEdit(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, `
		SELECT i.id FROM inventory i
		WHERE i.variant_id IN (SELECT variant_id FROM order_items WHERE order_id=$1 AND variant_id IS NOT NULL)
		ORDER BY i.id FOR UPDATE
	`, orderID); err != nil {
		return fmt.Errorf("lock inventory for order edit: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		WITH wanted AS (
			SELECT variant_id,SUM(reserved_qty)::INTEGER AS quantity
			FROM order_items WHERE order_id=$1 AND variant_id IS NOT NULL GROUP BY variant_id
		), allocation AS (
			SELECT i.id,LEAST(i.reserved_qty,GREATEST(w.quantity-COALESCE(SUM(i.reserved_qty) OVER(
				PARTITION BY i.variant_id ORDER BY i.id ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
			),0),0)) AS give_back
			FROM inventory i JOIN wanted w ON w.variant_id=i.variant_id
		)
		UPDATE inventory i SET reserved_qty=i.reserved_qty-a.give_back
		FROM allocation a WHERE a.id=i.id AND a.give_back>0
	`, orderID); err != nil {
		return fmt.Errorf("release inventory for order edit: %w", err)
	}
	return nil
}

func needsPreorderForEdit(ctx context.Context, tx pgx.Tx, orderID, variantID int64, quantity int) (bool, error) {
	// The availability snapshot decides whether manager review is needed.
	// Use confirmed Saby stock: other warehouses and stale local values cannot
	// turn a request into an immediately payable order. No reservation is made.
	var available int
	var fresh bool
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(GREATEST(i.available_qty-i.reserved_qty+order_sale_owned_quantity($2,i.warehouse_id,i.variant_id),0)),0)::INTEGER, COUNT(*)>0
		FROM order_sale_inventory i
		JOIN warehouses w ON w.id=i.warehouse_id AND w.saby_id='saby-ryazan-main' AND w.is_active=1
		JOIN product_variants v ON v.id=i.variant_id
		JOIN products p ON p.id=v.product_id AND 'stock'=ANY(p.saby_fields)
		JOIN saby_nomenclature n ON n.saby_id=v.saby_id AND n.missing_since IS NULL
		WHERE i.variant_id=$1 AND i.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours'
	`, variantID, orderID).Scan(&available, &fresh); err != nil {
		return false, err
	}
	return !fresh || available < quantity, nil
}

func currentAdminOrder(ctx context.Context, repository *PostgresRepository, id int64) (Order, error) {
	var item Order
	err := repository.pool.QueryRow(ctx, `
		SELECT id, order_number, customer_id, customer_name, phone, email, address,
			comment, delivery_method, delivery_fee_pending = 1,
			delivery_repack_requested = 1, payment_method, payment_status,
			COALESCE(cdek_track_number, ''), has_preorder = 1, status,
			total::DOUBLE PRECISION, created_at
		FROM orders WHERE id=$1
	`, id).Scan(&item.ID, &item.OrderNumber, &item.CustomerID, &item.CustomerName, &item.Phone,
		&item.Email, &item.Address, &item.Comment, &item.DeliveryMethod, &item.DeliveryFeePending,
		&item.RepackRequested, &item.PaymentMethod, &item.PaymentStatus, &item.TrackNumber,
		&item.HasPreorder, &item.Status, &item.Total, &item.CreatedAt)
	if err != nil {
		return Order{}, err
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT id,COALESCE(product_id,0),COALESCE((SELECT product_code::TEXT FROM products WHERE id=order_items.product_id AND status='published'),''),COALESCE(sku,''),COALESCE(variant_label,''),
			product_name,unit_price::DOUBLE PRECISION,quantity
		FROM order_items WHERE order_id=$1 ORDER BY id
	`, id)
	if err != nil {
		return Order{}, err
	}
	defer rows.Close()
	item.Items = []OrderItem{}
	for rows.Next() {
		var line OrderItem
		if err := rows.Scan(&line.ID, &line.ProductID, &line.ProductCode, &line.SKU, &line.VariantLabel, &line.ProductName, &line.UnitPrice, &line.Quantity); err != nil {
			return Order{}, err
		}
		item.Items = append(item.Items, line)
	}
	if err := rows.Err(); err != nil {
		return Order{}, err
	}
	rows.Close()
	pointers := make([]*OrderItem, len(item.Items))
	for i := range item.Items {
		pointers[i] = &item.Items[i]
	}
	if err := repository.hydrateOrderItemMedia(ctx, pointers); err != nil {
		return Order{}, err
	}
	if err := repository.hydrateOrderWorkflows(ctx, []*Order{&item}); err != nil {
		return Order{}, err
	}
	return item, nil
}

// EditOrder replaces the requested composition transactionally. Existing
// lines keep the price agreed when the order was placed; newly added lines
// use today's storefront price and the customer's current retail discount.
// Historical legacy reservations are released. A creation-time realization
// keeps its document identity and receives an audited durable revision of the
// final composition and net goods prices in the same transaction.
func (repository *PostgresRepository) EditOrder(ctx context.Context, actor Actor, id int64, edit OrderEdit) (Order, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return Order{}, ErrForbidden
	}
	if edit.RecheckAvailability && (edit.Items != nil || edit.DeliveryFee != nil || edit.Discount != nil) {
		return Order{}, errors.New("проверка наличия не должна менять состав, скидку или доставку")
	}
	if edit.DeliveryFee != nil && (*edit.DeliveryFee < 0 || *edit.DeliveryFee > 100000) {
		return Order{}, fmt.Errorf("стоимость доставки вне разумных пределов")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status, deliveryMethod string
	var customerID *int64
	var oldDeliveryFee float64
	var oldFeePending bool
	if err := tx.QueryRow(ctx, `SELECT status,delivery_method,customer_id,delivery_fee::DOUBLE PRECISION,delivery_fee_pending=1 FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&status, &deliveryMethod, &customerID, &oldDeliveryFee, &oldFeePending); err != nil {
		return Order{}, err
	}
	// A checkout application already contains this composition and delivery fee.
	// Do not silently leave the carrier with a different order snapshot.
	var activeCDEK bool
	if err := tx.QueryRow(ctx, `SELECT (cdek_uuid<>'' AND cdek_cancel_state<>'cancelled') OR cdek_create_state IN ('creating','unknown') FROM orders WHERE id=$1`, id).Scan(&activeCDEK); err != nil {
		return Order{}, err
	}
	var packingActive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_packing_workflows WHERE order_id=$1 AND state NOT IN ('cancelled','stale'))`, id).Scan(&packingActive); err != nil {
		return Order{}, err
	}
	if packingActive {
		return Order{}, errors.New("упаковка уже подтверждена и заказ передан в обработку; состав и доставка зафиксированы")
	}
	if activeCDEK {
		return Order{}, fmt.Errorf("заявка СДЭК уже создаётся или зарегистрирована: сначала сверьте и отмените её у перевозчика перед изменением состава или доставки")
	}
	if status == "cancelled" || status == "completed" || status == "shipped" {
		return Order{}, fmt.Errorf("состав уже закрытого или отправленного заказа менять нельзя")
	}
	if err := filterUnchangedOrderEditFields(ctx, tx, id, &edit); err != nil {
		return Order{}, err
	}
	if edit.RecheckAvailability {
		rows, err := tx.Query(ctx, `SELECT id,variant_id,quantity,reserved_qty FROM order_items WHERE order_id=$1 ORDER BY id FOR UPDATE`, id)
		if err != nil {
			return Order{}, err
		}
		type stockLine struct {
			id                 int64
			variant            *int64
			quantity, reserved int
		}
		var lines []stockLine
		for rows.Next() {
			var line stockLine
			if err := rows.Scan(&line.id, &line.variant, &line.quantity, &line.reserved); err != nil {
				rows.Close()
				return Order{}, err
			}
			lines = append(lines, line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return Order{}, err
		}
		if len(lines) == 0 {
			return Order{}, errors.New("заказ не содержит товаров")
		}
		hasPreorder := false
		for _, line := range lines {
			preorder := true
			if line.variant != nil {
				needed := line.quantity - line.reserved
				if needed < 0 {
					needed = 0
				}
				preorder, err = needsPreorderForEdit(ctx, tx, id, *line.variant, needed)
				if err != nil {
					return Order{}, err
				}
			}
			hasPreorder = hasPreorder || preorder
			if _, err = tx.Exec(ctx, `UPDATE order_items SET is_preorder=$2 WHERE id=$1`, line.id, boolToSmallInt(preorder)); err != nil {
				return Order{}, err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE orders SET has_preorder=$2 WHERE id=$1`, id, boolToSmallInt(hasPreorder)); err != nil {
			return Order{}, err
		}
		if !hasPreorder {
			if _, err = tx.Exec(ctx, `UPDATE procurement_requests SET status='cancelled',updated_at=CURRENT_TIMESTAMP WHERE customer_order_id=$1 AND kind='customer_order' AND status='open'`, id); err != nil {
				return Order{}, err
			}
		}
		if err = insertAudit(ctx, tx, actor, "order.availability.recheck", "order", fmt.Sprint(id), nil, map[string]any{"hasPreorder": hasPreorder}); err != nil {
			return Order{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Order{}, err
		}
		return currentAdminOrder(ctx, repository, id)
	}
	commercialEdit := edit.Items != nil || edit.Discount != nil
	if commercialEdit {
		// Partial shipments have their own carrier/packing lifecycle and need
		// not have a whole-order packing workflow. Their immutable item rows
		// cannot disappear through order_items' ON DELETE CASCADE relation.
		var activePartial bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shipment_offers WHERE order_id=$1 AND (status IN ('payment_pending','paid','shipping','shipped','ready','completed') OR cdek_create_state IN ('creating','unknown') OR (COALESCE(cdek_uuid,'')<>'' AND UPPER(COALESCE(cdek_status,''))<>'REMOVED')))`, id).Scan(&activePartial); err != nil {
			return Order{}, err
		}
		if activePartial {
			return Order{}, fmt.Errorf("%w: состав и скидка зафиксированы активной частичной отправкой; сначала сверьте её оплату и передачу перевозчику", ErrInvalidInput)
		}
	}
	var requestedLines []OrderEditLine
	if edit.Items != nil {
		requestedLines, err = normalizeOrderEditLines(*edit.Items)
		if err != nil {
			return Order{}, err
		}
		if err = lockOrderEditStock(ctx, tx, id, requestedLines); err != nil {
			return Order{}, err
		}
	}
	if commercialEdit {
		if err := ordersale.BeforeOrderEdit(ctx, tx, id); err != nil {
			return Order{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE shipment_offers SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE order_id=$1 AND status IN ('draft','packaging_required','notifying','offered')`, id); err != nil {
		return Order{}, err
	}

	var before map[string]any
	if err := tx.QueryRow(ctx, `SELECT jsonb_build_object('subtotal',subtotal,'deliveryFee',delivery_fee,'total',total,'hasPreorder',has_preorder,'feePending',delivery_fee_pending,'discountType',manager_discount_type,'discountValue',manager_discount_value,'discountAmount',manager_discount_amount) FROM orders WHERE id=$1`, id).Scan(&before); err != nil {
		return Order{}, err
	}

	if edit.Items != nil {
		if err = replaceOrderEditLines(ctx, tx, id, customerID, oldFeePending, requestedLines); err != nil {
			return Order{}, err
		}
	}

	if edit.DeliveryFee != nil {
		if _, err := tx.Exec(ctx, `UPDATE orders SET delivery_fee=$2,delivery_fee_pending=0,delivery_repack_requested=0 WHERE id=$1`, id, *edit.DeliveryFee); err != nil {
			return Order{}, err
		}
	}
	var gross int64
	var discount OrderDiscount
	if err := tx.QueryRow(ctx, `SELECT ROUND(COALESCE((SELECT SUM(unit_price*quantity) FROM order_items WHERE order_id=$1),0)*100)::BIGINT,manager_discount_type,manager_discount_value::double precision FROM orders WHERE id=$1`, id).Scan(&gross, &discount.Type, &discount.Value); err != nil {
		return Order{}, err
	}
	discountChanged := edit.Discount != nil && (discount.Type != edit.Discount.Type || discount.Value != edit.Discount.Value)
	if edit.Discount != nil {
		discount = *edit.Discount
	}
	amount, err := discountMinor(gross, discount)
	if err != nil {
		return Order{}, err
	}
	if discountChanged {
		var legacy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE order_id=$1 AND status IN ('pending','superseded') AND (provider_payment_id<>'' OR request_payload IS NOT NULL) AND COALESCE(request_payload->'Metadata'->>'ficusin_capture_mode',request_payload->'metadata'->>'ficusin_capture_mode','')<>'two_stage')`, id).Scan(&legacy); err != nil {
			return Order{}, err
		}
		if legacy {
			return Order{}, errors.New("сначала сверьте действующую платёжную ссылку старого типа перед изменением скидки")
		}
		var paidOrPartial bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE order_id=$1 AND status='paid') OR EXISTS(SELECT 1 FROM shipment_offers WHERE order_id=$1 AND status NOT IN ('draft','packaging_required','stale','cancelled','expired'))`, id).Scan(&paidOrPartial); err != nil {
			return Order{}, err
		}
		if paidOrPartial {
			return Order{}, errors.New("скидка доступна до оплаты и отправки заказа")
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET subtotal=$2::numeric/100,total=$2::numeric/100+CASE WHEN delivery_payee='shop' THEN delivery_fee ELSE 0 END,manager_discount_type=$3,manager_discount_value=$4,manager_discount_amount=$5::numeric/100,shipment_revision=shipment_revision+1 WHERE id=$1`, id, gross-amount, discount.Type, discount.Value, amount); err != nil {
		return Order{}, err
	}

	if commercialEdit {
		if err := ordersale.QueueRevision(ctx, tx, id); err != nil {
			return Order{}, err
		}
	}

	var after map[string]any
	if err := tx.QueryRow(ctx, `SELECT jsonb_build_object('subtotal',subtotal,'deliveryFee',delivery_fee,'total',total,'hasPreorder',has_preorder,'feePending',delivery_fee_pending,'discountType',manager_discount_type,'discountValue',manager_discount_value,'discountAmount',manager_discount_amount) FROM orders WHERE id=$1`, id).Scan(&after); err != nil {
		return Order{}, err
	}
	if err := insertAudit(ctx, tx, actor, "order.contents.update", "order", fmt.Sprint(id), before, after); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return currentAdminOrder(ctx, repository, id)
}
