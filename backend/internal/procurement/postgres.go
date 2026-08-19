package procurement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (store *PostgresStore) Dashboard(ctx context.Context) (Dashboard, error) {
	var result Dashboard
	if err := store.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM procurement_orders WHERE status NOT IN ('received', 'cancelled'))::INTEGER,
			(SELECT COUNT(*) FROM procurement_supplier_aliases WHERE match_status IN ('unmatched', 'suggested', 'new_product'))::INTEGER,
			(SELECT COUNT(*) FROM procurement_supplier_products WHERE availability_status = 'check' OR (check_after IS NOT NULL AND check_after <= CURRENT_DATE))::INTEGER,
			(SELECT COUNT(*) FROM procurement_requests WHERE status = 'open')::INTEGER
	`).Scan(
		&result.Summary.OpenOrders,
		&result.Summary.UnresolvedAliases,
		&result.Summary.AvailabilityChecks,
		&result.Summary.OpenRequests,
	); err != nil {
		return Dashboard{}, fmt.Errorf("query procurement totals: %w", err)
	}

	suppliers, err := store.listSuppliers(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	orders, err := store.listOrders(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	review, err := store.listReview(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	documents, err := store.listDocuments(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	settings, err := store.loadSettings(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	requests, err := store.listRequests(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	availability, err := store.listAvailability(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	recommendations, err := store.listRecommendations(ctx, settings)
	if err != nil {
		return Dashboard{}, err
	}
	salesSync, err := store.listSalesSync(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	result.Settings = settings
	result.Suppliers, result.Orders, result.Documents, result.Review = suppliers, orders, documents, review
	result.Requests, result.Availability, result.Recommendations = requests, availability, recommendations
	result.SalesSync = salesSync
	return result, nil
}

func (store *PostgresStore) ListIntegrationHealth(ctx context.Context) ([]IntegrationHealth, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT channel, false, last_checked_at, last_success_at, last_error
		FROM procurement_integration_health ORDER BY CASE channel WHEN 'saby' THEN 1 WHEN 'wb' THEN 2 ELSE 3 END
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement integration health: %w", err)
	}
	defer rows.Close()
	items := make([]IntegrationHealth, 0, 3)
	for rows.Next() {
		var item IntegrationHealth
		if err := rows.Scan(&item.Channel, &item.Configured, &item.LastCheckedAt, &item.LastSuccessAt, &item.LastError); err != nil {
			return nil, fmt.Errorf("scan procurement integration health: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) RecordIntegrationCheck(ctx context.Context, channel string, configured bool, checkErr error) (IntegrationHealth, error) {
	message := ""
	if checkErr != nil {
		message = safeError(checkErr.Error())
	}
	var item IntegrationHealth
	err := store.pool.QueryRow(ctx, `
		INSERT INTO procurement_integration_health (channel, last_checked_at, last_success_at, last_error)
		VALUES ($1, CURRENT_TIMESTAMP, CASE WHEN $3 = '' THEN CURRENT_TIMESTAMP ELSE NULL END, $3)
		ON CONFLICT (channel) DO UPDATE SET last_checked_at = CURRENT_TIMESTAMP,
			last_success_at = CASE WHEN EXCLUDED.last_error = '' THEN CURRENT_TIMESTAMP ELSE procurement_integration_health.last_success_at END,
			last_error = EXCLUDED.last_error
		RETURNING channel, $2::BOOLEAN, last_checked_at, last_success_at, last_error
	`, channel, configured, message).Scan(
		&item.Channel, &item.Configured, &item.LastCheckedAt, &item.LastSuccessAt, &item.LastError,
	)
	if err != nil {
		return IntegrationHealth{}, fmt.Errorf("record procurement integration check: %w", err)
	}
	return item, nil
}

func safeError(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if len([]rune(value)) > 500 {
		return string([]rune(value)[:500]) + "…"
	}
	return value
}

func (store *PostgresStore) loadSettings(ctx context.Context) (PricingSettings, error) {
	var item PricingSettings
	err := store.pool.QueryRow(ctx, `
		SELECT version, default_exchange_rate::DOUBLE PRECISION,
			trolley_cost_currency::DOUBLE PRECISION, trolley_cost_rub::DOUBLE PRECISION, trolley_volume_cm3::DOUBLE PRECISION,
			trolley_fill_ratio::DOUBLE PRECISION, return_loss_rate::DOUBLE PRECISION,
			marketplace_cost_rate::DOUBLE PRECISION, tax_rate::DOUBLE PRECISION,
			reserve_rate::DOUBLE PRECISION, package_rub::DOUBLE PRECISION,
			price_change_threshold::DOUBLE PRECISION, domestic_retail_multiplier::DOUBLE PRECISION,
			international_cost_multiplier::DOUBLE PRECISION, international_retail_multiplier::DOUBLE PRECISION,
			marketplace_strike_markup::DOUBLE PRECISION, retail_round_step,
			avoid_round_hundreds, recommendation_days, target_cover_days,
			retail_markup_multiplier::DOUBLE PRECISION, round_prices,
			marketplace_logistics_per_cm::DOUBLE PRECISION
		FROM procurement_pricing_settings WHERE id = 1
	`).Scan(
		&item.Version, &item.DefaultExchangeRate, &item.TrolleyCostCurrency, &item.TrolleyCostRUB,
		&item.TrolleyVolumeCM3, &item.TrolleyFillRatio, &item.ReturnLossRate,
		&item.MarketplaceCostRate, &item.TaxRate, &item.ReserveRate, &item.PackageRUB,
		&item.PriceChangeThreshold, &item.DomesticRetailMultiplier,
		&item.InternationalCostMultiplier, &item.InternationalRetailMultiplier,
		&item.MarketplaceStrikeMarkup, &item.RetailRoundStep, &item.AvoidRoundHundreds,
		&item.RecommendationDays, &item.TargetCoverDays,
		&item.RetailMarkupMultiplier, &item.RoundPrices,
		&item.MarketplaceLogisticsPerCM,
	)
	if err != nil {
		return PricingSettings{}, fmt.Errorf("load procurement pricing settings: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) UpdateSettings(ctx context.Context, actor Actor, input PricingSettings) (PricingSettings, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return PricingSettings{}, fmt.Errorf("begin update procurement settings: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var item PricingSettings
	err = tx.QueryRow(ctx, `
		UPDATE procurement_pricing_settings SET
			version = version + 1, default_exchange_rate = $1, trolley_cost_currency = $2, trolley_cost_rub = $3,
			trolley_volume_cm3 = $4, trolley_fill_ratio = $5, return_loss_rate = $6,
			marketplace_cost_rate = $7, tax_rate = $8, reserve_rate = $9, package_rub = $10,
			price_change_threshold = $11, domestic_retail_multiplier = $12,
			international_cost_multiplier = $13, international_retail_multiplier = $14,
			marketplace_strike_markup = $15, retail_round_step = $16,
			avoid_round_hundreds = $17, recommendation_days = $18, target_cover_days = $19,
			retail_markup_multiplier = $20, round_prices = $21,
			marketplace_logistics_per_cm = $22,
			updated_by = $23, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
		RETURNING version, default_exchange_rate::DOUBLE PRECISION,
			trolley_cost_currency::DOUBLE PRECISION, trolley_cost_rub::DOUBLE PRECISION, trolley_volume_cm3::DOUBLE PRECISION,
			trolley_fill_ratio::DOUBLE PRECISION, return_loss_rate::DOUBLE PRECISION,
			marketplace_cost_rate::DOUBLE PRECISION, tax_rate::DOUBLE PRECISION,
			reserve_rate::DOUBLE PRECISION, package_rub::DOUBLE PRECISION,
			price_change_threshold::DOUBLE PRECISION, domestic_retail_multiplier::DOUBLE PRECISION,
			international_cost_multiplier::DOUBLE PRECISION, international_retail_multiplier::DOUBLE PRECISION,
			marketplace_strike_markup::DOUBLE PRECISION, retail_round_step,
			avoid_round_hundreds, recommendation_days, target_cover_days,
			retail_markup_multiplier::DOUBLE PRECISION, round_prices,
			marketplace_logistics_per_cm::DOUBLE PRECISION
	`, input.DefaultExchangeRate, input.TrolleyCostCurrency, input.TrolleyCostRUB, input.TrolleyVolumeCM3,
		input.TrolleyFillRatio, input.ReturnLossRate, input.MarketplaceCostRate,
		input.TaxRate, input.ReserveRate, input.PackageRUB, input.PriceChangeThreshold,
		input.DomesticRetailMultiplier, input.InternationalCostMultiplier,
		input.InternationalRetailMultiplier, input.MarketplaceStrikeMarkup,
		input.RetailRoundStep, input.AvoidRoundHundreds, input.RecommendationDays,
		input.TargetCoverDays, input.RetailMarkupMultiplier, input.RoundPrices,
		input.MarketplaceLogisticsPerCM, actor.CustomerID,
	).Scan(
		&item.Version, &item.DefaultExchangeRate, &item.TrolleyCostCurrency, &item.TrolleyCostRUB,
		&item.TrolleyVolumeCM3, &item.TrolleyFillRatio, &item.ReturnLossRate,
		&item.MarketplaceCostRate, &item.TaxRate, &item.ReserveRate, &item.PackageRUB,
		&item.PriceChangeThreshold, &item.DomesticRetailMultiplier,
		&item.InternationalCostMultiplier, &item.InternationalRetailMultiplier,
		&item.MarketplaceStrikeMarkup, &item.RetailRoundStep, &item.AvoidRoundHundreds,
		&item.RecommendationDays, &item.TargetCoverDays,
		&item.RetailMarkupMultiplier, &item.RoundPrices,
		&item.MarketplaceLogisticsPerCM,
	)
	if err != nil {
		return PricingSettings{}, fmt.Errorf("update procurement settings: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.settings.update", "procurement_settings", 1, item); err != nil {
		return PricingSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PricingSettings{}, fmt.Errorf("commit procurement settings: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) CreateSupplier(
	ctx context.Context,
	actor Actor,
	input SupplierCreate,
) (Supplier, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Supplier{}, fmt.Errorf("begin create supplier: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var item Supplier
	err = tx.QueryRow(ctx, `
		INSERT INTO procurement_suppliers (name, kind, country_code, default_currency)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, kind, country_code, default_currency, active, created_at
	`, input.Name, input.Kind, input.CountryCode, input.DefaultCurrency).Scan(
		&item.ID, &item.Name, &item.Kind, &item.CountryCode,
		&item.DefaultCurrency, &item.Active, &item.CreatedAt,
	)
	if err != nil {
		if uniqueViolation(err) {
			return Supplier{}, ErrInvalidInput
		}
		return Supplier{}, fmt.Errorf("insert procurement supplier: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.supplier.create", "procurement_supplier", item.ID, item); err != nil {
		return Supplier{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Supplier{}, fmt.Errorf("commit create supplier: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) DeleteSupplier(ctx context.Context, actor Actor, supplierID int64) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete supplier: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var item Supplier
	err = tx.QueryRow(ctx, `
		SELECT id, name, kind, country_code, default_currency, active, created_at
		FROM procurement_suppliers WHERE id = $1 FOR UPDATE
	`, supplierID).Scan(&item.ID, &item.Name, &item.Kind, &item.CountryCode, &item.DefaultCurrency, &item.Active, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load procurement supplier for deletion: %w", err)
	}

	var used bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM procurement_orders WHERE supplier_id = $1)
		    OR EXISTS (SELECT 1 FROM procurement_documents WHERE supplier_id = $1)
	`, supplierID).Scan(&used)
	if err != nil {
		return fmt.Errorf("check procurement supplier usage: %w", err)
	}
	if used {
		return ErrSupplierInUse
	}
	if err := audit(ctx, tx, actor, "procurement.supplier.delete", "procurement_supplier", supplierID, item); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_suppliers WHERE id = $1`, supplierID); err != nil {
		return fmt.Errorf("delete procurement supplier: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete supplier: %w", err)
	}
	return nil
}

func (store *PostgresStore) CreateOrder(
	ctx context.Context,
	actor Actor,
	input OrderCreate,
) (OrderSummary, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return OrderSummary{}, fmt.Errorf("begin create procurement order: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var item OrderSummary
	err = tx.QueryRow(ctx, `
		INSERT INTO procurement_orders (
			supplier_id, order_number, source_kind, currency, notes, created_by
		)
		SELECT id, $2, $3, $4, $5, $6
		FROM procurement_suppliers
		WHERE id = $1 AND active = TRUE
		RETURNING id, supplier_id, order_number, document_number, document_date,
			source_kind, currency, status, created_at
	`, input.SupplierID, input.OrderNumber, input.SourceKind, input.Currency, input.Notes, actor.CustomerID).Scan(
		&item.ID, &item.SupplierID, &item.OrderNumber, &item.DocumentNumber,
		&item.DocumentDate, &item.SourceKind, &item.Currency, &item.Status, &item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderSummary{}, ErrNotFound
	}
	if err != nil {
		return OrderSummary{}, fmt.Errorf("insert procurement order: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT name FROM procurement_suppliers WHERE id = $1`, item.SupplierID).Scan(&item.SupplierName); err != nil {
		return OrderSummary{}, fmt.Errorf("load procurement supplier name: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.order.create", "procurement_order", item.ID, item); err != nil {
		return OrderSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrderSummary{}, fmt.Errorf("commit create procurement order: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) CreatePlan(ctx context.Context, actor Actor, input PlanCreate) (OrderSummary, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return OrderSummary{}, fmt.Errorf("begin create procurement plan: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var orderID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO procurement_orders (supplier_id, order_number, source_kind, currency, status, created_by)
		SELECT id, $2, 'recommendation', default_currency, 'ordered', $3
		FROM procurement_suppliers WHERE id = $1 AND active = TRUE RETURNING id
	`, input.SupplierID, input.OrderNumber, actor.CustomerID).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderSummary{}, ErrNotFound
	}
	if err != nil {
		return OrderSummary{}, fmt.Errorf("insert procurement plan: %w", err)
	}
	for _, source := range input.Items {
		command, err := tx.Exec(ctx, `
			INSERT INTO procurement_order_lines (
				procurement_order_id, supplier_alias_id, saby_id, raw_name,
				supplier_article, ordered_qty, expected_unit_price, match_status, customer_request
			)
			SELECT $1, alias.id, n.saby_id, n.name,
				COALESCE(alias.supplier_article, ''), $4, NULLIF($5, 0), 'confirmed',
				EXISTS (SELECT 1 FROM procurement_requests r WHERE r.saby_id = n.saby_id AND r.status = 'open')
			FROM saby_nomenclature n
			LEFT JOIN LATERAL (
				SELECT id, supplier_article FROM procurement_supplier_aliases
				WHERE supplier_id = $2 AND matched_saby_id = n.saby_id AND match_status = 'confirmed'
				ORDER BY last_seen_at DESC NULLS LAST, id DESC LIMIT 1
			) alias ON TRUE
			WHERE n.saby_id = $3
		`, orderID, input.SupplierID, source.SabyID, source.Quantity, source.ExpectedUnitPrice)
		if err != nil {
			return OrderSummary{}, fmt.Errorf("insert procurement plan line: %w", err)
		}
		if command.RowsAffected() == 0 {
			return OrderSummary{}, ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_requests SET status = 'included', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'open' AND saby_id = ANY($1::TEXT[])
	`, planSabyIDs(input.Items)); err != nil {
		return OrderSummary{}, fmt.Errorf("include procurement requests in plan: %w", err)
	}
	order, err := loadOrderSummary(ctx, tx, orderID)
	if err != nil {
		return OrderSummary{}, err
	}
	if err := audit(ctx, tx, actor, "procurement.plan.create", "procurement_order", orderID, input); err != nil {
		return OrderSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrderSummary{}, fmt.Errorf("commit procurement plan: %w", err)
	}
	return order, nil
}

func (store *PostgresStore) OrderDetail(ctx context.Context, orderID int64) (OrderDetail, error) {
	order, err := loadOrderSummary(ctx, store.pool, orderID)
	if err != nil {
		return OrderDetail{}, err
	}
	settings, err := store.loadSettings(ctx)
	if err != nil {
		return OrderDetail{}, err
	}
	var detail OrderDetail
	detail.Order = order
	err = store.pool.QueryRow(ctx, `
		SELECT COALESCE(exchange_rate, 0)::DOUBLE PRECISION,
			trolley_cost_currency::DOUBLE PRECISION, trolley_cost_rub::DOUBLE PRECISION,
			delivery_to_moscow_rub::DOUBLE PRECISION, delivery_to_ryazan_rub::DOUBLE PRECISION
		FROM procurement_orders WHERE id = $1
	`, orderID).Scan(&detail.Costs.ExchangeRate, &detail.Costs.TrolleyCostCurrency, &detail.Costs.TrolleyCostRUB,
		&detail.Costs.DeliveryToMoscowRUB, &detail.Costs.DeliveryToRyazanRUB)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("load procurement order costs: %w", err)
	}
	rows, err := store.pool.Query(ctx, `
		SELECT l.id, COALESCE(l.saby_id, ''), COALESCE(n.name, ''), l.raw_name,
			l.supplier_article, COALESCE(l.invoiced_qty, l.ordered_qty),
			l.ordered_qty, l.invoiced_qty,
			COALESCE(l.unit_price, l.expected_unit_price, 0)::DOUBLE PRECISION,
			l.expected_unit_price::DOUBLE PRECISION, l.load_unit,
			l.pot_diameter_cm::DOUBLE PRECISION, l.height_cm::DOUBLE PRECISION, l.match_status,
			l.purchase_unit_rub::DOUBLE PRECISION, l.trolley_delivery_unit_rub::DOUBLE PRECISION,
			l.ryazan_delivery_unit_rub::DOUBLE PRECISION, l.unit_cost_rub::DOUBLE PRECISION,
			COALESCE(n.price_minor, 0)::DOUBLE PRECISION / 100,
			l.proposed_retail_rub, l.proposed_marketplace_rub,
			l.proposed_marketplace_strike_rub, l.customer_request,
			l.comparison_accepted, l.comparison_note
		FROM procurement_order_lines l
		LEFT JOIN saby_nomenclature n ON n.saby_id = l.saby_id
		WHERE l.procurement_order_id = $1
		ORDER BY l.load_unit, l.id
	`, orderID)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("query procurement order lines: %w", err)
	}
	defer rows.Close()
	detail.Lines = make([]OrderLine, 0)
	for rows.Next() {
		var line OrderLine
		if err := rows.Scan(&line.ID, &line.SabyID, &line.SabyName, &line.RawName,
			&line.SupplierArticle, &line.Quantity, &line.OrderedQuantity, &line.InvoicedQuantity,
			&line.UnitPrice, &line.ExpectedUnitPrice, &line.LoadUnit,
			&line.PotDiameterCM, &line.HeightCM, &line.MatchStatus,
			&line.PurchaseUnitRUB, &line.TrolleyDeliveryUnitRUB, &line.RyazanDeliveryUnitRUB,
			&line.UnitCostRUB, &line.CurrentRetailRUB, &line.ProposedRetailRUB,
			&line.ProposedMarketplaceRUB, &line.ProposedMarketplaceStrikeRUB,
			&line.CustomerRequest, &line.ComparisonAccepted, &line.ComparisonNote); err != nil {
			return OrderDetail{}, fmt.Errorf("scan procurement order line: %w", err)
		}
		if line.ProposedRetailRUB != nil {
			line.PriceChangeNeeded = priceChangeNeeded(line.CurrentRetailRUB, *line.ProposedRetailRUB, settings.PriceChangeThreshold)
		}
		detail.Lines = append(detail.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return OrderDetail{}, err
	}
	type comparisonGroup struct {
		ordered, invoiced       int
		expected                *float64
		priceMismatch, accepted bool
		first                   int
	}
	groups := make(map[string]*comparisonGroup)
	for index := range detail.Lines {
		line := &detail.Lines[index]
		if line.MatchStatus != "confirmed" || line.SabyID == "" {
			continue
		}
		group := groups[line.SabyID]
		if group == nil {
			group = &comparisonGroup{first: index}
			groups[line.SabyID] = group
		}
		group.ordered += line.OrderedQuantity
		if line.InvoicedQuantity != nil {
			group.invoiced += *line.InvoicedQuantity
		}
		if line.ExpectedUnitPrice != nil {
			value := *line.ExpectedUnitPrice
			group.expected = &value
		}
		group.accepted = group.accepted || line.ComparisonAccepted
	}
	for sabyID, group := range groups {
		if group.expected != nil {
			for index := range detail.Lines {
				line := detail.Lines[index]
				if line.SabyID == sabyID && line.InvoicedQuantity != nil && math.Abs(line.UnitPrice-*group.expected) > .005 {
					group.priceMismatch = true
				}
			}
		}
		mismatch := group.ordered > 0 && group.ordered != group.invoiced || group.priceMismatch
		detail.Lines[group.first].ComparisonMismatch = mismatch
		detail.Lines[group.first].ComparisonAccepted = group.accepted
	}
	detail.Batches, err = store.listBatches(ctx, orderID)
	if err != nil {
		return OrderDetail{}, err
	}
	detail.Validation, err = store.loadOrderValidation(ctx, orderID, detail)
	if err != nil {
		return OrderDetail{}, err
	}
	return detail, nil
}

