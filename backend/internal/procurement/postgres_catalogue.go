package procurement

// Справочники, продажи и строки заказа: чтение того, что уже собрано, и
// ручной разбор кодов, которым не нашлось товара.
//
// Вынесено из postgres.go: в одном файле лежало две с половиной тысячи
// строк, и найти в нём нужный запрос было отдельной работой.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func (store *PostgresStore) listSuppliers(ctx context.Context) ([]Supplier, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, name, kind, country_code, tax_id, kpp, default_currency, active, created_at
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
		if err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.CountryCode, &item.TaxID, &item.KPP, &item.DefaultCurrency, &item.Active, &item.CreatedAt); err != nil {
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

func (store *PostgresStore) listAvailability(ctx context.Context) ([]AvailabilityItem, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT sp.supplier_id, s.name, sp.saby_id, COALESCE(n.name, ''),
			COALESCE(NULLIF(sp.supplier_article, ''), NULLIF(pc.holland_article, ''), ''),
			sp.availability_status, COALESCE(sp.check_after::TEXT, ''),
			COALESCE(sp.unavailable_since::TEXT, ''), COALESCE(n.balance, 0), a.last_seen_at
		FROM procurement_supplier_products sp
		JOIN procurement_suppliers s ON s.id = sp.supplier_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = sp.saby_id
		LEFT JOIN procurement_product_channels pc ON pc.saby_id = sp.saby_id
		LEFT JOIN LATERAL (
			SELECT last_seen_at FROM procurement_supplier_aliases
			WHERE supplier_id = sp.supplier_id AND matched_saby_id = sp.saby_id
			ORDER BY last_seen_at DESC NULLS LAST, id DESC LIMIT 1
		) a ON TRUE
		WHERE sp.availability_status IN ('check', 'temporarily_unavailable', 'discontinued')
			OR (sp.check_after IS NOT NULL AND sp.check_after <= CURRENT_DATE)
		ORDER BY CASE sp.availability_status WHEN 'check' THEN 0
				WHEN 'temporarily_unavailable' THEN 1 ELSE 2 END,
			sp.check_after NULLS FIRST, a.last_seen_at DESC NULLS LAST, sp.saby_id
		LIMIT 300
	`)
	if err != nil {
		return nil, fmt.Errorf("query procurement availability: %w", err)
	}
	defer rows.Close()
	items := make([]AvailabilityItem, 0)
	for rows.Next() {
		var item AvailabilityItem
		if err := rows.Scan(&item.SupplierID, &item.SupplierName, &item.SabyID, &item.Name,
			&item.SupplierArticle, &item.Status, &item.CheckAfter, &item.UnavailableSince,
			&item.Balance, &item.LastSeenAt); err != nil {
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
			SELECT saby_id,
				COALESCE(SUM(units) FILTER (WHERE channel = 'site'), 0)::INTEGER AS site_units,
				COALESCE(SUM(units) FILTER (WHERE channel = 'saby'), 0)::INTEGER AS saby_units,
				COALESCE(SUM(units) FILTER (WHERE channel = 'wb'), 0)::INTEGER AS wb_units,
				COALESCE(SUM(units) FILTER (WHERE channel = 'ozon'), 0)::INTEGER AS ozon_units,
				COALESCE(SUM(units), 0)::INTEGER AS units
			FROM procurement_sales_daily
			WHERE sale_date >= CURRENT_DATE - ($1 - 1) AND saby_id IS NOT NULL
			GROUP BY saby_id
		), requests AS (
			SELECT saby_id,
				COALESCE(SUM(quantity) FILTER (WHERE kind = 'customer_order'), 0)::INTEGER AS customer_units,
				COALESCE(SUM(quantity) FILTER (WHERE kind = 'staff_recommendation'), 0)::INTEGER AS staff_units
			FROM procurement_requests WHERE status = 'open' AND saby_id IS NOT NULL GROUP BY saby_id
		), incoming AS (
				SELECT l.saby_id, COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty)), 0)::INTEGER AS units
				FROM procurement_order_lines l
				JOIN procurement_orders o ON o.id = l.procurement_order_id
				WHERE l.saby_id IS NOT NULL AND l.match_status = 'confirmed'
					AND o.status IN ('ordered', 'invoice_received', 'review', 'ready_to_receive')
				GROUP BY l.saby_id
		), last_orders AS (
			SELECT l.saby_id, MAX(COALESCE(o.received_at, o.created_at)) AS last_ordered_at
			FROM procurement_order_lines l JOIN procurement_orders o ON o.id = l.procurement_order_id
			WHERE l.saby_id IS NOT NULL AND o.status <> 'cancelled' GROUP BY l.saby_id
		), products AS (
			-- Товар может быть заведён у нескольких поставщиков. Берём того,
			-- у кого наличие подтверждено раньше прочих: рекомендация должна
			-- вести к поставщику, у которого растение действительно есть.
			SELECT sp.*, COALESCE(a.id, 0) AS alias_id, a.last_seen_at,
				COALESCE(NULLIF(sp.supplier_article, ''), NULLIF(pc.holland_article, ''), '') AS article,
				ROW_NUMBER() OVER (PARTITION BY sp.saby_id ORDER BY
					CASE sp.availability_status WHEN 'available' THEN 0 WHEN 'check' THEN 1 WHEN 'unknown' THEN 2 ELSE 3 END,
					a.last_seen_at DESC NULLS LAST, sp.updated_at DESC, sp.supplier_id) AS preference
			FROM procurement_supplier_products sp
			LEFT JOIN procurement_product_channels pc ON pc.saby_id = sp.saby_id
			LEFT JOIN LATERAL (SELECT id, last_seen_at FROM procurement_supplier_aliases
				WHERE supplier_id = sp.supplier_id AND matched_saby_id = sp.saby_id
				ORDER BY last_seen_at DESC NULLS LAST, id DESC LIMIT 1) a ON TRUE
		)
			SELECT sp.alias_id, sp.supplier_id, n.saby_id, n.name, sp.article,
				sp.availability_status, n.balance, COALESCE(i.units, 0),
				COALESCE(s.site_units, 0), COALESCE(s.saby_units, 0), COALESCE(s.wb_units, 0),
				COALESCE(s.ozon_units, 0), COALESCE(r.customer_units, 0), COALESCE(r.staff_units, 0),
				sp.minimum_order_qty, sp.order_multiple, lo.last_ordered_at,
				e.saby_id IS NOT NULL, COALESCE(e.reason, '')
			FROM products sp
			JOIN saby_nomenclature n ON n.saby_id = sp.saby_id
			LEFT JOIN sales s ON s.saby_id = n.saby_id
			LEFT JOIN requests r ON r.saby_id = n.saby_id
			LEFT JOIN incoming i ON i.saby_id = n.saby_id
			LEFT JOIN last_orders lo ON lo.saby_id = n.saby_id
			LEFT JOIN procurement_excluded_products e ON e.saby_id = n.saby_id
			WHERE sp.preference = 1 AND (n.balance <= 0 OR COALESCE(s.units, 0) > 0 OR
				COALESCE(r.customer_units, 0) > 0 OR COALESCE(r.staff_units, 0) > 0 OR
				e.saby_id IS NOT NULL)
			ORDER BY CASE WHEN COALESCE(s.units, 0) > 0 OR COALESCE(r.customer_units, 0) > 0
				OR COALESCE(r.staff_units, 0) > 0 THEN 0 ELSE 1 END, n.balance, n.name
		LIMIT 2000
	`, settings.RecommendationDays)
	if err != nil {
		return nil, fmt.Errorf("query procurement recommendations: %w", err)
	}
	defer rows.Close()
	items := make([]Recommendation, 0, 200)
	for rows.Next() {
		var input recommendationInput
		if err := rows.Scan(&input.AliasID, &input.SupplierID, &input.SabyID, &input.Name, &input.SupplierArticle,
			&input.AvailabilityStatus, &input.Balance, &input.Incoming, &input.SiteSales, &input.SabySales,
			&input.WBSales, &input.OzonSales, &input.CustomerRequests, &input.StaffRequests,
			&input.MinimumOrderQty, &input.OrderMultiple, &input.LastOrderedAt,
			&input.Excluded, &input.ExclusionReason); err != nil {
			return nil, fmt.Errorf("scan procurement recommendation: %w", err)
		}
		item, include := calculateRecommendation(input, settings.RecommendationDays, settings.TargetCoverDays)
		if include {
			items = append(items, item)
		}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read procurement recommendations: %w", rows.Err())
	}
	sort.SliceStable(items, func(left, right int) bool {
		if recommendationPriority(items[left]) != recommendationPriority(items[right]) {
			return recommendationPriority(items[left]) < recommendationPriority(items[right])
		}
		if items[left].SuggestedQty != items[right].SuggestedQty {
			return items[left].SuggestedQty > items[right].SuggestedQty
		}
		return items[left].Name < items[right].Name
	})

	return items, rows.Err()
}

