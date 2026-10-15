package procurement

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Names are supplier aliases, not replacements for the site's product names.
// A manual confirmation applies to that exact alias in unfinished purchases.
func propagateProcurementAlias(ctx context.Context, tx pgx.Tx, aliasID int64, sabyID string, canonical int64) error {
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_order_lines l
 JOIN procurement_orders o ON o.id=l.procurement_order_id
 JOIN procurement_action_batches b ON b.procurement_order_id=o.id
 WHERE l.supplier_alias_id=$1 AND l.receiving_mode IN ('catalog','awaiting_catalog') AND o.status NOT IN ('received','cancelled')
 AND l.saby_id IS DISTINCT FROM $2 AND (b.status='processing' OR EXISTS
 (SELECT 1 FROM procurement_action_items i WHERE i.batch_id=b.id AND i.status='completed')))`, aliasID, sabyID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return &UserFacingError{Message: "По этому названию уже выполняется или выполнена отправка. Сначала сверьте документы связанной закупки."}
	}
	rows, err := tx.Query(ctx, `UPDATE procurement_order_lines l SET saby_id=$2,canonical_variant_id=NULLIF($3,0),match_status='confirmed',receiving_mode='catalog',
 purchase_unit_rub=NULL,additional_purchase_unit_rub=NULL,trolley_delivery_unit_rub=NULL,ryazan_delivery_unit_rub=NULL,unit_cost_rub=NULL,
 proposed_retail_rub=NULL,proposed_marketplace_rub=NULL,proposed_marketplace_strike_rub=NULL,channel_quotes='{}'::JSONB,updated_at=CURRENT_TIMESTAMP
 FROM procurement_orders o WHERE l.procurement_order_id=o.id AND l.supplier_alias_id=$1
 AND o.status NOT IN ('received','cancelled') AND l.receiving_mode IN ('catalog','awaiting_catalog') AND l.reconciliation_status<>'superseded'
 AND (l.saby_id IS DISTINCT FROM $2 OR l.canonical_variant_id IS DISTINCT FROM NULLIF($3,0) OR l.match_status<>'confirmed')
 RETURNING o.id`, aliasID, sabyID, canonical)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE procurement_action_batches b SET status='cancelled',updated_at=CURRENT_TIMESTAMP
 WHERE procurement_order_id=ANY($1) AND status IN ('draft','partially_completed')
 AND NOT EXISTS(SELECT 1 FROM procurement_action_items i WHERE i.batch_id=b.id AND i.status='completed')`, ids); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_orders SET status='review',calculated_at=NULL,calculation_settings=NULL,calculation_version=NULL,
 updated_at=CURRENT_TIMESTAMP WHERE id=ANY($1)`, ids)
	return err
}

// The parent holds the aggregate used by calculations and receipts. The JSON
// snapshots and superseded source rows retain the individual PDF facts.
func appendInvoiceLinesToPlan(ctx context.Context, tx pgx.Tx, actorID, orderID, plannedID int64, invoiceIDs []int64) error {
	var sabyID string
	var canonical, planAlias, documentID int64
	var loadUnit string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(saby_id,''),COALESCE(canonical_variant_id,
 (SELECT variant_id FROM canonical_product_directory WHERE active AND saby_id=l.saby_id ORDER BY variant_id LIMIT 1),0),
 COALESCE(supplier_alias_id,0),COALESCE(procurement_document_id,0),load_unit
 FROM procurement_order_lines l WHERE id=$1 AND procurement_order_id=$2 AND ordered_qty>0
 AND NOT invoice_excluded AND reconciliation_status IN ('missing','matched','changed') FOR UPDATE`, plannedID, orderID).
		Scan(&sabyID, &canonical, &planAlias, &documentID, &loadUnit); err != nil {
		return &UserFacingError{Message: "Обновите закупку и выберите существующую позицию плана."}
	}
	if sabyID == "" {
		return &UserFacingError{Message: "Сначала свяжите позицию плана с товаром СБИС."}
	}
	var count int
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT COUNT(*)::INTEGER,COALESCE(BOOL_AND(l.ordered_qty=0 AND l.reconciliation_status='added'
 AND NOT l.comparison_accepted AND NOT l.invoice_excluded AND l.supplier_alias_id IS NOT NULL
 AND l.source_line IS NOT NULL AND l.invoiced_qty>0 AND l.unit_price>0
 AND d.superseded_at IS NULL AND ($4=0 OR l.procurement_document_id=$4)),FALSE)
 FROM procurement_order_lines l LEFT JOIN procurement_documents d ON d.id=l.procurement_document_id
 WHERE l.procurement_order_id=$1 AND l.id=ANY($2) AND l.id<>$3`, orderID, invoiceIDs, plannedID, documentID).Scan(&count, &valid); err != nil {
		return err
	}
	if count != len(invoiceIDs) || !valid {
		return &UserFacingError{Message: "Часть выбранных строк уже сопоставлена или относится к другой версии инвойса. Обновите закупку."}
	}
	var documents, loads, sizes int
	if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT procurement_document_id)::INTEGER,COUNT(DISTINCT load_unit)::INTEGER,
 COUNT(DISTINCT (COALESCE(a.pot_diameter_cm,l.pot_diameter_cm),COALESCE(a.height_cm,l.height_cm)))::INTEGER FROM procurement_order_lines l LEFT JOIN procurement_supplier_aliases a ON a.id=l.supplier_alias_id
 WHERE l.procurement_order_id=$1 AND (l.id=ANY($2) OR (l.id=$3 AND l.procurement_document_id IS NOT NULL))`, orderID, invoiceIDs, plannedID).Scan(&documents, &loads, &sizes); err != nil {
		return err
	}
	// Plan dimensions can differ from the PDF and are reviewed separately; rows
	// with different loading units cannot share one logistics calculation.
	if sizes > 1 {
		return &UserFacingError{Message: "В выбранных строках разные размеры растения. Уточните размеры перед объединением."}
	}
	if documents != 1 || loads != 1 {
		return &UserFacingError{Message: "Объединяйте строки одного инвойса с одинаковой телегой или полкой."}
	}
	var aliases []int64
	rows, err := tx.Query(ctx, `SELECT id,supplier_alias_id FROM procurement_order_lines WHERE id=ANY($1) ORDER BY id FOR UPDATE`, invoiceIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, alias int64
		if err := rows.Scan(&id, &alias); err != nil {
			rows.Close()
			return err
		}
		aliases = append(aliases, alias)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if planAlias > 0 {
		aliases = append(aliases, planAlias)
	}
	for _, alias := range aliases {
		if err := remapProcurementAlias(ctx, tx, actorID, alias, sabyID, canonical); err != nil {
			return err
		}
	}
	// Capture the original first assignment before changing its totals.
	if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines l SET invoice_components=CASE WHEN jsonb_array_length(invoice_components)>0
 THEN invoice_components ELSE jsonb_build_array(jsonb_build_object('rawName',invoice_raw_name,'article',invoice_supplier_article,
 'quantity',invoiced_qty,'unitPrice',unit_price,'lineTotal',COALESCE(line_total,invoiced_qty*unit_price),
 'aliasId',supplier_alias_id,'documentId',procurement_document_id,'sourcePage',source_page,'sourceLine',source_line,'loadUnit',load_unit,'potDiameterCm',pot_diameter_cm,'heightCm',height_cm,'packageCount',package_count,'unitsPerPackage',units_per_package)) END
 WHERE id=$1 AND procurement_document_id IS NOT NULL`, plannedID); err != nil {
		return err
	}
	var components []byte
	if err := tx.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('rawName',invoice_raw_name,'article',invoice_supplier_article,
 'quantity',invoiced_qty,'unitPrice',unit_price,'lineTotal',COALESCE(line_total,invoiced_qty*unit_price),
 'aliasId',supplier_alias_id,'documentId',procurement_document_id,'sourcePage',source_page,'sourceLine',source_line,'loadUnit',load_unit,'potDiameterCm',pot_diameter_cm,'heightCm',height_cm,'packageCount',package_count,'unitsPerPackage',units_per_package) ORDER BY source_line,id)
 FROM procurement_order_lines WHERE id=ANY($1)`, invoiceIDs).Scan(&components); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET procurement_document_id=NULL,reconciliation_status='superseded',updated_at=CURRENT_TIMESTAMP WHERE id=ANY($1)`, invoiceIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `WITH parts AS(SELECT invoice_components||$2::JSONB AS data FROM procurement_order_lines WHERE id=$1),
 totals AS(SELECT data,SUM((v->>'quantity')::INTEGER)::INTEGER AS qty,SUM((v->>'lineTotal')::NUMERIC) AS total,
 STRING_AGG(DISTINCT v->>'rawName',' / ') AS names FROM parts,jsonb_array_elements(data) v GROUP BY data)
 UPDATE procurement_order_lines l SET invoice_components=t.data,dimensions_reviewed=FALSE,invoiced_qty=t.qty,line_total=t.total,unit_price=t.total/t.qty,
 invoice_raw_name=t.names,invoice_supplier_article=COALESCE(t.data->0->>'article',''),
 procurement_document_id=(t.data->0->>'documentId')::BIGINT,supplier_alias_id=(t.data->0->>'aliasId')::BIGINT,
 source_page=(t.data->0->>'sourcePage')::INTEGER,source_line=(t.data->0->>'sourceLine')::INTEGER,
 load_unit=t.data->0->>'loadUnit',match_status='confirmed',canonical_variant_id=NULLIF($3,0),
 comparison_accepted=FALSE,comparison_note='',
 reconciliation_status=CASE WHEN l.ordered_qty<>t.qty OR l.expected_unit_price IS NULL OR
 EXISTS(SELECT 1 FROM jsonb_array_elements(t.data) v WHERE ABS(l.expected_unit_price-(v->>'unitPrice')::NUMERIC)>.005)
 THEN 'changed' ELSE 'matched' END,
 purchase_unit_rub=NULL,additional_purchase_unit_rub=NULL,trolley_delivery_unit_rub=NULL,ryazan_delivery_unit_rub=NULL,unit_cost_rub=NULL,
 proposed_retail_rub=NULL,proposed_marketplace_rub=NULL,proposed_marketplace_strike_rub=NULL,channel_quotes='{}'::JSONB,updated_at=CURRENT_TIMESTAMP
 FROM totals t WHERE l.id=$1`, plannedID, string(components), canonical); err != nil {
		return fmt.Errorf("aggregate invoice rows: %w", err)
	}
	return nil
}

// Automatic grouping requires one exact destination (card and dimensions).
// Ambiguous plans stay in the explicit picker instead of choosing by name.
func reconcileConfirmedInvoiceGroups(ctx context.Context, tx pgx.Tx, actorID, orderID int64) error {
	rows, err := tx.Query(ctx, `WITH destinations AS (
 SELECT i.id AS invoice_id,MIN(p.id) AS plan_id
 FROM procurement_order_lines i JOIN procurement_order_lines p
 ON p.procurement_order_id=i.procurement_order_id AND p.saby_id=i.saby_id
 AND p.pot_diameter_cm IS NOT DISTINCT FROM i.pot_diameter_cm
 AND p.height_cm IS NOT DISTINCT FROM i.height_cm AND p.load_unit=i.load_unit
 WHERE i.procurement_order_id=$1 AND i.reconciliation_status='added' AND i.match_status='confirmed'
 AND NOT i.invoice_excluded AND NOT i.comparison_accepted
 AND p.ordered_qty>0 AND p.match_status='confirmed' AND NOT p.invoice_excluded
 AND p.reconciliation_status IN ('missing','matched','changed')
 GROUP BY i.id HAVING COUNT(*)=1)
 SELECT plan_id,ARRAY_AGG(invoice_id ORDER BY invoice_id) FROM destinations GROUP BY plan_id ORDER BY plan_id`, orderID)
	if err != nil {
		return err
	}
	type group struct {
		plan     int64
		invoices []int64
	}
	groups := []group{}
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.plan, &g.invoices); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if err := appendInvoiceLinesToPlan(ctx, tx, actorID, orderID, g.plan, g.invoices); err != nil {
			return err
		}
	}
	return nil
}
