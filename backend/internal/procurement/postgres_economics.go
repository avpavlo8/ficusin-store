package procurement

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func validatePriceQuotes(ctx context.Context, tx pgx.Tx, orderID int64, channels []string) error {
	var fresh bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(o.calculated_at IS NOT NULL AND o.calculation_version=s.version,FALSE)
		FROM procurement_orders o CROSS JOIN procurement_pricing_settings s WHERE o.id=$1 AND s.id=1`, orderID).Scan(&fresh); err != nil {
		return err
	}
	if !fresh {
		return &UserFacingError{Message: "Формула изменилась или расчёт устарел. Пересчитайте закупку перед подготовкой цен."}
	}
	rows, err := tx.Query(ctx, `SELECT l.raw_name,l.channel_quotes,pv.marketplace_scope
		FROM procurement_order_lines l JOIN product_variants pv ON pv.id=l.canonical_variant_id
		WHERE l.procurement_order_id=$1 AND l.match_status='confirmed' AND NOT l.invoice_excluded
		AND l.reconciliation_status<>'superseded' AND l.saby_id IS NOT NULL`, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, scope string
		var quotes map[string]ChannelQuote
		if err := rows.Scan(&name, &quotes, &scope); err != nil {
			return err
		}
		for _, channel := range channels {
			if channel == "saby_price" || channel == "avito" {
				channel = "site"
			}
			if channel != "site" && scope == "site_only" {
				continue
			}
			quote := quotes[channel]
			if quote.PriceRUB == nil || *quote.PriceRUB <= 0 || quote.ContributionRUB == nil || *quote.ContributionRUB <= 0 {
				reason := strings.Join(quote.Missing, ", ")
				if reason == "" {
					reason = "нужен новый расчёт расходов"
				}
				return &UserFacingError{Message: fmt.Sprintf("%s · %s: %s. Заполните расходы и пересчитайте закупку.", name, channel, reason)}
			}
		}
	}
	return rows.Err()
}