func (store *PostgresStore) loadOrderValidation(ctx context.Context, orderID int64, detail OrderDetail) (OrderValidation, error) {
	result := OrderValidation{Blockers: make([]string, 0)}
	var kind, status string
	var documents int
	if err := store.pool.QueryRow(ctx, `
		SELECT s.kind, o.status,
			COUNT(d.id)::INTEGER,
			COUNT(d.id) FILTER (WHERE d.arithmetic_status <> 'ok')::INTEGER,
			COUNT(DISTINCT NULLIF(l.load_unit, '')) FILTER (WHERE l.match_status = 'confirmed')::INTEGER
		FROM procurement_orders o
		JOIN procurement_suppliers s ON s.id = o.supplier_id
		LEFT JOIN procurement_documents d ON d.procurement_order_id = o.id
		LEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id
		WHERE o.id = $1 GROUP BY s.kind, o.status
	`, orderID).Scan(&kind, &status, &documents, &result.ArithmeticMismatch, &result.TrolleyCount); err != nil {
		return OrderValidation{}, fmt.Errorf("validate procurement order: %w", err)
	}
	// The joins above multiply documents by lines; use an exact document count.
	if err := store.pool.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER, COUNT(*) FILTER (WHERE arithmetic_status <> 'ok')::INTEGER
		FROM procurement_documents WHERE procurement_order_id = $1
	`, orderID).Scan(&documents, &result.ArithmeticMismatch); err != nil {
		return OrderValidation{}, fmt.Errorf("validate procurement documents: %w", err)
	}
	for _, line := range detail.Lines {
		if line.MatchStatus != "confirmed" && line.MatchStatus != "ignored" {
			result.Unmatched++
		}
		if line.MatchStatus != "confirmed" {
			continue
		}
		if line.InvoicedQuantity == nil || line.Quantity <= 0 || line.UnitPrice <= 0 {
			result.InvalidLines++
		}
		if line.ComparisonMismatch && !line.ComparisonAccepted {
			result.ComparisonMismatch++
		}
		if kind == KindInternational && (line.PotDiameterCM == nil || *line.PotDiameterCM <= 0 || line.HeightCM == nil || *line.HeightCM <= 0) {
			result.MissingDimensions++
		}
		if kind == KindInternational && strings.TrimSpace(line.LoadUnit) == "" {
			result.MissingLoadUnits++
		}
		quantity := float64(line.Quantity)
		if line.TrolleyDeliveryUnitRUB != nil {
			result.AllocatedTrolleyRUB += *line.TrolleyDeliveryUnitRUB * quantity
		}
		if line.RyazanDeliveryUnitRUB != nil {
			result.AllocatedRyazanRUB += *line.RyazanDeliveryUnitRUB * quantity
		}
	}
	if kind == KindInternational {
		result.ExpectedTrolleyRUB = detail.Costs.DeliveryToMoscowRUB
		if result.ExpectedTrolleyRUB == 0 {
			result.ExpectedTrolleyRUB = float64(result.TrolleyCount) * detail.Costs.TrolleyCostRUB
		}
	}
	result.ExpectedRyazanRUB = detail.Costs.DeliveryToRyazanRUB
	if documents == 0 {
		result.Blockers = append(result.Blockers, "Не загружен инвойс или счёт")
	}
	if result.ArithmeticMismatch > 0 {
		result.Blockers = append(result.Blockers, "Итог документа не совпадает с суммой строк")
	}
	if result.Unmatched > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("Не сопоставлено строк: %d", result.Unmatched))
	}
	if result.ComparisonMismatch > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("Не подтверждено расхождений заказа и инвойса: %d", result.ComparisonMismatch))
	}
	if result.MissingDimensions > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("Не заполнены размеры: %d", result.MissingDimensions))
	}
	if result.MissingLoadUnits > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("Не указана телега: %d", result.MissingLoadUnits))
	}
	if result.InvalidLines > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("В инвойсе нет количества или цены: %d", result.InvalidLines))
	}
	result.CanCalculate = status != "received" && status != "cancelled" && len(result.Blockers) == 0
	result.CanPrepareActions = status == "ready_to_receive" && result.CanCalculate && detail.Order.Status == "ready_to_receive"
	return result, nil
}

func (store *PostgresStore) CalculateOrder(ctx context.Context, actor Actor, orderID int64, input CalculationInput) (OrderDetail, error) {
	settings, err := store.loadSettings(ctx)
	if err != nil {
		return OrderDetail{}, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("begin procurement calculation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var kind, status string
	if err := tx.QueryRow(ctx, `
		SELECT s.kind, o.status FROM procurement_orders o
		JOIN procurement_suppliers s ON s.id = o.supplier_id
		WHERE o.id = $1 FOR UPDATE OF o
	`, orderID).Scan(&kind, &status); errors.Is(err, pgx.ErrNoRows) {
		return OrderDetail{}, ErrNotFound
	} else if err != nil {
		return OrderDetail{}, fmt.Errorf("lock procurement order: %w", err)
	}
	if status == "received" || status == "cancelled" {
		return OrderDetail{}, ErrInvalidInput
	}
	var activeApproved bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM procurement_action_batches
			WHERE procurement_order_id = $1 AND status NOT IN ('draft', 'cancelled', 'completed'))
	`, orderID).Scan(&activeApproved); err != nil {
		return OrderDetail{}, fmt.Errorf("check active procurement actions: %w", err)
	}
	if activeApproved {
		return OrderDetail{}, ErrInvalidInput
	}
	type sourceLine struct {
		id                            int64
		quantity, orderedQty          int
		unitPrice, pot, height        float64
		expectedPrice                 *float64
		invoicedQty                   *int
		matchStatus, loadUnit, sabyID string
		comparisonAccepted            bool
	}
	rows, err := tx.Query(ctx, `
		SELECT id, COALESCE(invoiced_qty, ordered_qty), COALESCE(unit_price, expected_unit_price, 0)::DOUBLE PRECISION,
			COALESCE(pot_diameter_cm, 0)::DOUBLE PRECISION,
			COALESCE(height_cm, 0)::DOUBLE PRECISION, match_status, load_unit,
			expected_unit_price::DOUBLE PRECISION, invoiced_qty, comparison_accepted, ordered_qty,
			COALESCE(saby_id, '')
		FROM procurement_order_lines WHERE procurement_order_id = $1 FOR UPDATE
	`, orderID)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("lock procurement lines: %w", err)
	}
	lines := make([]sourceLine, 0)
	totalHeightUnits := 0.0
	loadVolumes := make(map[string]float64)
	for rows.Next() {
		var line sourceLine
		if err := rows.Scan(&line.id, &line.quantity, &line.unitPrice, &line.pot, &line.height, &line.matchStatus,
			&line.loadUnit, &line.expectedPrice, &line.invoicedQty, &line.comparisonAccepted, &line.orderedQty, &line.sabyID); err != nil {
			rows.Close()
			return OrderDetail{}, fmt.Errorf("scan procurement calculation line: %w", err)
		}
		if line.matchStatus != "confirmed" && line.matchStatus != "ignored" {
			rows.Close()
			return OrderDetail{}, ErrInvalidInput
		}
		if line.matchStatus == "confirmed" {
			if line.quantity <= 0 || line.unitPrice <= 0 || line.invoicedQty == nil {
				rows.Close()
				return OrderDetail{}, ErrInvalidInput
			}
			if kind == KindInternational && (line.pot <= 0 || line.height <= 0 || strings.TrimSpace(line.loadUnit) == "") {
				rows.Close()
				return OrderDetail{}, ErrInvalidInput
			}
			totalHeightUnits += line.height * float64(line.quantity)
			if kind == KindInternational {
				volume := math.Pi * math.Pow(line.pot/2, 2) * line.height
				loadVolumes[line.loadUnit] += volume * float64(line.quantity)
			}
		}
		lines = append(lines, line)
	}
	rows.Close()
	if len(lines) == 0 {
		return OrderDetail{}, ErrInvalidInput
	}
	type calculationComparison struct {
		ordered, invoiced int
		expected          *float64
		prices            []float64
		accepted          bool
	}
	comparisons := make(map[string]*calculationComparison)
	deliveryToMoscowRUB := input.DeliveryToMoscowRUB
	if deliveryToMoscowRUB == 0 && input.TrolleyCostRUB > 0 {
		deliveryToMoscowRUB = input.TrolleyCostRUB * float64(len(loadVolumes))
	}
	perTrolleyRUB := 0.0
	perTrolleyRUB = deliveryPerTrolley(deliveryToMoscowRUB, len(loadVolumes))
	for _, line := range lines {
		if line.matchStatus != "confirmed" || line.sabyID == "" {
			continue
		}
		group := comparisons[line.sabyID]
		if group == nil {
			group = &calculationComparison{}
			comparisons[line.sabyID] = group
		}
		group.ordered += line.orderedQty
		if line.invoicedQty != nil {
			group.invoiced += *line.invoicedQty
		}
		if line.expectedPrice != nil {
			value := *line.expectedPrice
			group.expected = &value
		}
		group.prices = append(group.prices, line.unitPrice)
		group.accepted = group.accepted || line.comparisonAccepted
	}
	for _, group := range comparisons {
		mismatch := group.ordered > 0 && group.ordered != group.invoiced
		if group.expected != nil {
			for _, price := range group.prices {
				mismatch = mismatch || math.Abs(price-*group.expected) > .005
			}
		}
		if mismatch && !group.accepted {
			return OrderDetail{}, ErrInvalidInput
		}
	}
	var documentCount, arithmeticMismatch int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER, COUNT(*) FILTER (WHERE arithmetic_status <> 'ok')::INTEGER
		FROM procurement_documents WHERE procurement_order_id = $1
	`, orderID).Scan(&documentCount, &arithmeticMismatch); err != nil {
		return OrderDetail{}, fmt.Errorf("validate documents before calculation: %w", err)
	}
	if documentCount == 0 || arithmeticMismatch > 0 || (input.DeliveryToRyazanRUB > 0 && totalHeightUnits <= 0) {
		return OrderDetail{}, ErrInvalidInput
	}
	for _, line := range lines {
		if line.matchStatus == "ignored" {
			continue
		}
		trolleyPerUnit := 0.0
		if kind == KindInternational && loadVolumes[line.loadUnit] > 0 {
			unitVolume := math.Pi * math.Pow(line.pot/2, 2) * line.height
			trolleyPerUnit = perTrolleyRUB * unitVolume / loadVolumes[line.loadUnit]
		}
		ryazanPerUnit := 0.0
		if totalHeightUnits > 0 {
			ryazanPerUnit = input.DeliveryToRyazanRUB * line.height / totalHeightUnits
		}
		calculated := calculateAllocatedLine(
			settings, kind, line.unitPrice, input.ExchangeRate,
			trolleyPerUnit, ryazanPerUnit, line.height,
		)
		if _, err := tx.Exec(ctx, `
			UPDATE procurement_order_lines SET purchase_unit_rub = $2,
				trolley_delivery_unit_rub = $3, ryazan_delivery_unit_rub = $4,
				unit_cost_rub = $5, proposed_retail_rub = $6,
				proposed_marketplace_rub = $7, proposed_marketplace_strike_rub = $8,
				updated_at = CURRENT_TIMESTAMP WHERE id = $1
		`, line.id, calculated.PurchaseUnitRUB, calculated.TrolleyDeliveryUnitRUB,
			calculated.RyazanDeliveryUnitRUB, calculated.UnitCostRUB,
			calculated.ProposedRetailRUB, calculated.ProposedMarketplaceRUB,
			calculated.ProposedMarketplaceStrikeRUB); err != nil {
			return OrderDetail{}, fmt.Errorf("save procurement calculation line: %w", err)
		}
	}
	snapshot, err := json.Marshal(settings)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("encode procurement calculation settings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_batches SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
		WHERE procurement_order_id = $1 AND status = 'draft'
	`, orderID); err != nil {
		return OrderDetail{}, fmt.Errorf("cancel stale procurement batches: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_orders SET exchange_rate = $2, trolley_cost_currency = $3, trolley_cost_rub = $4,
			delivery_to_moscow_rub = $5, delivery_to_ryazan_rub = $6, calculation_version = $7,
			calculation_settings = $8, calculated_at = CURRENT_TIMESTAMP,
			status = 'ready_to_receive', updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, orderID, input.ExchangeRate, input.TrolleyCostCurrency, perTrolleyRUB, deliveryToMoscowRUB,
		input.DeliveryToRyazanRUB, settings.Version, snapshot); err != nil {
		return OrderDetail{}, fmt.Errorf("save procurement calculation: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.order.calculate", "procurement_order", orderID, input); err != nil {
		return OrderDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrderDetail{}, fmt.Errorf("commit procurement calculation: %w", err)
	}
	return store.OrderDetail(ctx, orderID)
}

func (store *PostgresStore) ImportDocument(
	ctx context.Context,
	actor Actor,
	input DocumentUpload,
	parsed ParsedDocument,
) (ImportResult, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ImportResult{}, fmt.Errorf("begin import procurement document: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var supplierName, supplierKind, supplierCurrency string
	if err := tx.QueryRow(ctx, `
		SELECT name, kind, default_currency FROM procurement_suppliers WHERE id = $1 AND active = TRUE FOR SHARE
	`, input.SupplierID).Scan(&supplierName, &supplierKind, &supplierCurrency); errors.Is(err, pgx.ErrNoRows) {
		return ImportResult{}, ErrNotFound
	} else if err != nil {
		return ImportResult{}, fmt.Errorf("load procurement supplier: %w", err)
	}
	if supplierKind == KindDomestic && (parsed.ParserKind != "domestic_payment_invoice" || parsed.Currency != "RUB") ||
		supplierKind == KindInternational && (parsed.ParserKind != "holland_packing_list" || parsed.Currency != supplierCurrency) {
		return ImportResult{}, ErrUnsupportedDocument
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(input.Content))
	if existing, order, found, err := loadDocumentByHash(ctx, tx, input.SupplierID, hash); err != nil {
		return ImportResult{}, err
	} else if found {
		return ImportResult{Document: existing, Order: order, Duplicate: true}, nil
	}

	orderID := input.OrderID
	sourceKind := SourceInvoice
	if parsed.ParserKind == "domestic_payment_invoice" {
		sourceKind = SourcePaymentInvoice
	}
	if orderID == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO procurement_orders (
				supplier_id, order_number, document_number, document_date,
				source_kind, currency, status, created_by
			) VALUES ($1, $2, $2, $3, $4, $5, 'invoice_received', $6)
			RETURNING id
		`, input.SupplierID, parsed.DocumentNumber, parsed.DocumentDate, sourceKind, parsed.Currency, actor.CustomerID).Scan(&orderID)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE procurement_orders SET
				document_number = $3, document_date = $4, source_kind = $5,
				currency = $6, status = 'invoice_received', updated_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND supplier_id = $2 AND status NOT IN ('received', 'cancelled')
			RETURNING id
		`, orderID, input.SupplierID, parsed.DocumentNumber, parsed.DocumentDate, sourceKind, parsed.Currency).Scan(&orderID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportResult{}, ErrNotFound
	}
	if err != nil {
		return ImportResult{}, fmt.Errorf("create or attach procurement order: %w", err)
	}

	arithmeticStatus := "mismatch"
	if parsed.ArithmeticOK {
		arithmeticStatus = "ok"
	}
	var document DocumentSummary
	err = tx.QueryRow(ctx, `
		INSERT INTO procurement_documents (
			supplier_id, procurement_order_id, file_name, content_type, size_bytes,
			sha256, content, parser_kind, parser_version, parse_status,
			arithmetic_status, document_number, document_date, currency,
			line_count, unit_count, product_subtotal, package_total,
			document_total, calculated_total, extracted_text, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, 'review', $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20, $21
		)
		RETURNING id, supplier_id, procurement_order_id, file_name, parser_kind,
			parse_status, arithmetic_status, document_number, document_date, currency,
			line_count, unit_count, product_subtotal::DOUBLE PRECISION,
			package_total::DOUBLE PRECISION, document_total::DOUBLE PRECISION,
			calculated_total::DOUBLE PRECISION, parse_error, created_at
	`, input.SupplierID, orderID, input.FileName, input.ContentType, len(input.Content),
		hash, input.Content, parsed.ParserKind, parserVersion, arithmeticStatus,
		parsed.DocumentNumber, parsed.DocumentDate, parsed.Currency, len(parsed.Lines),
		countUnits(parsed.Lines), parsed.ProductSubtotal, parsed.PackageTotal,
		parsed.DocumentTotal, parsed.CalculatedTotal, parsed.ExtractedText, actor.CustomerID,
	).Scan(
		&document.ID, &document.SupplierID, &document.OrderID, &document.FileName,
		&document.ParserKind, &document.ParseStatus, &document.ArithmeticStatus,
		&document.DocumentNumber, &document.DocumentDate, &document.Currency,
		&document.Lines, &document.Units, &document.ProductSubtotal,
		&document.PackageTotal, &document.DocumentTotal, &document.CalculatedTotal,
		&document.ParseError, &document.CreatedAt,
	)
	if err != nil {
		if uniqueViolation(err) {
			return ImportResult{}, ErrDuplicate
		}
		return ImportResult{}, fmt.Errorf("insert procurement document: %w", err)
	}
	document.SupplierName = supplierName

	unmatched := 0
	for _, line := range parsed.Lines {
		aliasID, sabyID, matchStatus, err := upsertAlias(ctx, tx, input.SupplierID, line, parsed.DocumentDate)
		if err != nil {
			return ImportResult{}, err
		}
		if matchStatus != "confirmed" && matchStatus != "ignored" {
			unmatched++
		}
		var reconciledID int64
		if sabyID != "" {
			err = tx.QueryRow(ctx, `
				UPDATE procurement_order_lines SET procurement_document_id = $2,
					supplier_alias_id = $3, raw_name = $5, supplier_article = $6,
					invoiced_qty = $7, unit_price = $8, line_total = $9, load_unit = $10,
					match_status = $11, source_page = $12, source_line = $13,
					pot_diameter_cm = $14, height_cm = $15, updated_at = CURRENT_TIMESTAMP
				WHERE id = (
					SELECT id FROM procurement_order_lines WHERE procurement_order_id = $1
						AND saby_id = $4 AND procurement_document_id IS NULL
					ORDER BY id LIMIT 1 FOR UPDATE
				) RETURNING id
			`, orderID, document.ID, aliasID, sabyID, line.RawName, line.SupplierArticle,
				line.Quantity, line.UnitPrice, line.LineTotal, line.LoadUnit, matchStatus,
				line.SourcePage, line.SourceLine, line.PotDiameterCM, line.HeightCM,
			).Scan(&reconciledID)
		}
		if sabyID == "" || errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx, `
				INSERT INTO procurement_order_lines (
					procurement_order_id, procurement_document_id, supplier_alias_id, saby_id,
					raw_name, supplier_article, invoiced_qty, unit_price, line_total,
					load_unit, match_status, source_page, source_line, pot_diameter_cm, height_cm
				) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			`, orderID, document.ID, aliasID, sabyID, line.RawName, line.SupplierArticle,
				line.Quantity, line.UnitPrice, line.LineTotal, line.LoadUnit, matchStatus,
				line.SourcePage, line.SourceLine, line.PotDiameterCM, line.HeightCM,
			)
		}
		if err != nil {
			return ImportResult{}, fmt.Errorf("reconcile procurement document line: %w", err)
		}
	}

	parseStatus, orderStatus := "parsed", "invoice_received"
	var comparisonMismatches int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER FROM procurement_order_lines
		WHERE procurement_order_id = $1 AND procurement_document_id = $2
			AND ordered_qty > 0 AND (
				invoiced_qty IS DISTINCT FROM ordered_qty OR
				(expected_unit_price IS NOT NULL AND unit_price IS DISTINCT FROM expected_unit_price)
			)
	`, orderID, document.ID).Scan(&comparisonMismatches); err != nil {
		return ImportResult{}, fmt.Errorf("compare procurement plan with invoice: %w", err)
	}
	if unmatched > 0 || comparisonMismatches > 0 || !parsed.ArithmeticOK {
		parseStatus, orderStatus = "review", "review"
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_documents SET parse_status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, document.ID, parseStatus); err != nil {
		return ImportResult{}, fmt.Errorf("update procurement document status: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_orders SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, orderID, orderStatus); err != nil {
		return ImportResult{}, fmt.Errorf("update procurement order status: %w", err)
	}
	document.ParseStatus = parseStatus

	order, err := loadOrderSummary(ctx, tx, orderID)
	if err != nil {
		return ImportResult{}, err
	}
	if err := audit(ctx, tx, actor, "procurement.document.import", "procurement_document", document.ID, document); err != nil {
		return ImportResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportResult{}, fmt.Errorf("commit procurement document import: %w", err)
	}
	return ImportResult{Document: document, Order: order}, nil
}
