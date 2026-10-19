package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

// Finished marketplace attempts must not block new proposals. A failed
// receipt may have been posted manually in Saby. Do not retry it,
// reconcile its status, or recalculate acquisition costs here. Only refresh
// reviewable price proposals from the existing cost snapshot.
// The caller holds the order lock, also used by batch approval and retry.
func refreshPricesAfterTerminalActions(ctx context.Context, tx pgx.Tx, actor Actor, orderID int64, input CalculationInput, settings PricingSettings) error {
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(BOOL_AND(
		b.status IN ('failed','partially_completed')
		AND ((b.kind='receipt' AND EXISTS(SELECT 1 FROM procurement_action_items i WHERE i.batch_id=b.id AND i.channel='saby_receipt' AND i.status='failed'))
		OR (b.kind='prices' AND EXISTS(SELECT 1 FROM procurement_action_items i WHERE i.batch_id=b.id)
		AND NOT EXISTS(SELECT 1 FROM procurement_action_items i WHERE i.batch_id=b.id AND i.status NOT IN ('completed','failed','skipped','not_configured'))))
		AND NOT EXISTS(SELECT 1 FROM procurement_action_items i WHERE i.batch_id=b.id AND i.status IN ('queued','processing'))
	),FALSE) FROM procurement_action_batches b
	WHERE b.procurement_order_id=$1 AND b.status NOT IN ('draft','cancelled','completed')`, orderID).Scan(&eligible); err != nil {
		return fmt.Errorf("check failed receipt price refresh: %w", err)
	}
	if !eligible {
		return &UserFacingError{Message: "По закупке есть незавершённый пакет отправки. Дождитесь завершения выполняющихся действий или сверьте ошибки в журнале закупки."}
	}
	var sameCosts bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(calculated_at IS NOT NULL
		AND exchange_rate=$2 AND delivery_to_moscow_rub=$3 AND delivery_to_ryazan_rub=$4
		AND additional_costs_rub=$5,FALSE) FROM procurement_orders WHERE id=$1`, orderID,
		input.ExchangeRate, input.DeliveryToMoscowRUB, input.DeliveryToRyazanRUB, input.AdditionalCostsRUB).Scan(&sameCosts); err != nil {
		return fmt.Errorf("check frozen procurement costs: %w", err)
	}
	if !sameCosts || input.TrolleyCostRUB != 0 || input.TrolleyCostCurrency != 0 {
		return &UserFacingError{Message: "Поступление СБИС уже отправлялось. Сейчас можно пересчитать только цены по сохранённой себестоимости. Верните сохранённые курс и расходы доставки; изменение себестоимости требует сверки поступления."}
	}
	type priceLine struct {
		id       int64
		cost     *float64
		current  float64
		override *int64
		costs    map[string]ChannelCosts
		prices   map[string]int64
	}
	rows, err := tx.Query(ctx, `SELECT l.id,l.unit_cost_rub::DOUBLE PRECISION,
		COALESCE(pv.base_price_minor::DOUBLE PRECISION/100,0),l.retail_override_rub,l.channel_costs,l.channel_prices
		FROM procurement_order_lines l LEFT JOIN product_variants pv ON pv.id=l.canonical_variant_id
		WHERE l.procurement_order_id=$1 AND l.match_status IN ('confirmed','deferred')
		AND NOT l.invoice_excluded AND l.reconciliation_status<>'superseded'
		AND (l.reconciliation_status<>'added' OR l.comparison_accepted) FOR UPDATE OF l`, orderID)
	if err != nil {
		return fmt.Errorf("read frozen procurement prices: %w", err)
	}
	var lines []priceLine
	for rows.Next() {
		var line priceLine
		if err := rows.Scan(&line.id, &line.cost, &line.current, &line.override, &line.costs, &line.prices); err != nil {
			rows.Close()
			return err
		}
		lines = append(lines, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(lines) == 0 {
		return &UserFacingError{Message: "Нет сохранённых строк для пересчёта цен."}
	}
	for _, line := range lines {
		if line.cost == nil || !finiteNonnegative(*line.cost) || *line.cost <= 0 {
			return &UserFacingError{Message: "В закупке нет сохранённой себестоимости. Сначала сверьте поступление СБИС."}
		}
		retail := roundRetail(*line.cost*settings.RetailMarkupMultiplier, settings.RoundPrices)
		if line.override == nil && line.current > 0 && math.Trunc(line.current) == line.current && !priceChangeNeeded(line.current, retail, settings.PriceChangeThreshold) {
			if q := calculateChannelQuotes(settings, *line.cost, int64(line.current), line.costs); q["site"].PriceRUB != nil {
				retail = int64(line.current)
			}
		}
		if line.override != nil {
			retail = *line.override
		}
		quotes, err := json.Marshal(calculateChannelQuotes(settings, *line.cost, retail, line.costs, line.prices))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE procurement_order_lines SET proposed_retail_rub=$2,
			proposed_marketplace_rub=NULL,proposed_marketplace_strike_rub=NULL,channel_quotes=$3,
			baseline_site_price_minor=(SELECT base_price_minor FROM product_variants WHERE id=canonical_variant_id),
			updated_at=CURRENT_TIMESTAMP WHERE id=$1`, line.id, retail, quotes); err != nil {
			return err
		}
	}
	if err := saveUnifiedOrderQuotes(ctx, tx, settings, orderID); err != nil {
		return err
	}
	snapshot, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE procurement_action_batches SET status='cancelled',updated_at=CURRENT_TIMESTAMP
		WHERE procurement_order_id=$1 AND status='draft' AND kind='prices'`, orderID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE procurement_orders SET calculation_version=$2,calculation_settings=$3,
		calculated_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, orderID, settings.Version, snapshot); err != nil {
		return err
	}
	return audit(ctx, tx, actor, "procurement.order.refresh_prices", "procurement_order", orderID, map[string]any{"purchaseCostsPreserved": true, "receiptUnchanged": true, "pricingVersion": settings.Version})
}
