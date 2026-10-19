package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

type pricingReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Physical PIM attributes stay authoritative. An MP observation is a pricing
// input only; reading or syncing it never confirms physical measurements.
func loadPricingPackages(ctx context.Context, db pricingReader, ids []int64) (map[int64]*PricingPackage, error) {
	result := map[int64]*PricingPackage{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := db.Query(ctx, `SELECT v.id,p.catalog_section,
 jsonb_build_object('lengthCm',variant_numeric_attribute(v.id,'package_length_cm'),
 'widthCm',variant_numeric_attribute(v.id,'package_width_cm'),
 'heightCm',variant_numeric_attribute(v.id,'package_height_cm'),'source','PIM сайта'),
 wb.package_dimensions,oz.package_dimensions,e.dimensions
	 FROM product_variants v JOIN products p ON p.id=v.product_id
 LEFT JOIN canonical_product_directory d ON d.variant_id=v.id
 LEFT JOIN procurement_channel_products wb ON wb.channel='wb' AND wb.external_id=d.wb_nm_ids[1]
 LEFT JOIN procurement_channel_products oz ON oz.channel='ozon' AND oz.external_id=d.ozon_articles[1]
 LEFT JOIN procurement_pricing_packages e ON e.variant_id=v.id
 WHERE v.id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var category string
		var pim, wb, oz, evidence *PricingPackage
		if err := rows.Scan(&id, &category, &pim, &wb, &oz, &evidence); err != nil {
			return nil, err
		}
		for _, p := range []*PricingPackage{pim, wb, oz, evidence} {
			if p.valid() {
				p.Category = category
				result[id] = p
				break
			}
		}
	}
	return result, rows.Err()
}

type UnifiedPriceLine struct {
	VariantID    int64                   `json:"variantId"`
	SourceLineID int64                   `json:"sourceLineId"`
	Name         string                  `json:"name"`
	Quantity     int                     `json:"quantity"`
	CostRUB      float64                 `json:"costRub"`
	Quotes       map[string]ChannelQuote `json:"quotes"`
}
type UnifiedOrderPreview struct {
	Version         int                `json:"version"`
	MarkupRate      float64            `json:"markupRate"`
	FixedRUB        float64            `json:"fixedRub"`
	Lines           []UnifiedPriceLine `json:"lines"`
	Units           int                `json:"units"`
	PricedUnits     int                `json:"pricedUnits"`
	ContributionRUB float64            `json:"contributionRub"`
}

func loadUnifiedOrderPreview(ctx context.Context, db pricingReader, s PricingSettings, orderID int64) (*UnifiedOrderPreview, error) {
	if s.Economics.Unified == nil || !s.Economics.Unified.Enabled {
		return nil, nil
	}
	// Closed receipts retain their historical identity and acquisition costs.
	// Resolve a missing identity for this read-only preview through an exact,
	// confirmed Saby link; ambiguous directory entries must remain unresolved.
	rows, err := db.Query(ctx, `WITH directory AS (
 SELECT saby_id,MIN(variant_id) AS variant_id FROM canonical_product_directory
 WHERE active AND saby_id<>'' GROUP BY saby_id HAVING COUNT(*)=1
 ), complete_links AS (
 SELECT x.order_line_id FROM procurement_pricing_line_links x
 JOIN procurement_order_lines l ON l.id=x.order_line_id
 JOIN procurement_orders o ON o.id=l.procurement_order_id
 LEFT JOIN directory d ON d.saby_id=x.saby_id
 WHERE l.procurement_order_id=$1 AND o.status='received' AND l.canonical_variant_id IS NULL
 GROUP BY x.order_line_id,l.invoiced_qty,l.ordered_qty
 HAVING SUM(x.quantity)=COALESCE(l.invoiced_qty,l.ordered_qty) AND COUNT(d.variant_id)=COUNT(*)
 ), source AS (
 SELECT l.*,CASE WHEN c.order_line_id IS NULL AND EXISTS(SELECT 1 FROM procurement_pricing_line_links pending WHERE pending.order_line_id=l.id)
 THEN NULL ELSE COALESCE(x.saby_id,a.matched_saby_id,l.saby_id) END AS pricing_saby_id,
 COALESCE(x.quantity,l.invoiced_qty,l.ordered_qty) AS pricing_qty
 FROM procurement_order_lines l
 LEFT JOIN complete_links c ON c.order_line_id=l.id
 LEFT JOIN procurement_pricing_line_links x ON x.order_line_id=c.order_line_id
 LEFT JOIN procurement_supplier_aliases a ON a.id=l.supplier_alias_id AND a.match_status='confirmed'
 WHERE l.procurement_order_id=$1
 ), resolved AS (
 SELECT l.*,COALESCE(l.canonical_variant_id,d.variant_id) AS preview_variant_id
 FROM source l LEFT JOIN directory d ON d.saby_id=l.pricing_saby_id
 )
 SELECT COALESCE(l.preview_variant_id,0),MIN(l.id),
 COALESCE(NULLIF(MAX(n.name),''),MAX(l.raw_name)),
 SUM(l.pricing_qty)::int,
 CASE WHEN COUNT(*) FILTER(WHERE l.unit_cost_rub>0)=COUNT(*) THEN
 SUM(l.unit_cost_rub*l.pricing_qty)::float8/
 NULLIF(SUM(l.pricing_qty),0) ELSE 0 END
 FROM resolved l LEFT JOIN saby_nomenclature n ON n.saby_id=l.pricing_saby_id
 WHERE l.procurement_order_id=$1 AND l.receiving_mode='catalog' AND l.match_status='confirmed'
 AND NOT l.invoice_excluded AND l.reconciliation_status<>'superseded'
 AND (l.reconciliation_status<>'added' OR l.comparison_accepted)
 AND l.pricing_qty>0
 GROUP BY l.preview_variant_id,CASE WHEN l.preview_variant_id IS NULL THEN l.id ELSE 0 END,CASE WHEN l.preview_variant_id IS NULL THEN l.pricing_saby_id ELSE '' END`, orderID)
	if err != nil {
		return nil, err
	}
	preview := &UnifiedOrderPreview{Version: s.Version, MarkupRate: s.Economics.Unified.MarkupRate, FixedRUB: s.Economics.MonthlyFixedCostsRUB, Lines: []UnifiedPriceLine{}}
	ids := []int64{}
	for rows.Next() {
		var l UnifiedPriceLine
		if err := rows.Scan(&l.VariantID, &l.SourceLineID, &l.Name, &l.Quantity, &l.CostRUB); err != nil {
			rows.Close()
			return nil, err
		}
		preview.Lines = append(preview.Lines, l)
		if l.VariantID > 0 {
			ids = append(ids, l.VariantID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	boxes, err := loadPricingPackages(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	for i := range preview.Lines {
		l := &preview.Lines[i]
		l.Quotes = UnifiedChannelQuotes(s, l.CostRUB, boxes[l.VariantID])
		if l.VariantID == 0 {
			for channel := range l.Quotes {
				l.Quotes[channel] = ChannelQuote{Model: "unified", Missing: []string{"нет связи с вариантом каталога"}}
			}
		}
		preview.Units += l.Quantity
		if l.Quotes["wb"].PriceRUB != nil && l.Quotes["ozon"].PriceRUB != nil {
			preview.PricedUnits += l.Quantity
			preview.ContributionRUB += l.CostRUB * s.Economics.Unified.MarkupRate * float64(l.Quantity)
		}
	}
	sort.Slice(preview.Lines, func(i, j int) bool { return preview.Lines[i].Name < preview.Lines[j].Name })
	return preview, nil
}

// This runs inside the existing explicit calculation transaction. No action
// batch is created or approved; neither a site nor an external price is edited.
func saveUnifiedOrderQuotes(ctx context.Context, tx pgx.Tx, s PricingSettings, orderID int64) error {
	preview, err := loadUnifiedOrderPreview(ctx, tx, s, orderID)
	if err != nil {
		return err
	}
	if preview == nil {
		return nil
	}
	// A SKU may occur in multiple invoice rows. The shared policy uses its
	// quantity-weighted batch cost, exactly as in the preview, for one MP price.
	for _, line := range preview.Lines {
		if line.VariantID == 0 {
			continue
		}
		encoded, err := json.Marshal(line.Quotes)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE procurement_order_lines SET channel_quotes=$3,proposed_retail_rub=$4,
   proposed_marketplace_rub=NULL,proposed_marketplace_strike_rub=NULL
   WHERE procurement_order_id=$1 AND canonical_variant_id=$2 AND receiving_mode='catalog'
   AND match_status='confirmed' AND NOT invoice_excluded AND reconciliation_status<>'superseded'
   AND (reconciliation_status<>'added' OR comparison_accepted)`, orderID, line.VariantID, encoded, line.Quotes["site"].PriceRUB); err != nil {
			return err
		}
	}

	return nil
}

// A manual retail edit must not revive the retired additive MP formula.
func UnifiedVariantPriceQuotes(ctx context.Context, db pricingReader, variantID int64) (map[string]ChannelQuote, bool, error) {
	var s PricingSettings
	if err := db.QueryRow(ctx, `SELECT economics,tax_rate::float8 FROM procurement_pricing_settings WHERE id=1`).Scan(&s.Economics, &s.TaxRate); err != nil {
		return nil, false, err
	}
	if s.Economics.Unified == nil || !s.Economics.Unified.Enabled {
		return nil, false, nil
	}
	var cost *float64
	if err := db.QueryRow(ctx, `SELECT current_unit_cost_rub::float8 FROM product_variants WHERE id=$1`, variantID).Scan(&cost); err != nil {
		return nil, true, err
	}
	if cost == nil || *cost <= 0 {
		return nil, true, nil
	}
	boxes, err := loadPricingPackages(ctx, db, []int64{variantID})
	if err != nil {
		return nil, true, fmt.Errorf("load pricing package: %w", err)
	}
	return UnifiedChannelQuotes(s, *cost, boxes[variantID]), true, nil
}
