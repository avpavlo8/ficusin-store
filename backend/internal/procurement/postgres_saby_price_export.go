package procurement

import (
	"context"
	"fmt"
)

// ApprovedSabyPriceExportLines returns only prices backed by a completed site
// approval from the current calculation. A download is still a manual import,
// so it must never imply that Saby has accepted the price.
func (store *PostgresStore) ApprovedSabyPriceExportLines(ctx context.Context, orderID int64) ([]OrderLine, error) {
	rows, err := store.pool.Query(ctx, `
		WITH candidates AS (
			SELECT line.saby_id, line.canonical_variant_id AS variant_id,
				MAX(line.proposed_retail_rub) AS proposed_price,
				MAX(line.baseline_site_price_minor) AS baseline_price
			FROM procurement_order_lines line
			WHERE line.procurement_order_id=$1 AND line.match_status='confirmed'
				AND NOT line.invoice_excluded AND line.reconciliation_status<>'superseded'
				AND line.saby_id IS NOT NULL AND line.canonical_variant_id IS NOT NULL
				AND line.proposed_retail_rub IS NOT NULL
			GROUP BY line.saby_id,line.canonical_variant_id
		), eligible AS (
			SELECT candidate.*, variant.base_price_minor,
				variant.saby_id AS variant_saby_id, saby.saby_id AS active_saby_id,
				COALESCE(NULLIF(saby.code,''),candidate.saby_id) AS saby_code,
				saby.missing_since
			FROM candidates candidate
			JOIN product_variants variant ON variant.id=candidate.variant_id
			LEFT JOIN saby_nomenclature saby ON saby.saby_id=candidate.saby_id
			WHERE COALESCE(candidate.baseline_price,variant.base_price_minor)<=0
				OR ABS(candidate.proposed_price::NUMERIC-
					COALESCE(candidate.baseline_price,variant.base_price_minor)::NUMERIC/100)
					> (COALESCE(candidate.baseline_price,variant.base_price_minor)::NUMERIC/100)*
					(SELECT price_change_threshold FROM procurement_pricing_settings WHERE id=1)
		)
		SELECT eligible.saby_id,eligible.variant_id,eligible.saby_code,
			eligible.proposed_price,
			(orders.status='ready_to_receive' AND eligible.active_saby_id IS NOT NULL
				AND eligible.missing_since IS NULL AND eligible.variant_saby_id=eligible.saby_id
				AND eligible.base_price_minor=ROUND(eligible.proposed_price*100)::BIGINT
				AND EXISTS (
					SELECT 1 FROM procurement_action_items item
					JOIN procurement_action_batches batch ON batch.id=item.batch_id
					JOIN procurement_order_lines line ON line.id=item.procurement_order_line_id
					WHERE batch.procurement_order_id=orders.id
						AND batch.calculation_version IS NOT DISTINCT FROM orders.calculation_version
						AND batch.status<>'cancelled' AND item.channel='site'
						AND item.status='completed' AND line.canonical_variant_id=eligible.variant_id
						AND item.new_value=eligible.proposed_price
				)) AS approved
		FROM eligible CROSS JOIN procurement_orders orders
		WHERE orders.id=$1
		ORDER BY eligible.saby_id,eligible.variant_id
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("read approved Saby prices: %w", err)
	}
	defer rows.Close()
	var lines []OrderLine
	seen := make(map[string]int64)
	for rows.Next() {
		var line OrderLine
		var variantID, price int64
		var approved bool
		if err := rows.Scan(&line.SabyID, &variantID, &line.SabyCode, &price, &approved); err != nil {
			return nil, fmt.Errorf("scan approved Saby price: %w", err)
		}
		if !approved {
			return nil, &UserFacingError{Message: "Сначала утвердите цену сайта из текущего расчёта. Если она изменилась позже, пересчитайте закупку."}
		}
		if previous, exists := seen[line.SabyID]; exists && previous != price {
			return nil, &UserFacingError{Message: "Одна позиция СБИС связана с вариантами с разными ценами. Исправьте связи перед выгрузкой."}
		}
		seen[line.SabyID] = price
		line.CanonicalVariantID = &variantID
		line.ProposedRetailRUB = &price
		line.MatchStatus = "confirmed"
		line.PriceChangeNeeded = true
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read approved Saby price rows: %w", err)
	}
	if len(lines) == 0 {
		return nil, &UserFacingError{Message: "В текущем расчёте нет цен СБИС выше порога изменения."}
	}
	return lines, nil
}
