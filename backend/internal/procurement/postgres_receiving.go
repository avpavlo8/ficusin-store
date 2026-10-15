package procurement

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The invoice row owns purchase totals and logistics. Its receipt-only children
// own catalog identity, prices and stock movements; they never train an alias.
func updateReceivingLine(ctx context.Context, tx pgx.Tx, actor Actor, lineID, orderID int64, input OrderLineUpdate) error {
	var mode string
	var quantity int
	var excluded bool
	if err := tx.QueryRow(ctx, `SELECT receiving_mode,COALESCE(invoiced_qty,0),invoice_excluded FROM procurement_order_lines WHERE id=$1`, lineID).Scan(&mode, &quantity, &excluded); err != nil {
		return err
	}
	if excluded || quantity <= 0 {
		return &UserFacingError{Message: "Сначала сопоставьте строку с инвойсом и проверьте количество."}
	}
	if input.CustomerHandedOver != nil {
		if mode != "customer_handoff" {
			return ErrInvalidInput
		}
		_, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET customer_handed_over=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, lineID, *input.CustomerHandedOver)
		return err
	}
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_action_batches b
  JOIN procurement_action_items i ON i.batch_id=b.id WHERE b.procurement_order_id=$1 AND b.status<>'cancelled'
  AND i.status NOT IN ('draft','skipped','not_configured')
  AND (i.channel='saby_receipt' OR i.procurement_order_line_id=$2 OR i.procurement_order_line_id IN
    (SELECT id FROM procurement_order_lines WHERE receiving_parent_line_id=$2)))`, orderID, lineID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return &UserFacingError{Message: "По этой строке уже отправлялись цены или поступление. Сначала сверьте фактические документы; менять разбор после отправки нельзя."}
	}
	if input.ReceivingMode != nil {
		mode = *input.ReceivingMode
	}
	if !oneOf(mode, "catalog", "assortment", "customer_handoff") {
		return ErrInvalidInput
	}
	if input.ReceivingAllocations != nil && mode != "assortment" {
		return ErrInvalidInput
	}
	// Replacing a draft distribution is atomic: on any invalid candidate, all old
	// children and their draft actions are restored by the outer transaction.
	if _, err := tx.Exec(ctx, `UPDATE procurement_action_batches SET status='cancelled',updated_at=CURRENT_TIMESTAMP WHERE procurement_order_id=$1 AND status='draft'`, orderID); err != nil {
		return err
	}
	if input.ReceivingMode != nil || input.ReceivingAllocations != nil {
		if err := releaseOrderAllocations(ctx, tx, orderID, "receiving_distribution_changed"); err != nil {
			return err
		}
		// Preserve cancelled proposals in history even when their receipt child goes.
		// Child rows are retired instead of deleted, so action foreign keys stay valid.
		if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET reconciliation_status='superseded',updated_at=CURRENT_TIMESTAMP WHERE receiving_parent_line_id=$1 AND reconciliation_status<>'superseded'`, lineID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET receiving_mode=$2,
  match_status=CASE WHEN $2<>'catalog' THEN 'deferred' WHEN saby_id IS NOT NULL THEN 'confirmed' ELSE 'unmatched' END,
  customer_handed_over=FALSE,purchase_unit_rub=NULL,trolley_delivery_unit_rub=NULL,ryazan_delivery_unit_rub=NULL,
  unit_cost_rub=NULL,proposed_retail_rub=NULL,proposed_marketplace_rub=NULL,proposed_marketplace_strike_rub=NULL,
  updated_at=CURRENT_TIMESTAMP WHERE id=$1`, lineID, mode); err != nil {
		return err
	}
	if input.ReceivingAllocations != nil {
		parts := *input.ReceivingAllocations
		sum := 0
		seen := map[string]bool{}
		for _, part := range parts {
			if part.SabyID == "" || part.Quantity <= 0 || part.Quantity > quantity || seen[part.SabyID] {
				return ErrInvalidInput
			}
			sum += part.Quantity
			seen[part.SabyID] = true
		}
		if len(parts) > 0 && sum != quantity {
			return &UserFacingError{Message: fmt.Sprintf("Распределено %d из %d шт. Сумма должна точно совпадать с инвойсом.", sum, quantity)}
		}
		for _, part := range parts {
			// Keep a stable row for an existing card; cancelled action previews continue
			// to refer to their original immutable payload and remain in history.
			tag, err := tx.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,receiving_parent_line_id,
    saby_id,canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,expected_unit_price,line_total,
    load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status,comparison_accepted)
    SELECT p.procurement_order_id,p.id,n.saby_id,
      (SELECT variant_id FROM canonical_product_directory WHERE active AND saby_id=n.saby_id ORDER BY variant_id LIMIT 1),
      n.name,0,$3::INTEGER,p.unit_price,p.expected_unit_price,p.unit_price*$3::INTEGER,p.load_unit,p.pot_diameter_cm,p.height_cm,
      'confirmed','matched',TRUE
    FROM procurement_order_lines p JOIN saby_nomenclature n ON n.saby_id=$2 AND n.missing_since IS NULL
    WHERE p.id=$1
    ON CONFLICT(receiving_parent_line_id,saby_id) WHERE receiving_parent_line_id IS NOT NULL
    DO UPDATE SET invoiced_qty=EXCLUDED.invoiced_qty,unit_price=EXCLUDED.unit_price,
      expected_unit_price=EXCLUDED.expected_unit_price,line_total=EXCLUDED.line_total,
      load_unit=EXCLUDED.load_unit,pot_diameter_cm=EXCLUDED.pot_diameter_cm,height_cm=EXCLUDED.height_cm,
      reconciliation_status='matched',updated_at=CURRENT_TIMESTAMP`, lineID, part.SabyID, part.Quantity)
			if err != nil {
				return fmt.Errorf("save receiving allocation: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return &UserFacingError{Message: "Одна из выбранных карточек больше не доступна в СБИС. Обновите поиск."}
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_orders SET status='review',calculated_at=NULL,calculation_settings=NULL,calculation_version=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, orderID); err != nil {
		return err
	}
	if err := rebalanceInvoiceAllocations(ctx, tx, orderID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE procurement_documents d SET parse_status=CASE WHEN arithmetic_status<>'ok' OR EXISTS(
  SELECT 1 FROM procurement_order_lines l WHERE l.procurement_document_id=d.id AND NOT l.invoice_excluded
   AND l.reconciliation_status<>'superseded' AND l.match_status IN('unmatched','suggested','new_product')) THEN 'review' ELSE 'parsed' END,
  updated_at=CURRENT_TIMESTAMP WHERE procurement_order_id=$1 AND superseded_at IS NULL`, orderID)
	return err
}

func syncReceivingChildCosts(ctx context.Context, tx pgx.Tx, orderID int64) error {
	_, err := tx.Exec(ctx, `UPDATE procurement_order_lines c SET purchase_unit_rub=p.purchase_unit_rub,
  trolley_delivery_unit_rub=p.trolley_delivery_unit_rub,ryazan_delivery_unit_rub=p.ryazan_delivery_unit_rub,
  unit_cost_rub=p.unit_cost_rub,pot_diameter_cm=p.pot_diameter_cm,height_cm=p.height_cm,load_unit=p.load_unit,proposed_retail_rub=p.proposed_retail_rub,
  proposed_marketplace_rub=p.proposed_marketplace_rub,proposed_marketplace_strike_rub=p.proposed_marketplace_strike_rub,
  baseline_site_price_minor=(SELECT base_price_minor FROM product_variants WHERE id=c.canonical_variant_id),updated_at=CURRENT_TIMESTAMP
  FROM procurement_order_lines p WHERE c.receiving_parent_line_id=p.id AND p.procurement_order_id=$1
  AND p.receiving_mode='assortment' AND c.reconciliation_status<>'superseded'`, orderID)
	return err
}

func checkReceivingComplete(ctx context.Context, tx pgx.Tx, orderID int64, includeHandoffs bool) error {
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_order_lines p WHERE p.procurement_order_id=$1
  AND p.receiving_parent_line_id IS NULL AND NOT p.invoice_excluded AND p.reconciliation_status<>'superseded'
  AND ((p.receiving_mode='assortment' AND COALESCE((SELECT SUM(c.invoiced_qty) FROM procurement_order_lines c
     WHERE c.receiving_parent_line_id=p.id AND c.reconciliation_status<>'superseded'),0)<>COALESCE(p.invoiced_qty,p.ordered_qty))
   OR ($2 AND p.receiving_mode='customer_handoff' AND NOT p.customer_handed_over)))`, orderID, includeHandoffs).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return &UserFacingError{Message: "Перед поступлением разберите миксы по карточкам, а перед завершением также отметьте передачу растений под заказ."}
	}
	return nil
}
