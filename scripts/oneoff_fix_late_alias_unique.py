from pathlib import Path

repo = Path(__file__).resolve().parents[1]
path = repo / "backend/internal/procurement/postgres_catalogue.go"
text = path.read_text()
start = text.index("func reconcileLateAliasMatches(")
end = text.index("func (store *PostgresStore) UpdateOrderLine(", start)
helper = r'''func reconcileLateAliasMatches(ctx context.Context, tx pgx.Tx, aliasID int64, sabyID string) ([]int64, error) {
	type lateAliasPair struct {
		orderID   int64
		addedID   int64
		plannedID int64
		documentID int64
		sourceLine int
	}
	rows, err := tx.Query(ctx, `
		WITH unique_added AS (
			SELECT line.procurement_order_id,line.saby_id,MIN(line.id) AS added_id
			FROM procurement_order_lines line
			JOIN procurement_supplier_aliases alias ON alias.id=line.supplier_alias_id
			WHERE line.supplier_alias_id=$1 AND line.saby_id=$2
				AND alias.match_status='confirmed' AND alias.matched_saby_id=line.saby_id
				AND line.procurement_document_id IS NOT NULL AND line.source_line IS NOT NULL
				AND line.reconciliation_status='added' AND line.match_status='confirmed'
				AND NOT line.invoice_excluded
			GROUP BY line.procurement_order_id,line.saby_id
			HAVING COUNT(*)=1
		), unique_missing AS (
			SELECT procurement_order_id,saby_id,MIN(id) AS planned_id
			FROM procurement_order_lines
			WHERE saby_id=$2 AND procurement_document_id IS NULL
				AND ordered_qty>0 AND reconciliation_status='missing'
			GROUP BY procurement_order_id,saby_id
			HAVING COUNT(*)=1
		)
		SELECT added.procurement_order_id,added.added_id,missing.planned_id,
			invoice.procurement_document_id,invoice.source_line
		FROM unique_added added
		JOIN unique_missing missing USING(procurement_order_id,saby_id)
		JOIN procurement_orders orders ON orders.id=added.procurement_order_id
		JOIN procurement_order_lines invoice ON invoice.id=added.added_id
		WHERE orders.status NOT IN ('received','cancelled')
	`, aliasID, sabyID)
	if err != nil {
		return nil, err
	}
	pairs := make([]lateAliasPair, 0)
	for rows.Next() {
		var pair lateAliasPair
		if err := rows.Scan(&pair.orderID, &pair.addedID, &pair.plannedID, &pair.documentID, &pair.sourceLine); err != nil {
			rows.Close()
			return nil, err
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	seen := map[int64]struct{}{}
	orderIDs := make([]int64, 0, len(pairs))
	for _, pair := range pairs {
		// Free the document/source-line unique key before moving invoice provenance
		// onto the buyer's original planned row. The superseded row keeps all
		// parsed invoice facts, while the active row becomes the single source of truth.
		if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET
			procurement_document_id=NULL,reconciliation_status='superseded',updated_at=CURRENT_TIMESTAMP
			WHERE id=$1`, pair.addedID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines planned SET
			procurement_document_id=$3,supplier_alias_id=invoice.supplier_alias_id,
			canonical_variant_id=COALESCE(invoice.canonical_variant_id,planned.canonical_variant_id),
			invoice_raw_name=invoice.invoice_raw_name,invoice_supplier_article=invoice.invoice_supplier_article,
			invoiced_qty=invoice.invoiced_qty,unit_price=invoice.unit_price,line_total=invoice.line_total,
			match_status=invoice.match_status,source_page=invoice.source_page,source_line=$4,
			comparison_accepted=FALSE,comparison_note='',
			reconciliation_status=CASE WHEN planned.ordered_qty IS DISTINCT FROM invoice.invoiced_qty
				OR (planned.expected_unit_price IS NOT NULL AND
					(invoice.unit_price IS NULL OR ABS(planned.expected_unit_price-invoice.unit_price)>.005))
				THEN 'changed' ELSE 'matched' END,
			updated_at=CURRENT_TIMESTAMP
			FROM procurement_order_lines invoice WHERE planned.id=$1 AND invoice.id=$2`,
			pair.plannedID, pair.addedID, pair.documentID, pair.sourceLine); err != nil {
			return nil, err
		}
		if _, ok := seen[pair.orderID]; !ok {
			seen[pair.orderID] = struct{}{}
			orderIDs = append(orderIDs, pair.orderID)
		}
	}
	return orderIDs, nil
}

'''
text = text[:start] + helper + text[end:]
path.write_text(text)