func (store *PostgresStore) listSalesSync(ctx context.Context) ([]SalesSyncStatus, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT state.channel, state.status, state.last_attempt_at, state.last_success_at,
			state.last_error, state.rows_synced, COALESCE(state.period_from::TEXT, ''),
			COALESCE(state.period_to::TEXT, ''), COALESCE(MAX(sale.sale_date)::TEXT, ''),
			COUNT(*) FILTER (WHERE sale.saby_id IS NOT NULL)::INTEGER
		FROM procurement_sales_sync_state state
		LEFT JOIN procurement_sales_daily sale ON sale.channel = state.channel
		GROUP BY state.channel, state.status, state.last_attempt_at, state.last_success_at,
			state.last_error, state.rows_synced, state.period_from, state.period_to
		ORDER BY CASE state.channel WHEN 'saby' THEN 0 WHEN 'site' THEN 1 WHEN 'wb' THEN 2 ELSE 3 END
	`)
	if err != nil {
		return nil, fmt.Errorf("query sales synchronization state: %w", err)
	}
	defer rows.Close()
	items := make([]SalesSyncStatus, 0, 4)
	for rows.Next() {
		var item SalesSyncStatus
		if err := rows.Scan(&item.Channel, &item.Status, &item.LastAttemptAt, &item.LastSuccessAt,
			&item.LastError, &item.RowsSynced, &item.PeriodFrom, &item.PeriodTo, &item.LatestSale,
			&item.RowsLinked); err != nil {
			return nil, fmt.Errorf("scan sales synchronization state: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) MarkSalesSync(ctx context.Context, channel, status string, syncErr error) error {
	if !validSalesChannel(channel) || !oneOf(status, "pending", "running", "ok", "error", "disabled") {
		return ErrInvalidInput
	}
	errorMessage := ""
	if syncErr != nil {
		errorMessage = syncErr.Error()
		if len(errorMessage) > 500 {
			errorMessage = errorMessage[:500]
		}
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO procurement_sales_sync_state (channel, status, last_attempt_at, last_error)
		VALUES ($1, $2, CASE WHEN $2 IN ('running', 'error') THEN CURRENT_TIMESTAMP ELSE NULL END, $3)
		ON CONFLICT (channel) DO UPDATE SET status = EXCLUDED.status,
			last_attempt_at = CASE WHEN EXCLUDED.status IN ('running', 'error')
				THEN CURRENT_TIMESTAMP ELSE procurement_sales_sync_state.last_attempt_at END,
			last_error = CASE WHEN EXCLUDED.status IN ('error', 'disabled') THEN EXCLUDED.last_error ELSE '' END,
			updated_at = CURRENT_TIMESTAMP
	`, channel, status, errorMessage)
	if err != nil {
		return fmt.Errorf("mark sales synchronization: %w", err)
	}
	return nil
}

