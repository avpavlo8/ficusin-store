package procurement

import (
	"context"
	"fmt"
)

// PublicationValues is the most recently observed state of a linked sales card.
// A missing value is unknown. A local dimension is not a remote observation.
type PublicationValues struct {
	Linked        bool     `json:"linked"`
	SizeLinked    bool     `json:"sizeLinked"`
	PriceEligible *bool    `json:"priceEligible,omitempty"`
	PriceRUB      *float64 `json:"priceRub,omitempty"`
	PotDiameter   *float64 `json:"potDiameterCm,omitempty"`
	Height        *float64 `json:"heightCm,omitempty"`
}

func (store *PostgresStore) loadPublicationValues(ctx context.Context, detail *OrderDetail) error {
	rows, err := store.pool.Query(ctx, `WITH variants AS (
  SELECT DISTINCT canonical_variant_id AS id FROM procurement_order_lines
  WHERE procurement_order_id=$1 AND NOT invoice_excluded AND canonical_variant_id IS NOT NULL
)
SELECT d.variant_id, COALESCE(saby.name,''), NULLIF(saby.price_minor,0)::float8/100,
    saby.saby_id IS NOT NULL, COALESCE(eligible.export_price,false),
    pv.marketplace_scope<>'site_only' AND COALESCE(d.wb_nm_ids[1],'')<>'',
    pv.marketplace_scope<>'site_only' AND COALESCE(d.ozon_articles[1],'')<>'',
    wb.current_price::float8, ozon.current_price::float8, COALESCE(sync.cards,'{}'::jsonb)
FROM variants v JOIN canonical_product_directory d ON d.variant_id=v.id
JOIN product_variants pv ON pv.id=v.id
LEFT JOIN LATERAL (
 SELECT (candidate.manual AND candidate.target*100 IS DISTINCT FROM candidate.baseline)
      OR candidate.baseline<=COALESCE(candidate.floor,0)*100
      OR ABS(candidate.target-candidate.baseline::numeric/100)>candidate.baseline::numeric/100*settings.price_change_threshold
      AS export_price
 FROM (
  SELECT MAX(line.proposed_retail_rub) AS target, BOOL_OR(line.retail_override_rub IS NOT NULL) AS manual,
      COALESCE(MAX(line.baseline_site_price_minor),pv.base_price_minor) AS baseline,
      MAX((line.channel_quotes->'site'->>'breakEvenRub')::bigint) AS floor
  FROM procurement_order_lines line
  WHERE line.procurement_order_id=$1 AND line.canonical_variant_id=v.id AND line.saby_id=d.saby_id
    AND line.match_status='confirmed' AND NOT line.invoice_excluded AND line.reconciliation_status<>'superseded'
    AND line.proposed_retail_rub IS NOT NULL
 ) candidate JOIN procurement_pricing_settings settings ON settings.id=1
 WHERE candidate.target IS NOT NULL
) eligible ON TRUE
LEFT JOIN saby_nomenclature saby ON saby.saby_id=d.saby_id AND saby.missing_since IS NULL
LEFT JOIN procurement_channel_products wb ON wb.channel='wb' AND wb.external_id=d.wb_nm_ids[1]
LEFT JOIN procurement_channel_products ozon ON ozon.channel='ozon' AND ozon.external_id=d.ozon_articles[1]
LEFT JOIN LATERAL (
 SELECT jsonb_object_agg(p.channel, jsonb_build_object('linked',true,'verified',
   CASE WHEN p.status='synced' AND p.payload->>'procurementMeasurements'='true'
     THEN p.payload->'attributes' ELSE NULL END)) AS cards
 FROM catalog_editor_publications p
 JOIN catalog_editor_remote_links l ON l.variant_id=p.variant_id AND l.channel=p.channel AND l.execution_mode=p.execution_mode
 WHERE p.variant_id=d.variant_id AND p.execution_mode='live' AND p.channel IN ('saby','wb','ozon')
   AND CASE p.channel WHEN 'saby' THEN l.remote_id=d.saby_id AND d.saby_id<>''
     WHEN 'wb' THEN l.remote_id=d.wb_nm_ids[1] WHEN 'ozon' THEN l.offer_id=d.ozon_articles[1] END
) sync ON TRUE`, detail.Order.ID)
	if err != nil {
		return fmt.Errorf("load procurement publication values: %w", err)
	}
	defer rows.Close()
	type observedCard struct {
		Linked   bool `json:"linked"`
		Verified struct {
			Pot    *float64 `json:"pot_diameter_cm"`
			Height *float64 `json:"height_cm"`
		} `json:"verified"`
	}
	byVariant := make(map[int64]map[string]PublicationValues)
	for rows.Next() {
		var id int64
		var name string
		var saby, wb, ozon PublicationValues
		var sabyPriceEligible bool
		var observed map[string]observedCard
		if err := rows.Scan(&id, &name, &saby.PriceRUB, &saby.Linked, &sabyPriceEligible, &wb.Linked, &ozon.Linked,
			&wb.PriceRUB, &ozon.PriceRUB, &observed); err != nil {
			return fmt.Errorf("scan procurement publication values: %w", err)
		}
		saby.PriceEligible = &sabyPriceEligible
		saby.SizeLinked = observed["saby"].Linked
		saby.PotDiameter = parseDimension(name, potPattern)
		for channel, values := range map[string]*PublicationValues{"wb": &wb, "ozon": &ozon} {
			card := observed[channel]
			values.SizeLinked = card.Linked
			values.PotDiameter = card.Verified.Pot
			values.Height = card.Verified.Height
		}
		byVariant[id] = map[string]PublicationValues{"saby_price": saby, "wb": wb, "ozon": ozon}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range detail.Lines {
		if id := detail.Lines[i].CanonicalVariantID; id != nil {
			detail.Lines[i].PublicationValues = byVariant[*id]
		}
	}
	return nil
}
