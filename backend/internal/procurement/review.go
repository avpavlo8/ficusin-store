package procurement

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type SupplierUpdate struct {
	SupplierCreate
	Active bool `json:"active"`
}

func (service *Service) UpdateSupplier(ctx context.Context, actor Actor, id int64, input SupplierUpdate) (Supplier, error) {
	normalized, err := normalizeSupplier(input.SupplierCreate)
	if err != nil || id <= 0 {
		return Supplier{}, ErrInvalidInput
	}
	input.SupplierCreate = normalized
	store, ok := service.store.(interface {
		UpdateSupplier(context.Context, Actor, int64, SupplierUpdate) (Supplier, error)
	})
	if !ok {
		return Supplier{}, ErrInvalidInput
	}
	return store.UpdateSupplier(ctx, actor, id, input)
}

func (store *PostgresStore) UpdateSupplier(ctx context.Context, actor Actor, id int64, input SupplierUpdate) (Supplier, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Supplier{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var kind string
	if err := tx.QueryRow(ctx, "SELECT kind FROM procurement_suppliers WHERE id=$1 FOR UPDATE", id).Scan(&kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Supplier{}, ErrNotFound
		}
		return Supplier{}, err
	}
	var used bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM procurement_orders WHERE supplier_id=$1)", id).Scan(&used); err != nil {
		return Supplier{}, err
	}
	if used && kind != input.Kind {
		return Supplier{}, &UserFacingError{Message: "Тип поставщика используется в расчётах существующих закупок. Создайте нового поставщика для другого типа."}
	}
	var item Supplier
	err = tx.QueryRow(ctx, `UPDATE procurement_suppliers SET name=$2,kind=$3,country_code=$4,tax_id=$5,kpp=$6,default_currency=$7,active=$8 WHERE id=$1 RETURNING id,name,kind,country_code,tax_id,kpp,default_currency,active,created_at`, id, input.Name, input.Kind, input.CountryCode, input.TaxID, input.KPP, input.DefaultCurrency, input.Active).Scan(&item.ID, &item.Name, &item.Kind, &item.CountryCode, &item.TaxID, &item.KPP, &item.DefaultCurrency, &item.Active, &item.CreatedAt)
	if err != nil {
		return Supplier{}, fmt.Errorf("update supplier: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.supplier.update", "procurement_supplier", id, item); err != nil {
		return Supplier{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Supplier{}, err
	}
	return item, nil
}

func (service *Service) DocumentPDF(ctx context.Context, id int64) ([]byte, error) {
	store, ok := service.store.(interface {
		DocumentPDF(context.Context, int64) ([]byte, error)
	})
	if !ok || id <= 0 {
		return nil, ErrInvalidInput
	}
	return store.DocumentPDF(ctx, id)
}

func (store *PostgresStore) DocumentPDF(ctx context.Context, id int64) ([]byte, error) {
	var content []byte
	err := store.pool.QueryRow(ctx, "SELECT content FROM procurement_documents WHERE id=$1", id).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return content, err
}

type ProductPage struct {
	Items        []ProductDirectoryItem `json:"items"`
	Total        int                    `json:"total"`
	FolderCounts map[string]int         `json:"folderCounts"`
}

func (service *Service) ProductPage(ctx context.Context, supplier int64, query, folder string, limit, offset int) (ProductPage, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return ProductPage{}, ErrInvalidInput
	}
	store, ok := service.store.(interface {
		ProductPage(context.Context, int64, string, string, int, int) (ProductPage, error)
	})
	if !ok {
		return ProductPage{}, ErrInvalidInput
	}
	return store.ProductPage(ctx, supplier, query, folder, limit, offset)
}
func (store *PostgresStore) ProductPage(ctx context.Context, supplier int64, query, folder string, limit, offset int) (ProductPage, error) {
	items, total, err := store.listProductPage(ctx, supplier, query, folder, limit, offset)
	if err != nil {
		return ProductPage{}, err
	}
	counts := map[string]int{}
	rows, err := store.pool.Query(ctx, `SELECT COALESCE(folder_saby_id,''),COUNT(*)::INTEGER FROM saby_nomenclature WHERE missing_since IS NULL AND ($1='' OR name ILIKE '%'||$1||'%' OR code ILIKE '%'||$1||'%' OR COALESCE(article,'') ILIKE '%'||$1||'%' OR saby_id ILIKE '%'||$1||'%') GROUP BY folder_saby_id`, query)
	if err != nil {
		return ProductPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return ProductPage{}, err
		}
		counts[id] = count
	}
	return ProductPage{Items: items, Total: total, FolderCounts: counts}, rows.Err()
}