func (store *PostgresStore) RefreshSiteSales(ctx context.Context, from, to time.Time) (int, error) {
	return store.replaceSalesWithQuery(ctx, "site", from, to, `
		INSERT INTO procurement_sales_daily (
			channel, sale_date, external_product_id, saby_id, units, gross_rub
		)
		SELECT 'site', o.created_at::DATE, pv.saby_id, pv.saby_id,
			SUM(oi.quantity)::INTEGER, SUM(oi.quantity * oi.unit_price)::NUMERIC
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		JOIN product_variants pv ON pv.id = oi.variant_id
		WHERE o.created_at::DATE BETWEEN $1 AND $2
			AND o.status <> 'cancelled' AND pv.saby_id IS NOT NULL
		GROUP BY o.created_at::DATE, pv.saby_id
	`)
}

func (store *PostgresStore) replaceSalesWithQuery(
	ctx context.Context,
	channel string,
	from, to time.Time,
	insertSQL string,
) (int, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin sales refresh: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_sales_daily WHERE channel = $1 AND sale_date BETWEEN $2 AND $3`, channel, from, to); err != nil {
		return 0, fmt.Errorf("clear sales refresh window: %w", err)
	}
	command, err := tx.Exec(ctx, insertSQL, from, to)
	if err != nil {
		return 0, fmt.Errorf("insert refreshed sales: %w", err)
	}
	count := int(command.RowsAffected())
	if err := finishSalesSync(ctx, tx, channel, from, to, count); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit sales refresh: %w", err)
	}
	return count, nil
}

func (store *PostgresStore) ReplaceSales(
	ctx context.Context,
	channel string,
	from, to time.Time,
	records []SalesRecord,
) (int, error) {
	if !validSalesChannel(channel) || from.After(to) {
		return 0, ErrInvalidInput
	}
	normalized, err := normalizeSalesRecords(records, day(from), day(to))
	if err != nil {
		return 0, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin replace sales: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_sales_daily WHERE channel = $1 AND sale_date BETWEEN $2 AND $3`, channel, from, to); err != nil {
		return 0, fmt.Errorf("clear sales window: %w", err)
	}
	inserted := 0
	for _, record := range normalized {
		command, err := tx.Exec(ctx, `
			WITH resolved AS (
				SELECT COALESCE(
					(SELECT saby_id FROM saby_nomenclature WHERE saby_id = NULLIF($4, '')),
					(SELECT saby_id FROM procurement_product_channels WHERE $1 = 'wb' AND wb_nm_id::TEXT = $3 LIMIT 1),
					(SELECT saby_id FROM procurement_product_channels WHERE $1 = 'ozon' AND ozon_offer_id = $3 LIMIT 1)
				) AS saby_id
			)
			INSERT INTO procurement_sales_daily (
				channel, sale_date, external_product_id, saby_id, units, gross_rub
			)
			SELECT $1, $2, $3, resolved.saby_id, $5, $6
			FROM resolved
			WHERE $1 <> 'saby' OR EXISTS (
				SELECT 1 FROM products
				WHERE products.saby_id = resolved.saby_id
					AND products.catalog_section = 'plants'
			)
			ON CONFLICT (channel, sale_date, external_product_id) DO UPDATE SET
				saby_id = EXCLUDED.saby_id, units = EXCLUDED.units,
				gross_rub = EXCLUDED.gross_rub, synced_at = CURRENT_TIMESTAMP
		`, channel, record.Date, record.ExternalID, record.SabyID, record.Units, record.GrossRUB)
		if err != nil {
			return 0, fmt.Errorf("insert sales record: %w", err)
		}
		inserted += int(command.RowsAffected())
	}
	if err := finishSalesSync(ctx, tx, channel, from, to, inserted); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit replace sales: %w", err)
	}
	return inserted, nil
}

