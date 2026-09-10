package procurement

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Save the estimate atomically with the plan, without making it receivable.
func savePlanCalculation(ctx context.Context, tx pgx.Tx, orderID int64, input PlanCreate, calculated []calculatedLine, settings PricingSettings) error {
	rows, err := tx.Query(ctx, `SELECT id FROM procurement_order_lines WHERE procurement_order_id=$1 ORDER BY id`, orderID)
	if err != nil { return err }
	var ids []int64
	for rows.Next() { var id int64; if err := rows.Scan(&id); err != nil { rows.Close(); return err }; ids = append(ids, id) }
	rows.Close()
	if err := rows.Err(); err != nil { return err }
	if len(ids) != len(input.Items) || len(ids) != len(calculated) { return fmt.Errorf("procurement plan line count mismatch") }
	for index, id := range ids {
		line, source := calculated[index], input.Items[index]
		if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET purchase_unit_rub=$2,
			trolley_delivery_unit_rub=$3, ryazan_delivery_unit_rub=$4, unit_cost_rub=$5,
			proposed_retail_rub=$6, proposed_marketplace_rub=$7, proposed_marketplace_strike_rub=$8,
			package_count=NULLIF($9,0), units_per_package=NULLIF($10,0), supplier_category=$11
			WHERE id=$1`, id, line.PurchaseUnitRUB, line.TrolleyDeliveryUnitRUB, line.RyazanDeliveryUnitRUB,
			line.UnitCostRUB, line.ProposedRetailRUB, line.ProposedMarketplaceRUB, line.ProposedMarketplaceStrikeRUB,
			source.PackageCount, source.UnitsPerPackage, source.Category); err != nil { return err }
		if source.SabyID != "" && source.UnitsPerPackage > 0 {
			if _, err := tx.Exec(ctx, `UPDATE procurement_supplier_products SET order_multiple=$3,
				supplier_article=$4, updated_at=CURRENT_TIMESTAMP WHERE supplier_id=$1 AND saby_id=$2`,
				input.SupplierID, source.SabyID, source.UnitsPerPackage, source.SupplierArticle); err != nil { return err }
		}
	}
	snapshot, err := json.Marshal(settings)
	if err != nil { return err }
	_, err = tx.Exec(ctx, `UPDATE procurement_orders SET exchange_rate=$2, delivery_to_moscow_rub=$3,
		delivery_to_ryazan_rub=$4, calculation_version=$5, calculation_settings=$6, calculated_at=CURRENT_TIMESTAMP
		WHERE id=$1`, orderID, input.Costs.ExchangeRate, input.Costs.DeliveryToMoscowRUB,
		input.Costs.DeliveryToRyazanRUB, settings.Version, snapshot)
	return err
}
