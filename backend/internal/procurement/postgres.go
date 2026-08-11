package procurement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
			(SELECT COUNT(*) FROM procurement_supplier_aliases WHERE match_status IN ('unmatched', 'suggested'))::INTEGER,
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
	result.Suppliers, result.Orders, result.Documents, result.Review = suppliers, orders, documents, review
	return result, nil
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO procurement_order_lines (
				procurement_order_id, procurement_document_id, supplier_alias_id, saby_id,
				raw_name, supplier_article, invoiced_qty, unit_price, line_total,
				load_unit, match_status, source_page, source_line, pot_diameter_cm, height_cm
			) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`, orderID, document.ID, aliasID, sabyID, line.RawName, line.SupplierArticle,
			line.Quantity, line.UnitPrice, line.LineTotal, line.LoadUnit, matchStatus,
			line.SourcePage, line.SourceLine, line.PotDiameterCM, line.HeightCM,
		); err != nil {
			return ImportResult{}, fmt.Errorf("insert procurement document line: %w", err)
		}
	}

	parseStatus, orderStatus := "parsed", "invoice_received"
	if unmatched > 0 || !parsed.ArithmeticOK {
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
			COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty) * COALESCE(l.unit_price, 0)), 0)::DOUBLE PRECISION,
			COUNT(l.id) FILTER (WHERE l.match_status IN ('unmatched', 'suggested'))::INTEGER,
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
		WHERE a.match_status IN ('unmatched', 'suggested')
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
				WHERE l.procurement_document_id = d.id AND l.match_status IN ('unmatched', 'suggested')
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
				WHERE l.procurement_order_id = o.id AND l.match_status IN ('unmatched', 'suggested')
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
			COALESCE(SUM(COALESCE(l.invoiced_qty, l.ordered_qty) * COALESCE(l.unit_price, 0)), 0)::DOUBLE PRECISION,
			COUNT(l.id) FILTER (WHERE l.match_status IN ('unmatched', 'suggested'))::INTEGER,
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