func finishSalesSync(ctx context.Context, tx pgx.Tx, channel string, from, to time.Time, count int) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO procurement_sales_sync_state (
			channel, status, last_attempt_at, last_success_at, last_error,
			rows_synced, period_from, period_to
		) VALUES ($1, 'ok', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, '', $2, $3, $4)
		ON CONFLICT (channel) DO UPDATE SET status = 'ok',
			last_attempt_at = CURRENT_TIMESTAMP, last_success_at = CURRENT_TIMESTAMP,
			last_error = '', rows_synced = EXCLUDED.rows_synced,
			period_from = EXCLUDED.period_from, period_to = EXCLUDED.period_to,
			updated_at = CURRENT_TIMESTAMP
	`, channel, count, from, to)
	if err != nil {
		return fmt.Errorf("finish sales synchronization: %w", err)
	}
	return nil
}

func (store *PostgresStore) SearchNomenclature(ctx context.Context, query string) ([]NomenclatureCandidate, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT saby_id, code, article, name, balance,
			price_minor::DOUBLE PRECISION / 100
		FROM saby_nomenclature
		WHERE missing_since IS NULL
			AND saby_id ~ '^[0-9]+$'
			AND (name ILIKE '%' || $1 || '%'
			OR code ILIKE '%' || $1 || '%'
			OR article ILIKE '%' || $1 || '%'
			OR saby_id ILIKE '%' || $1 || '%')
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
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM saby_nomenclature
			WHERE saby_id = $1 AND missing_since IS NULL AND saby_id ~ '^[0-9]+$'
		)`, input.SabyID).Scan(&exists); err != nil {
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
			saby_id = NULLIF($2, ''), match_status = $3,
			purchase_unit_rub = NULL, trolley_delivery_unit_rub = NULL,
			ryazan_delivery_unit_rub = NULL, unit_cost_rub = NULL,
			proposed_retail_rub = NULL, proposed_marketplace_rub = NULL,
			proposed_marketplace_strike_rub = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE supplier_alias_id = $1 AND procurement_order_id IN (
			SELECT id FROM procurement_orders WHERE status NOT IN ('received', 'cancelled')
		)
	`, aliasID, input.SabyID, input.MatchStatus); err != nil {
		return AliasReview{}, fmt.Errorf("resolve procurement order lines: %w", err)
	}
	// A prepared document contains a snapshot of the old Saby IDs and balances.
	// After an alias is moved to another card that snapshot must not be reused.
	// Cancel only batches which have not produced a successful external action;
	// an already completed Saby document remains immutable and visible in history.
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_batches batch SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
		WHERE batch.procurement_order_id IN (
			SELECT DISTINCT procurement_order_id FROM procurement_order_lines WHERE supplier_alias_id = $1
		) AND batch.status IN ('draft', 'partially_completed')
		AND NOT EXISTS (
			SELECT 1 FROM procurement_action_items item
			WHERE item.batch_id = batch.id AND item.status = 'completed'
		)
	`, aliasID); err != nil {
		return AliasReview{}, fmt.Errorf("cancel stale procurement batches after rematch: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_orders SET status = 'review', calculated_at = NULL,
			calculation_settings = NULL, calculation_version = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE status NOT IN ('received', 'cancelled') AND id IN (
			SELECT DISTINCT procurement_order_id FROM procurement_order_lines WHERE supplier_alias_id = $1
		)
	`, aliasID); err != nil {
		return AliasReview{}, fmt.Errorf("invalidate procurement calculation after rematch: %w", err)
	}
	if input.MatchStatus == "confirmed" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO procurement_supplier_products (supplier_id, saby_id, supplier_article, availability_status, updated_by)
			SELECT supplier_id, $2, supplier_article, availability_status, $3
			FROM procurement_supplier_aliases WHERE id = $1
			ON CONFLICT (supplier_id, saby_id) DO UPDATE SET
				supplier_article = CASE WHEN EXCLUDED.supplier_article <> '' THEN EXCLUDED.supplier_article ELSE procurement_supplier_products.supplier_article END,
				updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
		`, aliasID, input.SabyID, actor.CustomerID); err != nil {
			return AliasReview{}, fmt.Errorf("upsert procurement supplier product: %w", err)
		}
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

func (store *PostgresStore) UpdateOrderLine(ctx context.Context, actor Actor, lineID int64, input OrderLineUpdate) (OrderDetail, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("begin update procurement line: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var orderID int64
	err = tx.QueryRow(ctx, `
		SELECT l.procurement_order_id FROM procurement_order_lines l
		JOIN procurement_orders o ON o.id = l.procurement_order_id
		WHERE l.id = $1 AND o.status NOT IN ('received', 'cancelled') FOR UPDATE OF l, o
	`, lineID).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderDetail{}, ErrNotFound
	}
	if err != nil {
		return OrderDetail{}, fmt.Errorf("lock procurement line: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE procurement_order_lines SET
			expected_unit_price = CASE WHEN $2 THEN $3 ELSE expected_unit_price END,
			pot_diameter_cm = CASE WHEN $4 THEN $5 ELSE pot_diameter_cm END,
			height_cm = CASE WHEN $6 THEN $7 ELSE height_cm END,
			load_unit = CASE WHEN $8 THEN $9 ELSE load_unit END,
			comparison_accepted = CASE WHEN $10 THEN $11 ELSE comparison_accepted END,
			comparison_note = CASE WHEN $12 THEN $13 ELSE comparison_note END,
			purchase_unit_rub = NULL, trolley_delivery_unit_rub = NULL,
			ryazan_delivery_unit_rub = NULL, unit_cost_rub = NULL,
			proposed_retail_rub = NULL, proposed_marketplace_rub = NULL,
			proposed_marketplace_strike_rub = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, lineID,
		input.ExpectedUnitPrice != nil, input.ExpectedUnitPrice,
		input.PotDiameterCM != nil, input.PotDiameterCM,
		input.HeightCM != nil, input.HeightCM,
		input.LoadUnit != nil, input.LoadUnit,
		input.AcceptComparison != nil, input.AcceptComparison,
		input.ComparisonNote != nil, input.ComparisonNote)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("update procurement line: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_batches SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
		WHERE procurement_order_id = $1 AND status = 'draft'
	`, orderID); err != nil {
		return OrderDetail{}, fmt.Errorf("cancel stale procurement batches: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_orders SET status = 'review', calculated_at = NULL,
			calculation_settings = NULL, calculation_version = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, orderID); err != nil {
		return OrderDetail{}, fmt.Errorf("invalidate procurement calculation: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.line.update", "procurement_order_line", lineID, input); err != nil {
		return OrderDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrderDetail{}, fmt.Errorf("commit procurement line update: %w", err)
	}
	return store.OrderDetail(ctx, orderID)
}

func (store *PostgresStore) UpdateOrderStatus(ctx context.Context, actor Actor, orderID int64, input OrderStatusUpdate) (OrderDetail, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("begin update procurement status: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
		return OrderDetail{}, ErrNotFound
	} else if err != nil {
		return OrderDetail{}, fmt.Errorf("lock procurement order status: %w", err)
	}
	allowed := input.Status == "cancelled" && current != "received" && current != "cancelled" ||
		input.Status == "review" && current == "ready_to_receive" ||
		input.Status == "received" && current == "ready_to_receive"
	if !allowed {
		return OrderDetail{}, ErrInvalidInput
	}
	if input.Status == "received" {
		var prepared bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM procurement_action_batches
				WHERE procurement_order_id = $1 AND kind = 'receipt' AND status NOT IN ('draft', 'cancelled'))
		`, orderID).Scan(&prepared); err != nil || !prepared {
			return OrderDetail{}, ErrInvalidInput
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_orders SET status = $2,
			received_at = CASE WHEN $2 = 'received' THEN CURRENT_TIMESTAMP ELSE received_at END,
			cancelled_at = CASE WHEN $2 = 'cancelled' THEN CURRENT_TIMESTAMP ELSE cancelled_at END,
			notes = CASE WHEN $3 = '' THEN notes ELSE CONCAT_WS(E'\n', NULLIF(notes, ''), $3) END,
			updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, orderID, input.Status, input.Note); err != nil {
		return OrderDetail{}, fmt.Errorf("update procurement order status: %w", err)
	}
	if input.Status == "cancelled" {
		if _, err := tx.Exec(ctx, `
			UPDATE procurement_requests SET status = 'open', updated_at = CURRENT_TIMESTAMP
			WHERE status = 'included' AND saby_id IN (
				SELECT saby_id FROM procurement_order_lines WHERE procurement_order_id = $1 AND saby_id IS NOT NULL
			)
		`, orderID); err != nil {
			return OrderDetail{}, fmt.Errorf("restore procurement requests: %w", err)
		}
		_, _ = tx.Exec(ctx, `UPDATE procurement_action_batches SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP WHERE procurement_order_id = $1 AND status = 'draft'`, orderID)
	}
	if input.Status == "received" {
		if _, err := tx.Exec(ctx, `
			UPDATE procurement_requests SET status = 'fulfilled', updated_at = CURRENT_TIMESTAMP
			WHERE status = 'included' AND saby_id IN (
				SELECT saby_id FROM procurement_order_lines WHERE procurement_order_id = $1 AND saby_id IS NOT NULL
			)
		`, orderID); err != nil {
			return OrderDetail{}, fmt.Errorf("fulfil procurement requests: %w", err)
		}
	}
	if input.Status == "review" {
		_, _ = tx.Exec(ctx, `UPDATE procurement_action_batches SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP WHERE procurement_order_id = $1 AND status = 'draft'`, orderID)
	}
	if err := audit(ctx, tx, actor, "procurement.order.status", "procurement_order", orderID, input); err != nil {
		return OrderDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrderDetail{}, fmt.Errorf("commit procurement status: %w", err)
	}
	return store.OrderDetail(ctx, orderID)
}

// DeleteOrder permanently removes only an already-cancelled purchase. The
// source document is removed in the same transaction so its SHA-256 no longer
// blocks a deliberate re-import of the same PDF.
func (store *PostgresStore) DeleteOrder(ctx context.Context, actor Actor, orderID int64) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete procurement order: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock procurement order for deletion: %w", err)
	}
	if status != "cancelled" {
		return ErrOrderNotCancelled
	}
	if err := audit(ctx, tx, actor, "procurement.order.delete", "procurement_order", orderID, map[string]any{"status": status}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_documents WHERE procurement_order_id = $1`, orderID); err != nil {
		return fmt.Errorf("delete procurement documents: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_orders WHERE id = $1`, orderID); err != nil {
		return fmt.Errorf("delete procurement order: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete procurement order: %w", err)
	}
	return nil
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

func (store *PostgresStore) UpdateRequest(ctx context.Context, actor Actor, requestID int64, input RequestUpdate) (Request, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("begin update procurement request: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var item Request
	err = tx.QueryRow(ctx, `
		UPDATE procurement_requests SET saby_id = NULLIF($2, ''), requested_name = $3,
			quantity = $4, status = $5, notes = $6, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, kind, COALESCE(saby_id, ''), requested_name, quantity, status, notes, created_at
	`, requestID, input.SabyID, input.RequestedName, input.Quantity, input.Status, input.Notes).Scan(
		&item.ID, &item.Kind, &item.SabyID, &item.RequestedName, &item.Quantity,
		&item.Status, &item.Notes, &item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("update procurement request: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.request.update", "procurement_request", requestID, item); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("commit procurement request update: %w", err)
	}
	return item, nil
}