migration = repo / "timeweb/migrations/20260916_procurement_late_alias_reconciliation.sql"
migration.write_text(r'''-- Repair the exact state produced when an invoice alias was unknown at import:
-- the buyer plan is "missing" and the invoice row is "added". Once both rows
-- resolve to one Saby item, collapse only unambiguous one-to-one pairs.
CREATE TEMP TABLE procurement_late_alias_pairs ON COMMIT DROP AS
WITH unique_added AS (
  SELECT line.procurement_order_id,line.saby_id,MIN(line.id) AS added_id
  FROM procurement_order_lines line
  JOIN procurement_supplier_aliases alias ON alias.id=line.supplier_alias_id
  JOIN procurement_orders orders ON orders.id=line.procurement_order_id
  WHERE line.saby_id IS NOT NULL
    AND alias.match_status='confirmed' AND alias.matched_saby_id=line.saby_id
    AND line.procurement_document_id IS NOT NULL AND line.source_line IS NOT NULL
    AND line.reconciliation_status='added' AND line.match_status='confirmed'
    AND NOT line.invoice_excluded AND orders.status NOT IN ('received','cancelled')
  GROUP BY line.procurement_order_id,line.saby_id
  HAVING COUNT(*)=1
), unique_missing AS (
  SELECT line.procurement_order_id,line.saby_id,MIN(line.id) AS planned_id
  FROM procurement_order_lines line
  JOIN procurement_orders orders ON orders.id=line.procurement_order_id
  WHERE line.saby_id IS NOT NULL AND line.procurement_document_id IS NULL
    AND line.ordered_qty>0 AND line.reconciliation_status='missing'
    AND orders.status NOT IN ('received','cancelled')
  GROUP BY line.procurement_order_id,line.saby_id
  HAVING COUNT(*)=1
)
SELECT added.procurement_order_id,added.added_id,missing.planned_id,
       invoice.procurement_document_id AS document_id,invoice.source_line
FROM unique_added added
JOIN unique_missing missing USING(procurement_order_id,saby_id)
JOIN procurement_order_lines invoice ON invoice.id=added.added_id;

-- The document/source-line pair is unique irrespective of reconciliation state,
-- so detach the superseded duplicate first and only then move provenance.
UPDATE procurement_order_lines stale SET
  procurement_document_id=NULL,reconciliation_status='superseded',updated_at=CURRENT_TIMESTAMP
FROM procurement_late_alias_pairs pair
WHERE stale.id=pair.added_id;

UPDATE procurement_order_lines planned SET
  procurement_document_id=pair.document_id,
  supplier_alias_id=invoice.supplier_alias_id,
  canonical_variant_id=COALESCE(invoice.canonical_variant_id,planned.canonical_variant_id),
  invoice_raw_name=invoice.invoice_raw_name,
  invoice_supplier_article=invoice.invoice_supplier_article,
  invoiced_qty=invoice.invoiced_qty,unit_price=invoice.unit_price,line_total=invoice.line_total,
  match_status=invoice.match_status,source_page=invoice.source_page,source_line=pair.source_line,
  comparison_accepted=FALSE,comparison_note='',
  reconciliation_status=CASE WHEN planned.ordered_qty IS DISTINCT FROM invoice.invoiced_qty
    OR (planned.expected_unit_price IS NOT NULL AND
      (invoice.unit_price IS NULL OR ABS(planned.expected_unit_price-invoice.unit_price)>.005))
    THEN 'changed' ELSE 'matched' END,
  updated_at=CURRENT_TIMESTAMP
FROM procurement_late_alias_pairs pair
JOIN procurement_order_lines invoice ON invoice.id=pair.added_id
WHERE planned.id=pair.planned_id;
''')
