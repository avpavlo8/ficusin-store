package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	result.Suppliers, result.Orders, result.Review = suppliers, orders, review
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
