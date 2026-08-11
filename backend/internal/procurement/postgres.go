package procurement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
			(SELECT COUNT(*) FROM procurement_supplier_aliases WHERE availability_status = 'check' OR (check_after IS NOT NULL AND check_after <= CURRENT_DATE))::INTEGER,
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
	result.Settings = settings
	result.Suppliers, result.Orders, result.Documents, result.Review = suppliers, orders, documents, review
	result.Requests, result.Availability, result.Recommendations = requests, availability, recommendations
	return result, nil
}

func (store *PostgresStore) loadSettings(ctx context.Context) (PricingSettings, error) {
	var item PricingSettings
	err := store.pool.QueryRow(ctx, `
		SELECT version, default_exchange_rate::DOUBLE PRECISION,
			trolley_cost_currency::DOUBLE PRECISION, trolley_volume_cm3::DOUBLE PRECISION,
			trolley_fill_ratio::DOUBLE PRECISION, return_loss_rate::DOUBLE PRECISION,
			marketplace_cost_rate::DOUBLE PRECISION, tax_rate::DOUBLE PRECISION,
			reserve_rate::DOUBLE PRECISION, package_rub::DOUBLE PRECISION,
			price_change_threshold::DOUBLE PRECISION, domestic_retail_multiplier::DOUBLE PRECISION,
			international_cost_multiplier::DOUBLE PRECISION, international_retail_multiplier::DOUBLE PRECISION,
			marketplace_strike_markup::DOUBLE PRECISION, retail_round_step,
			avoid_round_hundreds, recommendation_days, target_cover_days
		FROM procurement_pricing_settings WHERE id = 1
	`).Scan(
		&item.Version, &item.DefaultExchangeRate, &item.TrolleyCostCurrency,
		&item.TrolleyVolumeCM3, &item.TrolleyFillRatio, &item.ReturnLossRate,
		&item.MarketplaceCostRate, &item.TaxRate, &item.ReserveRate, &item.PackageRUB,
		&item.PriceChangeThreshold, &item.DomesticRetailMultiplier,
		&item.InternationalCostMultiplier, &item.InternationalRetailMultiplier,
		&item.MarketplaceStrikeMarkup, &item.RetailRoundStep, &item.AvoidRoundHundreds,
		&item.RecommendationDays, &item.TargetCoverDays,
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
			version = version + 1, default_exchange_rate = $1, trolley_cost_currency = $2,
			trolley_volume_cm3 = $3, trolley_fill_ratio = $4, return_loss_rate = $5,
			marketplace_cost_rate = $6, tax_rate = $7, reserve_rate = $8, package_rub = $9,
			price_change_threshold = $10, domestic_retail_multiplier = $11,
			international_cost_multiplier = $12, international_retail_multiplier = $13,
			marketplace_strike_markup = $14, retail_round_step = $15,
			avoid_round_hundreds = $16, recommendation_days = $17, target_cover_days = $18,
			updated_by = $19, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
		RETURNING version, default_exchange_rate::DOUBLE PRECISION,
			trolley_cost_currency::DOUBLE PRECISION, trolley_volume_cm3::DOUBLE PRECISION,
			trolley_fill_ratio::DOUBLE PRECISION, return_loss_rate::DOUBLE PRECISION,
			marketplace_cost_rate::DOUBLE PRECISION, tax_rate::DOUBLE PRECISION,
			reserve_rate::DOUBLE PRECISION, package_rub::DOUBLE PRECISION,
			price_change_threshold::DOUBLE PRECISION, domestic_retail_multiplier::DOUBLE PRECISION,
			international_cost_multiplier::DOUBLE PRECISION, international_retail_multiplier::DOUBLE PRECISION,
			marketplace_strike_markup::DOUBLE PRECISION, retail_round_step,
			avoid_round_hundreds, recommendation_days, target_cover_days
	`, input.DefaultExchangeRate, input.TrolleyCostCurrency, input.TrolleyVolumeCM3,
		input.TrolleyFillRatio, input.ReturnLossRate, input.MarketplaceCostRate,
		input.TaxRate, input.ReserveRate, input.PackageRUB, input.PriceChangeThreshold,
		input.DomesticRetailMultiplier, input.InternationalCostMultiplier,
		input.InternationalRetailMultiplier, input.MarketplaceStrikeMarkup,
		input.RetailRoundStep, input.AvoidRoundHundreds, input.RecommendationDays,
		input.TargetCoverDays, actor.CustomerID,
	).Scan(
		&item.Version, &item.DefaultExchangeRate, &item.TrolleyCostCurrency,
		&item.TrolleyVolumeCM3, &item.TrolleyFillRatio, &item.ReturnLossRate,
		&item.MarketplaceCostRate, &item.TaxRate, &item.ReserveRate, &item.PackageRUB,
		&item.PriceChangeThreshold, &item.DomesticRetailMultiplier,
		&item.InternationalCostMultiplier, &item.InternationalRetailMultiplier,
		&item.MarketplaceStrikeMarkup, &item.RetailRoundStep, &item.AvoidRoundHundreds,
		&item.RecommendationDays, &item.TargetCoverDays,
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
			trolley_cost_currency::DOUBLE PRECISION, delivery_to_ryazan_rub::DOUBLE PRECISION
		FROM procurement_orders WHERE id = $1
	`, orderID).Scan(&detail.Costs.ExchangeRate, &detail.Costs.TrolleyCostCurrency, &detail.Costs.DeliveryToRyazanRUB)
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
			l.proposed_marketplace_strike_rub, l.customer_request
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
			&line.CustomerRequest); err != nil {
			return OrderDetail{}, fmt.Errorf("scan procurement order line: %w", err)
		}
		if line.ProposedRetailRUB != nil {
			line.PriceChangeNeeded = priceChangeNeeded(line.CurrentRetailRUB, *line.ProposedRetailRUB, settings.PriceChangeThreshold)
		}
		line.ComparisonMismatch = line.InvoicedQuantity != nil && line.OrderedQuantity > 0 && *line.InvoicedQuantity != line.OrderedQuantity
		if line.ExpectedUnitPrice != nil && line.InvoicedQuantity != nil && math.Abs(line.UnitPrice-*line.ExpectedUnitPrice) > .005 {
			line.ComparisonMismatch = true
		}
		detail.Lines = append(detail.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return OrderDetail{}, err
	}
	detail.Batches, err = store.listBatches(ctx, orderID)
	if err != nil {
		return OrderDetail{}, err
	}
	return detail, nil
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
	type sourceLine struct {
		id                     int64
		quantity               int
		unitPrice, pot, height float64
		matchStatus            string
	}
	rows, err := tx.Query(ctx, `
		SELECT id, COALESCE(invoiced_qty, ordered_qty), COALESCE(unit_price, expected_unit_price, 0)::DOUBLE PRECISION,
			COALESCE(pot_diameter_cm, 0)::DOUBLE PRECISION,
			COALESCE(height_cm, 0)::DOUBLE PRECISION, match_status
		FROM procurement_order_lines WHERE procurement_order_id = $1 FOR UPDATE
	`, orderID)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("lock procurement lines: %w", err)
	}
	lines := make([]sourceLine, 0)
	totalHeightUnits := 0.0
	for rows.Next() {
		var line sourceLine
		if err := rows.Scan(&line.id, &line.quantity, &line.unitPrice, &line.pot, &line.height, &line.matchStatus); err != nil {
			rows.Close()
			return OrderDetail{}, fmt.Errorf("scan procurement calculation line: %w", err)
		}
		if line.matchStatus != "confirmed" && line.matchStatus != "ignored" {
			rows.Close()
			return OrderDetail{}, ErrInvalidInput
		}
		if line.matchStatus == "confirmed" {
			totalHeightUnits += line.height * float64(line.quantity)
		}
		lines = append(lines, line)
	}
	rows.Close()
	if len(lines) == 0 {
		return OrderDetail{}, ErrInvalidInput
	}
	for _, line := range lines {
		if line.matchStatus == "ignored" {
			continue
		}
		calculated := calculateLine(settings, OrderCosts{
			ExchangeRate: input.ExchangeRate, TrolleyCostCurrency: input.TrolleyCostCurrency,
			DeliveryToRyazanRUB: input.DeliveryToRyazanRUB,
		}, calculationLine{Kind: kind, Quantity: line.quantity, UnitPrice: line.unitPrice,
			PotDiameterCM: line.pot, HeightCM: line.height}, totalHeightUnits)
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
		UPDATE procurement_orders SET exchange_rate = $2, trolley_cost_currency = $3,
			delivery_to_ryazan_rub = $4, calculation_version = $5,
			calculation_settings = $6, calculated_at = CURRENT_TIMESTAMP,
			status = 'ready_to_receive', updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, orderID, input.ExchangeRate, input.TrolleyCostCurrency, input.DeliveryToRyazanRUB,
		settings.Version, snapshot); err != nil {
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

	var supplierName string
	if err := tx.QueryRow(ctx, `
		SELECT name FROM procurement_suppliers WHERE id = $1 AND active = TRUE FOR SHARE
	`, input.SupplierID).Scan(&supplierName); errors.Is(err, pgx.ErrNoRows) {
		return ImportResult{}, ErrNotFound
	} else if err != nil {
		return ImportResult{}, fmt.Errorf("load procurement supplier: %w", err)
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

func (store *PostgresStore) listSuppliers(ctx context.Context) ([]Supplier, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, name, kind, country_code, default_currency, active, created_at
		FROM procurement_suppliers
		ORDER BY active DESC, name
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement suppliers: %w", err)
	}
	defer rows.Close()
	items := make([]Supplier, 0)
	for rows.Next() {
		var item Supplier
		if err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.CountryCode, &item.DefaultCurrency, &item.Active, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan procurement supplier: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) listOrders(ctx context.Context) ([]OrderSummary, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT o.id, o.supplier_id, s.name, o.order_number, o.document_number,
			o.document_date, o.source_kind, o.currency, o.status,
			COUNT(l.id)::INTEGER,
			COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty)), 0)::INTEGER,
			COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty) * COALESCE(l.unit_price, l.expected_unit_price, 0)), 0)::DOUBLE PRECISION,
			COUNT(l.id) FILTER (WHERE l.match_status IN ('unmatched', 'suggested', 'new_product'))::INTEGER,
			o.created_at
		FROM procurement_orders o
		JOIN procurement_suppliers s ON s.id = o.supplier_id
		LEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id
		GROUP BY o.id, s.name
		ORDER BY o.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement orders: %w", err)
	}
	defer rows.Close()
	items := make([]OrderSummary, 0)
	for rows.Next() {
		var item OrderSummary
		if err := rows.Scan(
			&item.ID, &item.SupplierID, &item.SupplierName, &item.OrderNumber,
			&item.DocumentNumber, &item.DocumentDate, &item.SourceKind, &item.Currency,
			&item.Status, &item.Lines, &item.Units, &item.Total, &item.Unmatched, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan procurement order: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) listDocuments(ctx context.Context) ([]DocumentSummary, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.id, d.supplier_id, s.name, COALESCE(d.procurement_order_id, 0),
			d.file_name, d.parser_kind, d.parse_status, d.arithmetic_status,
			d.document_number, d.document_date, d.currency, d.line_count, d.unit_count,
			COALESCE(d.product_subtotal, 0)::DOUBLE PRECISION,
			COALESCE(d.package_total, 0)::DOUBLE PRECISION,
			COALESCE(d.document_total, 0)::DOUBLE PRECISION,
			COALESCE(d.calculated_total, 0)::DOUBLE PRECISION,
			d.parse_error, d.created_at
		FROM procurement_documents d
		JOIN procurement_suppliers s ON s.id = d.supplier_id
		ORDER BY d.created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement documents: %w", err)
	}
	defer rows.Close()
	items := make([]DocumentSummary, 0)
	for rows.Next() {
		var item DocumentSummary
		if err := scanDocument(rows, &item); err != nil {
			return nil, fmt.Errorf("scan procurement document: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) listReview(ctx context.Context) ([]AliasReview, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT a.id, a.supplier_id, s.name, a.raw_name, a.supplier_article,
			a.pot_diameter_cm::DOUBLE PRECISION, a.height_cm::DOUBLE PRECISION,
			COALESCE(a.matched_saby_id, ''), COALESCE(n.name, ''),
			a.match_status, a.confidence::DOUBLE PRECISION, a.availability_status,
			a.last_seen_at
		FROM procurement_supplier_aliases a
		JOIN procurement_suppliers s ON s.id = a.supplier_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = a.matched_saby_id
		WHERE a.match_status IN ('unmatched', 'suggested', 'new_product')
		ORDER BY a.occurrences DESC, a.last_seen_at DESC NULLS LAST, a.id
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement alias review: %w", err)
	}
	defer rows.Close()
	items := make([]AliasReview, 0)
	for rows.Next() {
		var item AliasReview
		if err := rows.Scan(
			&item.ID, &item.SupplierID, &item.SupplierName, &item.RawName,
			&item.SupplierArticle, &item.PotDiameterCM, &item.HeightCM,
			&item.SuggestedSabyID, &item.SuggestedSabyName, &item.MatchStatus,
			&item.Confidence, &item.AvailabilityStatus, &item.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("scan procurement alias review: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) listAvailability(ctx context.Context) ([]AliasReview, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT a.id, a.supplier_id, s.name, a.raw_name, a.supplier_article,
			a.pot_diameter_cm::DOUBLE PRECISION, a.height_cm::DOUBLE PRECISION,
			COALESCE(a.matched_saby_id, ''), COALESCE(n.name, ''),
			a.match_status, a.confidence::DOUBLE PRECISION, a.availability_status,
			a.last_seen_at
		FROM procurement_supplier_aliases a
		JOIN procurement_suppliers s ON s.id = a.supplier_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = a.matched_saby_id
		WHERE a.availability_status IN ('check', 'temporarily_unavailable', 'discontinued')
			OR (a.check_after IS NOT NULL AND a.check_after <= CURRENT_DATE)
		ORDER BY CASE a.availability_status WHEN 'check' THEN 0 WHEN 'temporarily_unavailable' THEN 1 ELSE 2 END,
			a.check_after NULLS FIRST, a.last_seen_at DESC NULLS LAST
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement availability: %w", err)
	}
	defer rows.Close()
	items := make([]AliasReview, 0)
	for rows.Next() {
		var item AliasReview
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.SupplierName, &item.RawName,
			&item.SupplierArticle, &item.PotDiameterCM, &item.HeightCM,
			&item.SuggestedSabyID, &item.SuggestedSabyName, &item.MatchStatus,
			&item.Confidence, &item.AvailabilityStatus, &item.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan procurement availability: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) listRequests(ctx context.Context) ([]Request, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, kind, COALESCE(saby_id, ''), requested_name, quantity, status, notes, created_at
		FROM procurement_requests
		WHERE status IN ('open', 'included')
		ORDER BY CASE kind WHEN 'customer_order' THEN 0 ELSE 1 END, created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement requests: %w", err)
	}
	defer rows.Close()
	items := make([]Request, 0)
	for rows.Next() {
		var item Request
		if err := rows.Scan(&item.ID, &item.Kind, &item.SabyID, &item.RequestedName,
			&item.Quantity, &item.Status, &item.Notes, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan procurement request: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) listRecommendations(ctx context.Context, settings PricingSettings) ([]Recommendation, error) {
	rows, err := store.pool.Query(ctx, `
		WITH sales AS (
			SELECT pv.saby_id, COALESCE(SUM(oi.quantity), 0)::INTEGER AS units
			FROM order_items oi
			JOIN product_variants pv ON pv.id = oi.variant_id
			JOIN orders o ON o.id = oi.order_id
			WHERE o.created_at >= CURRENT_TIMESTAMP - ($1 * INTERVAL '1 day')
				AND o.status <> 'cancelled' AND pv.saby_id IS NOT NULL
			GROUP BY pv.saby_id
		), requests AS (
			SELECT saby_id, COALESCE(SUM(quantity), 0)::INTEGER AS units
			FROM procurement_requests WHERE status = 'open' AND saby_id IS NOT NULL GROUP BY saby_id
		), incoming AS (
			SELECT l.saby_id, COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty)), 0)::INTEGER AS units
			FROM procurement_order_lines l
			JOIN procurement_orders o ON o.id = l.procurement_order_id
			WHERE l.saby_id IS NOT NULL AND l.match_status = 'confirmed'
				AND o.status IN ('ordered', 'invoice_received', 'review', 'ready_to_receive')
			GROUP BY l.saby_id
		), aliases AS (
			SELECT DISTINCT ON (matched_saby_id) id, matched_saby_id, supplier_article, availability_status
			FROM procurement_supplier_aliases
			WHERE matched_saby_id IS NOT NULL
			ORDER BY matched_saby_id, last_seen_at DESC NULLS LAST
		)
		SELECT COALESCE(a.id, 0), n.saby_id, n.name, COALESCE(a.supplier_article, ''), n.balance,
			COALESCE(s.units, 0), COALESCE(r.units, 0),
			GREATEST(0, CEIL(COALESCE(s.units, 0)::NUMERIC * $2 / $1)::INTEGER + COALESCE(r.units, 0) - n.balance - COALESCE(i.units, 0))
		FROM saby_nomenclature n
		LEFT JOIN sales s ON s.saby_id = n.saby_id
		LEFT JOIN requests r ON r.saby_id = n.saby_id
		LEFT JOIN incoming i ON i.saby_id = n.saby_id
		LEFT JOIN aliases a ON a.matched_saby_id = n.saby_id
		WHERE (COALESCE(s.units, 0) > 0 OR COALESCE(r.units, 0) > 0)
			AND COALESCE(a.availability_status, 'unknown') NOT IN ('temporarily_unavailable', 'discontinued')
			AND GREATEST(0, CEIL(COALESCE(s.units, 0)::NUMERIC * $2 / $1)::INTEGER + COALESCE(r.units, 0) - n.balance - COALESCE(i.units, 0)) > 0
		ORDER BY GREATEST(0, CEIL(COALESCE(s.units, 0)::NUMERIC * $2 / $1)::INTEGER + COALESCE(r.units, 0) - n.balance - COALESCE(i.units, 0)) DESC,
			COALESCE(r.units, 0) DESC, n.name
		LIMIT 100
	`, settings.RecommendationDays, settings.TargetCoverDays)
	if err != nil {
		return nil, fmt.Errorf("query procurement recommendations: %w", err)
	}
	defer rows.Close()
	items := make([]Recommendation, 0)
	for rows.Next() {
		var item Recommendation
		if err := rows.Scan(&item.AliasID, &item.SabyID, &item.Name, &item.SupplierArticle, &item.Balance,
			&item.SiteSales, &item.OpenRequests, &item.SuggestedQty); err != nil {
			return nil, fmt.Errorf("scan procurement recommendation: %w", err)
		}
		if item.OpenRequests > 0 {
			item.Reason = "Есть клиентский заказ"
		} else if item.SuggestedQty > 0 {
			item.Reason = "Продажи выше целевого остатка"
		} else {
			item.Reason = "Запас достаточен"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) SearchNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT saby_id, code, article, name, balance,
			price_minor::DOUBLE PRECISION / 100
		FROM saby_nomenclature
		WHERE name ILIKE '%' || $1 || '%'
			OR code ILIKE '%' || $1 || '%'
			OR article ILIKE '%' || $1 || '%'
			OR saby_id ILIKE '%' || $1 || '%'
		ORDER BY CASE
			WHEN UPPER(code) = UPPER($1) OR UPPER(article) = UPPER($1) OR UPPER(saby_id) = UPPER($1) THEN 0
			WHEN name ILIKE $1 || '%' THEN 1
			ELSE 2
		END, balance DESC, name
		LIMIT 30
	`, query)
	if err != nil {
		return nil, fmt.Errorf("search Saby nomenclature for procurement: %w", err)
	}
	defer rows.Close()
	items := make([]NomenclatureCandidate, 0)
	for rows.Next() {
		var item NomenclatureCandidate
		if err := rows.Scan(&item.SabyID, &item.Code, &item.Article, &item.Name, &item.Balance, &item.Price); err != nil {
			return nil, fmt.Errorf("scan Saby nomenclature candidate: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) ResolveAlias(
	ctx context.Context,
	actor Actor,
	aliasID int64,
	input AliasResolution,
) (AliasReview, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return AliasReview{}, fmt.Errorf("begin resolve procurement alias: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT TRUE FROM procurement_supplier_aliases WHERE id = $1 FOR UPDATE`, aliasID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return AliasReview{}, ErrNotFound
	} else if err != nil {
		return AliasReview{}, fmt.Errorf("lock procurement alias: %w", err)
	}
	if input.MatchStatus == "confirmed" {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM saby_nomenclature WHERE saby_id = $1)`, input.SabyID).Scan(&exists); err != nil {
			return AliasReview{}, fmt.Errorf("validate Saby nomenclature candidate: %w", err)
		}
		if !exists {
			return AliasReview{}, ErrNotFound
		}
	}

	confidence := 0.0
	if input.MatchStatus == "confirmed" {
		confidence = 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_supplier_aliases SET
			matched_saby_id = NULLIF($2, ''), match_status = $3,
			confidence = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, aliasID, input.SabyID, input.MatchStatus, confidence); err != nil {
		return AliasReview{}, fmt.Errorf("resolve procurement alias: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_order_lines SET
			saby_id = NULLIF($2, ''), match_status = $3, updated_at = CURRENT_TIMESTAMP
		WHERE supplier_alias_id = $1
	`, aliasID, input.SabyID, input.MatchStatus); err != nil {
		return AliasReview{}, fmt.Errorf("resolve procurement order lines: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_documents d SET
			parse_status = CASE WHEN d.arithmetic_status <> 'ok' OR EXISTS (
				SELECT 1 FROM procurement_order_lines l
				WHERE l.procurement_document_id = d.id AND l.match_status IN ('unmatched', 'suggested', 'new_product')
			) THEN 'review' ELSE 'parsed' END,
			updated_at = CURRENT_TIMESTAMP
		WHERE EXISTS (
			SELECT 1 FROM procurement_order_lines affected
			WHERE affected.procurement_document_id = d.id AND affected.supplier_alias_id = $1
		)
	`, aliasID); err != nil {
		return AliasReview{}, fmt.Errorf("refresh procurement document review status: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_orders o SET
			status = CASE WHEN EXISTS (
				SELECT 1 FROM procurement_order_lines l
				WHERE l.procurement_order_id = o.id AND l.match_status IN ('unmatched', 'suggested', 'new_product')
			) OR EXISTS (
				SELECT 1 FROM procurement_documents d
				WHERE d.procurement_order_id = o.id AND d.arithmetic_status <> 'ok'
			) THEN 'review' ELSE 'invoice_received' END,
			updated_at = CURRENT_TIMESTAMP
		WHERE o.status NOT IN ('received', 'cancelled') AND EXISTS (
			SELECT 1 FROM procurement_order_lines affected
			WHERE affected.procurement_order_id = o.id AND affected.supplier_alias_id = $1
		)
	`, aliasID); err != nil {
		return AliasReview{}, fmt.Errorf("refresh procurement order review status: %w", err)
	}

	item, err := loadAliasReview(ctx, tx, aliasID)
	if err != nil {
		return AliasReview{}, err
	}
	if err := audit(ctx, tx, actor, "procurement.alias.resolve", "procurement_supplier_alias", aliasID, item); err != nil {
		return AliasReview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AliasReview{}, fmt.Errorf("commit procurement alias resolution: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) CreateRequest(ctx context.Context, actor Actor, input RequestCreate) (Request, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("begin create procurement request: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var item Request
	err = tx.QueryRow(ctx, `
		INSERT INTO procurement_requests (kind, saby_id, requested_name, quantity, notes, created_by)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6)
		RETURNING id, kind, COALESCE(saby_id, ''), requested_name, quantity, status, notes, created_at
	`, input.Kind, input.SabyID, input.RequestedName, input.Quantity, input.Notes, actor.CustomerID).Scan(
		&item.ID, &item.Kind, &item.SabyID, &item.RequestedName, &item.Quantity,
		&item.Status, &item.Notes, &item.CreatedAt,
	)
	if err != nil {
		if uniqueViolation(err) {
			return Request{}, ErrInvalidInput
		}
		return Request{}, fmt.Errorf("insert procurement request: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.request.create", "procurement_request", item.ID, item); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("commit procurement request: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) UpdateAvailability(ctx context.Context, actor Actor, aliasID int64, input AvailabilityUpdate) (AliasReview, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return AliasReview{}, fmt.Errorf("begin update procurement availability: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	command, err := tx.Exec(ctx, `
		UPDATE procurement_supplier_aliases SET availability_status = $2,
			unavailable_since = CASE WHEN $2 = 'temporarily_unavailable' THEN COALESCE(unavailable_since, CURRENT_DATE) ELSE NULL END,
			check_after = NULLIF($3, '')::DATE, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, aliasID, input.Status, input.CheckAfter)
	if err != nil {
		return AliasReview{}, fmt.Errorf("update procurement availability: %w", err)
	}
	if command.RowsAffected() == 0 {
		return AliasReview{}, ErrNotFound
	}
	item, err := loadAliasReview(ctx, tx, aliasID)
	if err != nil {
		return AliasReview{}, err
	}
	if err := audit(ctx, tx, actor, "procurement.alias.availability", "procurement_supplier_alias", aliasID, item); err != nil {
		return AliasReview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AliasReview{}, fmt.Errorf("commit procurement availability: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) PrepareBatch(ctx context.Context, actor Actor, orderID int64, kind string) (ActionBatch, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ActionBatch{}, fmt.Errorf("begin prepare procurement batch: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, ErrNotFound
	} else if err != nil {
		return ActionBatch{}, fmt.Errorf("lock procurement order for batch: %w", err)
	}
	if status != "ready_to_receive" {
		return ActionBatch{}, ErrInvalidInput
	}
	var batchID int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM procurement_action_batches
		WHERE procurement_order_id = $1 AND kind = $2 AND status NOT IN ('cancelled', 'completed')
		ORDER BY id DESC LIMIT 1 FOR UPDATE
	`, orderID, kind).Scan(&batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO procurement_action_batches (procurement_order_id, kind, created_by)
			VALUES ($1, $2, $3) RETURNING id
		`, orderID, kind, actor.CustomerID).Scan(&batchID)
	}
	if err != nil {
		return ActionBatch{}, fmt.Errorf("create procurement action batch: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_action_items WHERE batch_id = $1 AND status = 'draft'`, batchID); err != nil {
		return ActionBatch{}, fmt.Errorf("refresh procurement action batch: %w", err)
	}
	if kind == "receipt" {
		_, err = tx.Exec(ctx, `
			INSERT INTO procurement_action_items (
				batch_id, procurement_order_line_id, channel, external_article, new_value, quantity
			)
			SELECT $1, l.id, 'saby_receipt', l.saby_id, l.unit_cost_rub,
				COALESCE(l.invoiced_qty, l.ordered_qty)
			FROM procurement_order_lines l
			WHERE l.procurement_order_id = $2 AND l.match_status = 'confirmed'
				AND l.saby_id IS NOT NULL AND l.unit_cost_rub IS NOT NULL
			ON CONFLICT (batch_id, procurement_order_line_id, channel) DO NOTHING
		`, batchID, orderID)
	} else {
		_, err = tx.Exec(ctx, `
			INSERT INTO procurement_action_items (
				batch_id, procurement_order_line_id, channel, external_article, old_value, new_value, compare_at_value
			)
			SELECT $1, l.id, channel.name,
				CASE channel.name WHEN 'wb' THEN COALESCE(pc.wb_article, '')
					WHEN 'ozon' THEN COALESCE(pc.ozon_article, '') ELSE l.saby_id END,
				CASE channel.name
					WHEN 'site' THEN COALESCE(pv.base_price_minor, 0)::NUMERIC / 100
					WHEN 'saby_price' THEN COALESCE(n.price_minor, 0)::NUMERIC / 100
					ELSE NULL
				END,
				CASE WHEN channel.name IN ('wb', 'ozon') THEN l.proposed_marketplace_rub ELSE l.proposed_retail_rub END,
				CASE WHEN channel.name IN ('wb', 'ozon') THEN l.proposed_marketplace_strike_rub ELSE NULL END
			FROM procurement_order_lines l
			JOIN saby_nomenclature n ON n.saby_id = l.saby_id
			LEFT JOIN product_variants pv ON pv.saby_id = l.saby_id
			LEFT JOIN procurement_product_channels pc ON pc.saby_id = l.saby_id
			CROSS JOIN (VALUES ('site'), ('saby_price'), ('wb'), ('ozon')) AS channel(name)
			CROSS JOIN procurement_pricing_settings settings
			WHERE l.procurement_order_id = $2 AND l.match_status = 'confirmed'
				AND l.proposed_retail_rub IS NOT NULL AND (
					n.price_minor <= 0 OR ABS(l.proposed_retail_rub - n.price_minor::NUMERIC / 100)
						> (n.price_minor::NUMERIC / 100 * settings.price_change_threshold)
				)
			ON CONFLICT (batch_id, procurement_order_line_id, channel) DO NOTHING
		`, batchID, orderID)
	}
	if err != nil {
		return ActionBatch{}, fmt.Errorf("fill procurement action batch: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.batch.prepare", "procurement_action_batch", batchID, map[string]any{"kind": kind}); err != nil {
		return ActionBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionBatch{}, fmt.Errorf("commit procurement action batch: %w", err)
	}
	batches, err := store.listBatches(ctx, orderID)
	if err != nil {
		return ActionBatch{}, err
	}
	for _, batch := range batches {
		if batch.ID == batchID {
			return batch, nil
		}
	}
	return ActionBatch{}, ErrNotFound
}

func (store *PostgresStore) ApproveBatch(ctx context.Context, actor Actor, batchID int64, configured map[string]bool) (ActionBatch, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ActionBatch{}, fmt.Errorf("begin approve procurement batch: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var orderID int64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT procurement_order_id, status FROM procurement_action_batches WHERE id = $1 FOR UPDATE
	`, batchID).Scan(&orderID, &status); errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, ErrNotFound
	} else if err != nil {
		return ActionBatch{}, fmt.Errorf("lock procurement batch: %w", err)
	}
	if status != "draft" {
		return ActionBatch{}, ErrInvalidInput
	}
	// The site price is atomic with approval. External calls are queued in the
	// same transaction and are performed by a restart-safe worker afterwards.
	if _, err := tx.Exec(ctx, `
		UPDATE product_variants pv SET base_price_minor = ROUND(item.new_value * 100), updated_at = CURRENT_TIMESTAMP
		FROM procurement_action_items item
		JOIN procurement_order_lines line ON line.id = item.procurement_order_line_id
		WHERE item.batch_id = $1 AND item.channel = 'site' AND line.saby_id = pv.saby_id
	`, batchID); err != nil {
		return ActionBatch{}, fmt.Errorf("apply procurement site prices: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_items SET status = CASE
				WHEN channel = 'site' THEN 'completed'
				WHEN channel = 'wb' AND $2 AND external_article <> '' THEN 'queued'
				WHEN channel = 'ozon' AND $3 AND external_article <> '' THEN 'queued'
				WHEN channel IN ('wb', 'ozon') AND external_article = '' THEN 'skipped'
				ELSE 'not_configured' END,
			error_message = CASE
				WHEN channel = 'site' OR (channel = 'wb' AND $2 AND external_article <> '') OR (channel = 'ozon' AND $3 AND external_article <> '') THEN ''
				WHEN channel IN ('wb', 'ozon') AND external_article = '' THEN 'Не заполнен артикул канала'
				ELSE 'API-адаптер канала не настроен' END,
			completed_at = CASE WHEN channel = 'site' THEN CURRENT_TIMESTAMP ELSE NULL END,
			next_attempt_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE batch_id = $1
	`, batchID, configured["wb"], configured["ozon"]); err != nil {
		return ActionBatch{}, fmt.Errorf("update procurement action statuses: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_batches SET status = CASE
			WHEN EXISTS (SELECT 1 FROM procurement_action_items WHERE batch_id = $1 AND status = 'queued') THEN 'processing'
			WHEN EXISTS (SELECT 1 FROM procurement_action_items WHERE batch_id = $1 AND status IN ('not_configured', 'skipped', 'failed')) THEN 'partially_completed'
			ELSE 'completed' END,
			approved_by = $2, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, batchID, actor.CustomerID); err != nil {
		return ActionBatch{}, fmt.Errorf("approve procurement action batch: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.batch.approve", "procurement_action_batch", batchID, map[string]any{"approved": true}); err != nil {
		return ActionBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionBatch{}, fmt.Errorf("commit procurement batch approval: %w", err)
	}
	batches, err := store.listBatches(ctx, orderID)
	if err != nil {
		return ActionBatch{}, err
	}
	for _, batch := range batches {
		if batch.ID == batchID {
			return batch, nil
		}
	}
	return ActionBatch{}, ErrNotFound
}

func (store *PostgresStore) listBatches(ctx context.Context, orderID int64) ([]ActionBatch, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, kind, status, created_at FROM procurement_action_batches
		WHERE procurement_order_id = $1 ORDER BY id DESC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query procurement batches: %w", err)
	}
	defer rows.Close()
	batches := make([]ActionBatch, 0)
	for rows.Next() {
		var batch ActionBatch
		if err := rows.Scan(&batch.ID, &batch.Kind, &batch.Status, &batch.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan procurement batch: %w", err)
		}
		itemRows, err := store.pool.Query(ctx, `
			SELECT item.id, item.procurement_order_line_id, COALESCE(n.name, line.raw_name),
				item.channel, item.external_article, item.old_value::DOUBLE PRECISION,
				item.new_value::DOUBLE PRECISION, item.compare_at_value::DOUBLE PRECISION,
				item.quantity, item.status, item.error_message
			FROM procurement_action_items item
			JOIN procurement_order_lines line ON line.id = item.procurement_order_line_id
			LEFT JOIN saby_nomenclature n ON n.saby_id = line.saby_id
			WHERE item.batch_id = $1 ORDER BY item.channel, item.id
		`, batch.ID)
		if err != nil {
			return nil, fmt.Errorf("query procurement batch items: %w", err)
		}
		batch.Items = make([]ActionItem, 0)
		for itemRows.Next() {
			var item ActionItem
			if err := itemRows.Scan(&item.ID, &item.LineID, &item.ProductName, &item.Channel,
				&item.ExternalArticle, &item.OldValue, &item.NewValue, &item.CompareAtValue, &item.Quantity,
				&item.Status, &item.ErrorMessage); err != nil {
				itemRows.Close()
				return nil, fmt.Errorf("scan procurement batch item: %w", err)
			}
			batch.Items = append(batch.Items, item)
		}
		itemRows.Close()
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (store *PostgresStore) ClaimAction(ctx context.Context) (*ActionItem, error) {
	var item ActionItem
	err := store.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM procurement_action_items
			WHERE (status = 'queued' AND next_attempt_at <= CURRENT_TIMESTAMP)
				OR (status = 'processing' AND locked_until < CURRENT_TIMESTAMP)
			ORDER BY next_attempt_at, id FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE procurement_action_items item SET status = 'processing', attempts = attempts + 1,
			last_attempt_at = CURRENT_TIMESTAMP, locked_until = CURRENT_TIMESTAMP + INTERVAL '2 minutes',
			updated_at = CURRENT_TIMESTAMP
		FROM candidate WHERE item.id = candidate.id
		RETURNING item.id, item.procurement_order_line_id, item.channel, item.external_article,
			item.old_value::DOUBLE PRECISION, item.new_value::DOUBLE PRECISION,
			item.compare_at_value::DOUBLE PRECISION, item.quantity, item.external_operation_id, item.attempts
	`).Scan(&item.ID, &item.LineID, &item.Channel, &item.ExternalArticle, &item.OldValue,
		&item.NewValue, &item.CompareAtValue, &item.Quantity, &item.ExternalOperationID, &item.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim procurement action: %w", err)
	}
	return &item, nil
}

func (store *PostgresStore) FinishAction(ctx context.Context, actionID int64, result ActionExecution, executeErr error) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin finish procurement action: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var batchID int64
	var attempts int
	if err := tx.QueryRow(ctx, `SELECT batch_id, attempts FROM procurement_action_items WHERE id = $1 FOR UPDATE`, actionID).Scan(&batchID, &attempts); err != nil {
		return fmt.Errorf("lock finished procurement action: %w", err)
	}
	status, message, delay := "queued", "", result.RetryAfter
	if delay <= 0 {
		delay = 15 * time.Second
	}
	if executeErr != nil {
		message = executeErr.Error()
		if attempts >= 5 {
			status = "failed"
		} else {
			delay = time.Duration(attempts*attempts) * 15 * time.Second
		}
	} else if result.Completed {
		status = "completed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_items SET status = $2, error_message = $3,
			external_operation_id = CASE WHEN $4 = '' THEN external_operation_id ELSE $4 END,
			completed_at = CASE WHEN $2 = 'completed' THEN CURRENT_TIMESTAMP ELSE NULL END,
			next_attempt_at = CURRENT_TIMESTAMP + ($5 * INTERVAL '1 second'), locked_until = NULL,
			updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, actionID, status, message, result.ExternalOperationID, int(delay.Seconds())); err != nil {
		return fmt.Errorf("update procurement action result: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_batches SET status = CASE
			WHEN EXISTS (SELECT 1 FROM procurement_action_items WHERE batch_id = $1 AND status IN ('queued', 'processing')) THEN 'processing'
			WHEN EXISTS (SELECT 1 FROM procurement_action_items WHERE batch_id = $1 AND status IN ('failed', 'not_configured', 'skipped')) THEN 'partially_completed'
			ELSE 'completed' END, updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, batchID); err != nil {
		return fmt.Errorf("update procurement batch result: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit procurement action result: %w", err)
	}
	return nil
}

func (store *PostgresStore) RetryBatch(ctx context.Context, actor Actor, batchID int64, configured map[string]bool) (ActionBatch, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ActionBatch{}, fmt.Errorf("begin retry procurement batch: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var orderID int64
	if err := tx.QueryRow(ctx, `SELECT procurement_order_id FROM procurement_action_batches WHERE id = $1 FOR UPDATE`, batchID).Scan(&orderID); errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, ErrNotFound
	} else if err != nil {
		return ActionBatch{}, fmt.Errorf("lock retry procurement batch: %w", err)
	}
	command, err := tx.Exec(ctx, `
		UPDATE procurement_action_items SET status = 'queued', attempts = 0, error_message = '',
			next_attempt_at = CURRENT_TIMESTAMP, locked_until = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE batch_id = $1 AND external_article <> '' AND (
			(status = 'failed' AND channel IN ('wb', 'ozon')) OR
			(status = 'not_configured' AND ((channel = 'wb' AND $2) OR (channel = 'ozon' AND $3)))
		)
	`, batchID, configured["wb"], configured["ozon"])
	if err != nil {
		return ActionBatch{}, fmt.Errorf("retry procurement actions: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_action_batches SET status = 'processing', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, batchID); err != nil {
		return ActionBatch{}, fmt.Errorf("mark procurement batch retry: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.batch.retry", "procurement_action_batch", batchID, map[string]any{"retried": command.RowsAffected()}); err != nil {
		return ActionBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionBatch{}, fmt.Errorf("commit procurement batch retry: %w", err)
	}
	batches, err := store.listBatches(ctx, orderID)
	if err != nil {
		return ActionBatch{}, err
	}
	for _, batch := range batches {
		if batch.ID == batchID {
			return batch, nil
		}
	}
	return ActionBatch{}, ErrNotFound
}

type rowScanner interface {
	Scan(...any) error
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanDocument(row rowScanner, item *DocumentSummary) error {
	return row.Scan(
		&item.ID, &item.SupplierID, &item.SupplierName, &item.OrderID,
		&item.FileName, &item.ParserKind, &item.ParseStatus, &item.ArithmeticStatus,
		&item.DocumentNumber, &item.DocumentDate, &item.Currency, &item.Lines,
		&item.Units, &item.ProductSubtotal, &item.PackageTotal, &item.DocumentTotal,
		&item.CalculatedTotal, &item.ParseError, &item.CreatedAt,
	)
}

func loadAliasReview(ctx context.Context, querier queryRower, aliasID int64) (AliasReview, error) {
	var item AliasReview
	err := querier.QueryRow(ctx, `
		SELECT a.id, a.supplier_id, s.name, a.raw_name, a.supplier_article,
			a.pot_diameter_cm::DOUBLE PRECISION, a.height_cm::DOUBLE PRECISION,
			COALESCE(a.matched_saby_id, ''), COALESCE(n.name, ''),
			a.match_status, a.confidence::DOUBLE PRECISION, a.availability_status,
			a.last_seen_at
		FROM procurement_supplier_aliases a
		JOIN procurement_suppliers s ON s.id = a.supplier_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = a.matched_saby_id
		WHERE a.id = $1
	`, aliasID).Scan(
		&item.ID, &item.SupplierID, &item.SupplierName, &item.RawName,
		&item.SupplierArticle, &item.PotDiameterCM, &item.HeightCM,
		&item.SuggestedSabyID, &item.SuggestedSabyName, &item.MatchStatus,
		&item.Confidence, &item.AvailabilityStatus, &item.LastSeenAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AliasReview{}, ErrNotFound
	}
	if err != nil {
		return AliasReview{}, fmt.Errorf("load procurement alias: %w", err)
	}
	return item, nil
}

func loadDocumentByHash(
	ctx context.Context,
	querier queryRower,
	supplierID int64,
	hash string,
) (DocumentSummary, OrderSummary, bool, error) {
	var document DocumentSummary
	err := scanDocument(querier.QueryRow(ctx, `
		SELECT d.id, d.supplier_id, s.name, COALESCE(d.procurement_order_id, 0),
			d.file_name, d.parser_kind, d.parse_status, d.arithmetic_status,
			d.document_number, d.document_date, d.currency, d.line_count, d.unit_count,
			COALESCE(d.product_subtotal, 0)::DOUBLE PRECISION,
			COALESCE(d.package_total, 0)::DOUBLE PRECISION,
			COALESCE(d.document_total, 0)::DOUBLE PRECISION,
			COALESCE(d.calculated_total, 0)::DOUBLE PRECISION,
			d.parse_error, d.created_at
		FROM procurement_documents d
		JOIN procurement_suppliers s ON s.id = d.supplier_id
		WHERE d.supplier_id = $1 AND d.sha256 = $2
	`, supplierID, hash), &document)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentSummary{}, OrderSummary{}, false, nil
	}
	if err != nil {
		return DocumentSummary{}, OrderSummary{}, false, fmt.Errorf("query duplicate procurement document: %w", err)
	}
	var order OrderSummary
	if document.OrderID > 0 {
		order, err = loadOrderSummary(ctx, querier, document.OrderID)
		if err != nil {
			return DocumentSummary{}, OrderSummary{}, false, err
		}
	}
	return document, order, true, nil
}

func loadOrderSummary(ctx context.Context, querier queryRower, orderID int64) (OrderSummary, error) {
	var item OrderSummary
	err := querier.QueryRow(ctx, `
		SELECT o.id, o.supplier_id, s.name, o.order_number, o.document_number,
			o.document_date, o.source_kind, o.currency, o.status,
			COUNT(l.id)::INTEGER,
			COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty)), 0)::INTEGER,
			COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty) * COALESCE(l.unit_price, l.expected_unit_price, 0)), 0)::DOUBLE PRECISION,
			COUNT(l.id) FILTER (WHERE l.match_status IN ('unmatched', 'suggested', 'new_product'))::INTEGER,
			o.created_at
		FROM procurement_orders o
		JOIN procurement_suppliers s ON s.id = o.supplier_id
		LEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id
		WHERE o.id = $1
		GROUP BY o.id, s.name
	`, orderID).Scan(
		&item.ID, &item.SupplierID, &item.SupplierName, &item.OrderNumber,
		&item.DocumentNumber, &item.DocumentDate, &item.SourceKind, &item.Currency,
		&item.Status, &item.Lines, &item.Units, &item.Total, &item.Unmatched, &item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderSummary{}, ErrNotFound
	}
	if err != nil {
		return OrderSummary{}, fmt.Errorf("load procurement order: %w", err)
	}
	return item, nil
}

func upsertAlias(
	ctx context.Context,
	tx pgx.Tx,
	supplierID int64,
	line ParsedLine,
	seenAt *time.Time,
) (int64, string, string, error) {
	var aliasID int64
	var sabyID, matchStatus string
	err := tx.QueryRow(ctx, `
		INSERT INTO procurement_supplier_aliases (
			supplier_id, raw_name, normalized_name, supplier_article,
			pot_diameter_cm, height_cm, occurrences, last_seen_at, availability_status
		) VALUES ($1, $2, $3, $4, $5, $6, 1, COALESCE($7, CURRENT_DATE), 'available')
		ON CONFLICT DO NOTHING
		RETURNING id, COALESCE(matched_saby_id, ''), match_status
	`, supplierID, line.RawName, normalizeAlias(line.RawName), line.SupplierArticle,
		line.PotDiameterCM, line.HeightCM, seenAt,
	).Scan(&aliasID, &sabyID, &matchStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			UPDATE procurement_supplier_aliases SET
				normalized_name = $3, occurrences = occurrences + 1,
				last_seen_at = COALESCE($7, CURRENT_DATE), availability_status = 'available',
				unavailable_since = NULL, check_after = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE supplier_id = $1 AND LOWER(raw_name) = LOWER($2)
				AND COALESCE(supplier_article, '') = COALESCE($4, '')
				AND COALESCE(pot_diameter_cm, -1) = COALESCE($5, -1)
				AND COALESCE(height_cm, -1) = COALESCE($6, -1)
			RETURNING id, COALESCE(matched_saby_id, ''), match_status
		`, supplierID, line.RawName, normalizeAlias(line.RawName), line.SupplierArticle,
			line.PotDiameterCM, line.HeightCM, seenAt,
		).Scan(&aliasID, &sabyID, &matchStatus)
	}
	if err != nil {
		return 0, "", "", fmt.Errorf("upsert procurement supplier alias: %w", err)
	}
	return aliasID, sabyID, matchStatus, nil
}

func normalizeAlias(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func countUnits(lines []ParsedLine) int {
	units := 0
	for _, line := range lines {
		units += line.Quantity
	}
	return units
}

func planSabyIDs(items []PlanItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.SabyID)
	}
	return result
}

type auditExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func audit(ctx context.Context, executor auditExecutor, actor Actor, action, entityType string, entityID int64, after any) error {
	payload, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode procurement audit: %w", err)
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO admin_audit_log (
			actor_customer_id, actor_role, action, entity_type, entity_id, after_data
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, actor.CustomerID, actor.Role, action, entityType, fmt.Sprint(entityID), string(payload)); err != nil {
		return fmt.Errorf("insert procurement audit: %w", err)
	}
	return nil
}

func uniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
