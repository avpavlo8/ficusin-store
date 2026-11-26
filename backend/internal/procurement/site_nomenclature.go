package procurement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (store *PostgresStore) SearchSiteNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	rows, err := store.pool.Query(ctx, `SELECT d.variant_id, COALESCE(n.saby_id,''),
 d.display_sku, '', CONCAT_WS(' · ',d.name,NULLIF(d.label,'')),
 COALESCE(n.balance,0),v.base_price_minor::DOUBLE PRECISION/100
 FROM canonical_product_directory d JOIN product_variants v ON v.id=d.variant_id
 LEFT JOIN saby_nomenclature n ON n.saby_id=d.saby_id AND n.missing_since IS NULL
 WHERE d.active AND d.product_status<>'archived' AND
  (CONCAT_WS(' ',d.name,d.label) ILIKE '%'||$1||'%' OR d.display_sku ILIKE '%'||$1||'%')
 ORDER BY (LOWER(d.display_sku)=LOWER($1)) DESC, (d.product_status='published') DESC, d.name,d.variant_id LIMIT 30`, query)
	if err != nil {
		return nil, fmt.Errorf("search procurement site catalogue: %w", err)
	}
	defer rows.Close()
	items := make([]NomenclatureCandidate, 0, 30)
	for rows.Next() {
		var item NomenclatureCandidate
		if err := rows.Scan(&item.VariantID, &item.SabyID, &item.Code, &item.Article, &item.Name, &item.Balance, &item.Price); err != nil {
			return nil, fmt.Errorf("scan procurement site catalogue: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func linkProcurementSiteLine(ctx context.Context, tx pgx.Tx, actor Actor, lineID, variantID int64) error {
	var aliasID, supplierID int64
	var line ParsedLine
	var seenAt *time.Time
	err := tx.QueryRow(ctx, `SELECT COALESCE(l.supplier_alias_id,0),o.supplier_id,
  COALESCE(NULLIF(l.invoice_raw_name,''),l.raw_name),
  COALESCE(NULLIF(l.invoice_supplier_article,''),l.supplier_article,''),
  l.pot_diameter_cm::float8,l.height_cm::float8,o.document_date
FROM procurement_order_lines l JOIN procurement_orders o ON o.id=l.procurement_order_id
WHERE l.id=$1 AND NOT l.invoice_excluded AND l.reconciliation_status NOT IN ('missing','superseded')
  AND l.invoiced_qty>0 AND l.receiving_mode IN ('catalog','awaiting_catalog')
  AND o.status IN ('review','invoice_received','ready_to_receive')`, lineID).Scan(
		&aliasID, &supplierID, &line.RawName, &line.SupplierArticle, &line.PotDiameterCM, &line.HeightCM, &seenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read procurement line for site card: %w", err)
	}
	if aliasID == 0 {
		aliasID, _, _, err = upsertAlias(ctx, tx, supplierID, line, seenAt)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET supplier_alias_id=$2 WHERE id=$1`, lineID, aliasID); err != nil {
			return fmt.Errorf("attach supplier alias to procurement line: %w", err)
		}
	}
	_, err = resolveAliasTx(ctx, tx, actor, aliasID, AliasResolution{MatchStatus: "confirmed", VariantID: variantID, OrderLineID: lineID})
	return err
}
