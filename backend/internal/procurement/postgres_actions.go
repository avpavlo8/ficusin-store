package procurement

// Товары поставщиков, наличие и очередь действий: то, что уходит наружу —
// в СБИС, на Wildberries и Ozon.
//
// Вынесено из postgres.go по той же причине, что и соседний файл.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (store *PostgresStore) ListProducts(ctx context.Context, supplierID int64, query string) ([]ProductDirectoryItem, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT n.saby_id, n.code, n.article, n.name, n.balance, n.price_minor::DOUBLE PRECISION / 100,
			sp.supplier_id, s.name, sp.supplier_article, sp.availability_status,
			COALESCE(sp.check_after::TEXT, ''), COALESCE(pc.holland_article, ''), pc.wb_nm_id,
			COALESCE(pc.wb_vendor_code, ''), COALESCE(NULLIF(pc.ozon_offer_id, ''), pc.ozon_article, ''),
			sp.minimum_order_qty, sp.order_multiple,
			COALESCE((SELECT ARRAY_AGG(a.raw_name ORDER BY a.last_seen_at DESC NULLS LAST, a.id DESC)
				FROM procurement_supplier_aliases a
				WHERE a.supplier_id = sp.supplier_id AND a.matched_saby_id = sp.saby_id), ARRAY[]::TEXT[]),
			COALESCE((SELECT ARRAY_AGG(a.id ORDER BY a.last_seen_at DESC NULLS LAST, a.id DESC)
				FROM procurement_supplier_aliases a
				WHERE a.supplier_id = sp.supplier_id AND a.matched_saby_id = sp.saby_id), ARRAY[]::BIGINT[])
		FROM procurement_supplier_products sp
		JOIN procurement_suppliers s ON s.id = sp.supplier_id
		JOIN saby_nomenclature n ON n.saby_id = sp.saby_id
		LEFT JOIN procurement_product_channels pc ON pc.saby_id = n.saby_id
		WHERE n.missing_since IS NULL AND n.saby_id ~ '^[0-9]+$'
			AND ($1 = 0 OR sp.supplier_id = $1) AND ($2 = '' OR n.name ILIKE '%' || $2 || '%'
			OR n.code ILIKE '%' || $2 || '%' OR n.article ILIKE '%' || $2 || '%'
			OR n.saby_id ILIKE '%' || $2 || '%' OR sp.supplier_article ILIKE '%' || $2 || '%')
		ORDER BY s.name, n.name LIMIT 500
	`, supplierID, query)
	if err != nil {
		return nil, fmt.Errorf("query procurement product directory: %w", err)
	}
	defer rows.Close()
	items := make([]ProductDirectoryItem, 0)
	for rows.Next() {
		var item ProductDirectoryItem
		if err := rows.Scan(&item.SabyID, &item.SabyCode, &item.SabyArticle, &item.Name,
			&item.Balance, &item.CurrentPriceRUB, &item.SupplierID, &item.SupplierName,
			&item.SupplierArticle, &item.AvailabilityStatus, &item.CheckAfter,
			&item.HollandArticle, &item.WBNmID, &item.WBVendorCode, &item.OzonOfferID,
			&item.MinimumOrderQty, &item.OrderMultiple,
			&item.Aliases, &item.AliasIDs); err != nil {
			return nil, fmt.Errorf("scan procurement product directory: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) UpdateProduct(ctx context.Context, actor Actor, input ProductDirectoryUpdate) (ProductDirectoryItem, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ProductDirectoryItem{}, fmt.Errorf("begin update procurement product: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM saby_nomenclature
		WHERE saby_id = $1 AND missing_since IS NULL AND saby_id ~ '^[0-9]+$')`, input.SabyID).Scan(&exists); err != nil || !exists {
		return ProductDirectoryItem{}, ErrNotFound
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO procurement_supplier_products (
			supplier_id, saby_id, supplier_article, availability_status, check_after, unavailable_since,
			minimum_order_qty, order_multiple, updated_by
		) VALUES ($1, $2, $3, $4, NULLIF($5, '')::DATE,
			CASE WHEN $4 = 'temporarily_unavailable' THEN CURRENT_DATE ELSE NULL END, $6, $7, $8)
		ON CONFLICT (supplier_id, saby_id) DO UPDATE SET supplier_article = EXCLUDED.supplier_article,
			availability_status = EXCLUDED.availability_status, check_after = EXCLUDED.check_after,
			unavailable_since = CASE WHEN EXCLUDED.availability_status = 'temporarily_unavailable'
				THEN COALESCE(procurement_supplier_products.unavailable_since, CURRENT_DATE) ELSE NULL END,
			minimum_order_qty = EXCLUDED.minimum_order_qty, order_multiple = EXCLUDED.order_multiple,
			updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
	`, input.SupplierID, input.SabyID, input.SupplierArticle, input.AvailabilityStatus, input.CheckAfter,
		input.MinimumOrderQty, input.OrderMultiple, actor.CustomerID)
	if err != nil {
		return ProductDirectoryItem{}, fmt.Errorf("upsert procurement supplier product: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO procurement_product_channels (
			saby_id, holland_article, wb_nm_id, wb_vendor_code, ozon_offer_id, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (saby_id) DO UPDATE SET holland_article = EXCLUDED.holland_article,
			wb_nm_id = EXCLUDED.wb_nm_id, wb_vendor_code = EXCLUDED.wb_vendor_code,
			ozon_offer_id = EXCLUDED.ozon_offer_id, updated_by = EXCLUDED.updated_by,
			updated_at = CURRENT_TIMESTAMP
	`, input.SabyID, input.HollandArticle, input.WBNmID, input.WBVendorCode, input.OzonOfferID, actor.CustomerID)
	if err != nil {
		return ProductDirectoryItem{}, fmt.Errorf("upsert procurement product channels: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_supplier_aliases SET availability_status = $3,
			check_after = NULLIF($4, '')::DATE,
			unavailable_since = CASE WHEN $3 = 'temporarily_unavailable' THEN COALESCE(unavailable_since, CURRENT_DATE) ELSE NULL END,
			updated_at = CURRENT_TIMESTAMP
		WHERE supplier_id = $1 AND matched_saby_id = $2
	`, input.SupplierID, input.SabyID, input.AvailabilityStatus, input.CheckAfter); err != nil {
		return ProductDirectoryItem{}, fmt.Errorf("sync procurement alias availability: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.product.update", "procurement_product", input.SupplierID, input); err != nil {
		return ProductDirectoryItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductDirectoryItem{}, fmt.Errorf("commit procurement product update: %w", err)
	}
	items, err := store.ListProducts(ctx, input.SupplierID, input.SabyID)
	if err != nil || len(items) == 0 {
		return ProductDirectoryItem{}, err
	}
	return items[0], nil
}

// UpdateAvailability помечает наличие у поставщика. Ключ — пара
// поставщик+товар: раньше статус жил на алиасе, и товар, чьё название ни
// разу не встретилось в разобранном PDF, пометить было нечем.
//
// Алиасы того же товара обновляем следом, чтобы очередь сопоставления не
// показывала вчерашнюю правду, но читаем мы теперь только пару.
func (store *PostgresStore) UpdateAvailability(ctx context.Context, actor Actor, input AvailabilityUpdate) (AvailabilityItem, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return AvailabilityItem{}, fmt.Errorf("begin update procurement availability: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	command, err := tx.Exec(ctx, `
		INSERT INTO procurement_supplier_products (
			supplier_id, saby_id, availability_status, check_after, unavailable_since, updated_by
		)
		SELECT $1, $2, $3, NULLIF($4, '')::DATE,
			CASE WHEN $3 = 'temporarily_unavailable' THEN CURRENT_DATE ELSE NULL END, $5
		WHERE EXISTS (SELECT 1 FROM procurement_suppliers WHERE id = $1)
			AND EXISTS (SELECT 1 FROM saby_nomenclature WHERE saby_id = $2)
		ON CONFLICT (supplier_id, saby_id) DO UPDATE SET
			availability_status = EXCLUDED.availability_status,
			check_after = EXCLUDED.check_after,
			unavailable_since = CASE WHEN EXCLUDED.availability_status = 'temporarily_unavailable'
				THEN COALESCE(procurement_supplier_products.unavailable_since, CURRENT_DATE) ELSE NULL END,
			updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
	`, input.SupplierID, input.SabyID, input.Status, input.CheckAfter, actor.CustomerID)
	if err != nil {
		return AvailabilityItem{}, fmt.Errorf("update procurement availability: %w", err)
	}
	if command.RowsAffected() == 0 {
		return AvailabilityItem{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_supplier_aliases SET availability_status = $3,
			check_after = NULLIF($4, '')::DATE,
			unavailable_since = CASE WHEN $3 = 'temporarily_unavailable'
				THEN COALESCE(unavailable_since, CURRENT_DATE) ELSE NULL END,
			updated_at = CURRENT_TIMESTAMP
		WHERE supplier_id = $1 AND matched_saby_id = $2
	`, input.SupplierID, input.SabyID, input.Status, input.CheckAfter); err != nil {
		return AvailabilityItem{}, fmt.Errorf("sync procurement alias availability: %w", err)
	}
	var item AvailabilityItem
	if err := tx.QueryRow(ctx, `
		SELECT sp.supplier_id, s.name, sp.saby_id, COALESCE(n.name, ''),
			COALESCE(sp.supplier_article, ''), sp.availability_status,
			COALESCE(sp.check_after::TEXT, ''), COALESCE(sp.unavailable_since::TEXT, ''),
			COALESCE(n.balance, 0)
		FROM procurement_supplier_products sp
		JOIN procurement_suppliers s ON s.id = sp.supplier_id
		LEFT JOIN saby_nomenclature n ON n.saby_id = sp.saby_id
		WHERE sp.supplier_id = $1 AND sp.saby_id = $2
	`, input.SupplierID, input.SabyID).Scan(&item.SupplierID, &item.SupplierName, &item.SabyID,
		&item.Name, &item.SupplierArticle, &item.Status, &item.CheckAfter,
		&item.UnavailableSince, &item.Balance); errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityItem{}, ErrNotFound
	} else if err != nil {
		return AvailabilityItem{}, fmt.Errorf("load procurement availability: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.availability.update", "procurement_supplier_product", input.SupplierID, item); err != nil {
		return AvailabilityItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AvailabilityItem{}, fmt.Errorf("commit procurement availability: %w", err)
	}
	return item, nil
}

// SetExclusion снимает товар с закупки или возвращает его обратно. Решение
// магазина, поэтому без поставщика и с обязательной причиной при снятии:
// через месяц «почему это не заказывается» — обычный вопрос.
func (store *PostgresStore) SetExclusion(ctx context.Context, actor Actor, input ExclusionUpdate) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin procurement exclusion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if input.Excluded {
		command, err := tx.Exec(ctx, `
			INSERT INTO procurement_excluded_products (saby_id, reason, updated_by)
			SELECT $1, $2, $3 FROM saby_nomenclature WHERE saby_id = $1
			ON CONFLICT (saby_id) DO UPDATE SET reason = EXCLUDED.reason,
				excluded_at = CURRENT_TIMESTAMP, updated_by = EXCLUDED.updated_by
		`, input.SabyID, input.Reason, actor.CustomerID)
		if err != nil {
			return fmt.Errorf("insert procurement exclusion: %w", err)
		}
		if command.RowsAffected() == 0 {
			return ErrNotFound
		}
	} else if _, err := tx.Exec(ctx,
		`DELETE FROM procurement_excluded_products WHERE saby_id = $1`, input.SabyID); err != nil {
		return fmt.Errorf("delete procurement exclusion: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.exclusion.update", "procurement_excluded_product", 0, input); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit procurement exclusion: %w", err)
	}
	return nil
}
func (store *PostgresStore) PrepareBatch(ctx context.Context, actor Actor, orderID int64, kind string, channels []string) (ActionBatch, error) {
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
	var existingID int64
	var existingStatus string
	err = tx.QueryRow(ctx, `
		SELECT id, status FROM procurement_action_batches
		WHERE procurement_order_id = $1 AND kind = $2 AND status NOT IN ('cancelled', 'completed')
		ORDER BY id DESC LIMIT 1 FOR UPDATE
	`, orderID, kind).Scan(&existingID, &existingStatus)
	if err == nil && existingStatus != "draft" {
		return ActionBatch{}, ErrInvalidInput
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, fmt.Errorf("lock procurement action batch: %w", err)
	}
	if existingID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE procurement_action_batches SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, existingID); err != nil {
			return ActionBatch{}, fmt.Errorf("cancel stale procurement action batch: %w", err)
		}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO procurement_action_batches (
			procurement_order_id, kind, created_by, calculation_version, calculated_at
		) SELECT id, $2, $3, calculation_version, calculated_at
		FROM procurement_orders WHERE id = $1 RETURNING id
	`, orderID, kind, actor.CustomerID).Scan(&batchID)
	if err != nil {
		return ActionBatch{}, fmt.Errorf("create procurement action batch: %w", err)
	}
	if kind == "receipt" {
		_, err = tx.Exec(ctx, `
			WITH receipt_lines AS (
				SELECT MIN(l.id) AS id, o.id AS order_id, o.order_number,
					supplier.name AS supplier_name, supplier.tax_id AS supplier_tax_id, supplier.kpp AS supplier_kpp,
					l.saby_id, COALESCE(NULLIF(n.code, ''), l.saby_id) AS code,
					COALESCE(NULLIF(n.name, ''), MAX(l.raw_name)) AS name,
					n.balance::INTEGER AS old_balance,
					MAX(COALESCE(l.unit_cost_rub, l.purchase_unit_rub, l.unit_price, l.expected_unit_price, 0))::DOUBLE PRECISION AS unit_cost,
					SUM(COALESCE(l.invoiced_qty, l.ordered_qty))::INTEGER AS quantity
				FROM procurement_order_lines l
				JOIN procurement_orders o ON o.id = l.procurement_order_id
				JOIN procurement_suppliers supplier ON supplier.id = o.supplier_id
				JOIN saby_nomenclature n ON n.saby_id = l.saby_id
				WHERE l.procurement_order_id = $2 AND l.match_status = 'confirmed'
					AND l.saby_id IS NOT NULL
				GROUP BY o.id, o.order_number, supplier.name, supplier.tax_id, supplier.kpp,
					l.saby_id, n.code, n.name, n.balance
			)
			INSERT INTO procurement_action_items (
				batch_id, procurement_order_line_id, channel, external_article, new_value, quantity, payload
			)
			SELECT $1, MIN(id), 'saby_receipt', order_id::TEXT, 0, SUM(quantity)::INTEGER,
				jsonb_build_object(
					'orderId', order_id,
					'orderNumber', order_number,
					'supplier', jsonb_build_object('name', supplier_name, 'taxId', supplier_tax_id, 'kpp', supplier_kpp),
					'lines', jsonb_agg(jsonb_build_object(
						'sabyId', saby_id, 'code', code, 'name', name, 'quantity', quantity, 'unitCost', unit_cost,
						'oldBalance', old_balance, 'newBalance', old_balance + quantity
					) ORDER BY id)
				)
			FROM receipt_lines
			GROUP BY order_id, order_number, supplier_name, supplier_tax_id, supplier_kpp
		`, batchID, orderID)
	} else {
		// Один товар СБИС может встретиться в накладной несколько раз с
		// разной закупочной ценой. Для продажи безопасно брать максимальную
		// рассчитанную цену: так дешёвая строка не заставит продавать более
		// дорогую поставку в убыток и Ozon не получит две команды на один SKU.
		_, err = tx.Exec(ctx, `
			WITH products AS (
				SELECT saby_id, MIN(id) AS line_id, MAX(proposed_retail_rub) AS retail,
					MAX(proposed_marketplace_rub) AS marketplace,
					MAX(proposed_marketplace_strike_rub) AS strike
				FROM procurement_order_lines
				WHERE procurement_order_id = $2 AND match_status = 'confirmed'
					AND saby_id IS NOT NULL AND proposed_retail_rub IS NOT NULL
				GROUP BY saby_id
			)
			INSERT INTO procurement_action_items (
				batch_id, procurement_order_line_id, channel, external_article, old_value, new_value, compare_at_value
			)
			SELECT $1, p.line_id, channel.name,
				CASE channel.name WHEN 'wb' THEN COALESCE(pc.wb_nm_id::TEXT, '')
					WHEN 'ozon' THEN COALESCE(NULLIF(pc.ozon_offer_id, ''), pc.ozon_article, '') ELSE p.saby_id END,
				CASE channel.name
					WHEN 'site' THEN COALESCE(pv.base_price_minor, 0)::NUMERIC / 100
					WHEN 'saby_price' THEN COALESCE(n.price_minor, 0)::NUMERIC / 100
					WHEN 'wb' THEN wb_product.current_price
					WHEN 'ozon' THEN ozon_product.current_price
					ELSE NULL
				END,
				CASE WHEN channel.name IN ('wb', 'ozon') THEN p.marketplace ELSE p.retail END,
				CASE WHEN channel.name IN ('wb', 'ozon') THEN p.strike ELSE NULL END
			FROM products p
			JOIN saby_nomenclature n ON n.saby_id = p.saby_id
			LEFT JOIN LATERAL (
				SELECT MAX(base_price_minor) AS base_price_minor FROM product_variants WHERE saby_id = p.saby_id
			) pv ON TRUE
			LEFT JOIN procurement_product_channels pc ON pc.saby_id = p.saby_id
			LEFT JOIN procurement_channel_products wb_product
				ON wb_product.channel = 'wb' AND wb_product.external_id = pc.wb_nm_id::TEXT
			LEFT JOIN procurement_channel_products ozon_product
				ON ozon_product.channel = 'ozon'
				AND ozon_product.external_id = COALESCE(NULLIF(pc.ozon_offer_id, ''), pc.ozon_article, '')
			CROSS JOIN (VALUES ('site'), ('wb'), ('ozon')) AS channel(name)
			WHERE channel.name = ANY($3::TEXT[])
		`, batchID, orderID, channels)
		if err == nil && containsString(channels, "saby_price") {
			_, err = tx.Exec(ctx, `
				WITH products AS (
					SELECT MIN(l.id) AS id, l.saby_id, COALESCE(NULLIF(n.code, ''), l.saby_id) AS code,
						n.name, n.price_minor::NUMERIC / 100 AS old_price, MAX(l.proposed_retail_rub) AS new_price
					FROM procurement_order_lines l
					JOIN saby_nomenclature n ON n.saby_id = l.saby_id
					WHERE l.procurement_order_id = $2 AND l.match_status = 'confirmed'
						AND l.saby_id IS NOT NULL AND l.proposed_retail_rub IS NOT NULL
					GROUP BY l.saby_id, n.code, n.name, n.price_minor
				)
				INSERT INTO procurement_action_items (
					batch_id, procurement_order_line_id, channel, external_article,
					old_value, new_value, payload
				)
				SELECT $1, MIN(id), 'saby_price', $2::TEXT, MIN(old_price), 0,
					jsonb_build_object(
						'orderId', $2,
						'lines', jsonb_agg(jsonb_build_object(
							'sabyId', saby_id, 'code', code, 'name', name,
							'oldPrice', old_price, 'newPrice', new_price
						) ORDER BY id)
					)
				FROM products
				HAVING COUNT(*) > 0
			`, batchID, orderID)
		}
	}
	if err != nil {
		return ActionBatch{}, fmt.Errorf("fill procurement action batch: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.batch.prepare", "procurement_action_batch", batchID, map[string]any{"kind": kind, "channels": channels}); err != nil {
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
				WHEN channel = 'saby_receipt' AND $4 THEN 'queued'
				WHEN channel = 'saby_price' AND $5 THEN 'queued'
				WHEN channel IN ('wb', 'ozon') AND external_article = '' THEN 'skipped'
				ELSE 'not_configured' END,
			error_message = CASE
				WHEN channel = 'site' OR (channel = 'wb' AND $2 AND external_article <> '') OR (channel = 'ozon' AND $3 AND external_article <> '')
					OR (channel = 'saby_receipt' AND $4) OR (channel = 'saby_price' AND $5) THEN ''
				WHEN channel = 'wb' AND external_article = '' THEN 'Не заполнен WB nmID. Укажите его в справочнике закупок или нажмите «Подтянуть артикулы».'
				WHEN channel = 'ozon' AND external_article = '' THEN 'Не заполнен Ozon offer_id. Укажите его в справочнике закупок или нажмите «Подтянуть артикулы».'
				ELSE 'API-адаптер канала не настроен' END,
			completed_at = CASE WHEN channel = 'site' THEN CURRENT_TIMESTAMP ELSE NULL END,
			next_attempt_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE batch_id = $1
	`, batchID, configured["wb"], configured["ozon"], configured["saby_receipt"], configured["saby_price"]); err != nil {
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
			SELECT item.id, item.procurement_order_line_id, COALESCE(n.name, line.raw_name), COALESCE(n.code, ''),
				item.channel, item.external_article, item.old_value::DOUBLE PRECISION,
				item.new_value::DOUBLE PRECISION, item.compare_at_value::DOUBLE PRECISION,
				item.quantity, item.status, item.error_message, item.external_operation_id, item.external_url, item.payload
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
			if err := itemRows.Scan(&item.ID, &item.LineID, &item.ProductName, &item.ProductCode, &item.Channel,
				&item.ExternalArticle, &item.OldValue, &item.NewValue, &item.CompareAtValue, &item.Quantity,
				&item.Status, &item.ErrorMessage, &item.ExternalOperationID, &item.ExternalURL, &item.Payload); err != nil {
				itemRows.Close()
				return nil, fmt.Errorf("scan procurement batch item: %w", err)
			}
			populateActionPreview(&item)
			batch.Items = append(batch.Items, item)
		}
		itemRows.Close()
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func populateActionPreview(item *ActionItem) {
	if item == nil || len(item.Payload) == 0 {
		return
	}
	var payload struct {
		Lines []ActionPreviewLine `json:"lines"`
	}
	if json.Unmarshal(item.Payload, &payload) == nil {
		item.PreviewLines = payload.Lines
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
			item.compare_at_value::DOUBLE PRECISION, item.quantity, item.external_operation_id,
			item.external_url, item.payload, item.attempts
	`).Scan(&item.ID, &item.LineID, &item.Channel, &item.ExternalArticle, &item.OldValue,
		&item.NewValue, &item.CompareAtValue, &item.Quantity, &item.ExternalOperationID,
		&item.ExternalURL, &item.Payload, &item.Attempts)
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
		// A marketplace 429 is known not to have applied the mutation and
		// carries RetryAfter. Keep it queued instead of turning a temporary
		// seller-wide limit into a permanent error after five attempts.
		if attempts >= 5 && result.RetryAfter <= 0 {
			status = "failed"
		} else if result.RetryAfter <= 0 {
			delay = time.Duration(attempts*attempts) * 15 * time.Second
		}
	} else if result.Completed {
		status = "completed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE procurement_action_items SET status = $2, error_message = $3,
			external_operation_id = CASE WHEN $4 = '' THEN external_operation_id ELSE $4 END,
			external_url = CASE WHEN $5 = '' THEN external_url ELSE $5 END,
			completed_at = CASE WHEN $2 = 'completed' THEN CURRENT_TIMESTAMP ELSE NULL END,
			next_attempt_at = CURRENT_TIMESTAMP + ($6 * INTERVAL '1 second'), locked_until = NULL,
			updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, actionID, status, message, result.ExternalOperationID, result.ExternalURL, int(delay.Seconds())); err != nil {
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
			(status = 'failed' AND channel IN ('wb', 'ozon', 'saby_receipt', 'saby_price')) OR
			(status = 'not_configured' AND ((channel = 'wb' AND $2) OR (channel = 'ozon' AND $3)
				OR (channel = 'saby_receipt' AND $4) OR (channel = 'saby_price' AND $5)))
		)
	`, batchID, configured["wb"], configured["ozon"], configured["saby_receipt"], configured["saby_price"])
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
	if sabyID != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO procurement_supplier_products (supplier_id, saby_id, supplier_article, availability_status)
			VALUES ($1, $2, $3, 'available')
			ON CONFLICT (supplier_id, saby_id) DO UPDATE SET availability_status = 'available',
				unavailable_since = NULL, check_after = NULL,
				supplier_article = CASE WHEN EXCLUDED.supplier_article <> '' THEN EXCLUDED.supplier_article ELSE procurement_supplier_products.supplier_article END,
				updated_at = CURRENT_TIMESTAMP
		`, supplierID, sabyID, line.SupplierArticle); err != nil {
			return 0, "", "", fmt.Errorf("refresh supplier product availability: %w", err)
		}
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

// LinkChannelProducts связывает карточки маркетплейса с номенклатурой СБИС.
//
// Связь ставится только по точному совпадению кода, артикула или штрихкода
// и только там, где поле канала ещё пустое: подтягивание справочника не
// должно переписывать связь, которую человек проставил руками. Ключ,
// совпавший больше чем с одним товаром, пропускается — лучше оставить
// пустым, чем приписать продажи чужому растению.
func (store *PostgresStore) LinkChannelProducts(
	ctx context.Context,
	actor Actor,
	channel string,
	items []ChannelProduct,
) (ChannelLinkResult, error) {
	result := ChannelLinkResult{Channel: channel, Fetched: len(items)}
	rows, err := store.pool.Query(ctx, `
		SELECT saby_id, LOWER(TRIM(code)), LOWER(TRIM(article)), LOWER(TRIM(barcode)), barcodes
		FROM saby_nomenclature WHERE missing_since IS NULL
	`)
	if err != nil {
		return ChannelLinkResult{}, fmt.Errorf("query saby nomenclature keys: %w", err)
	}
	defer rows.Close()
	bySabyKey := make(map[string]string, 4096)
	ambiguous := make(map[string]bool)
	for rows.Next() {
		var sabyID, code, article, barcode string
		var barcodes []string
		if err := rows.Scan(&sabyID, &code, &article, &barcode, &barcodes); err != nil {
			return ChannelLinkResult{}, fmt.Errorf("scan saby nomenclature key: %w", err)
		}
		// Штрихкод, выданный маркетплейсом, — единственное, что совпадает у
		// СБИС и площадки: код товара и артикул у них свои.
		keys := []string{code, article, barcode}
		for _, extra := range barcodes {
			keys = append(keys, strings.ToLower(strings.TrimSpace(extra)))
		}
		for _, key := range keys {
			if key == "" {
				continue
			}
			if existing, seen := bySabyKey[key]; seen && existing != sabyID {
				ambiguous[key] = true
				continue
			}
			bySabyKey[key] = sabyID
			if len(result.CatalogSamples) < 5 {
				result.CatalogSamples = append(result.CatalogSamples, key)
			}
		}
	}
	if rows.Err() != nil {
		return ChannelLinkResult{}, fmt.Errorf("read saby nomenclature keys: %w", rows.Err())
	}
	result.CatalogKeys = len(bySabyKey)

	matched := make(map[string]string, len(items))
	for _, item := range items {
		keys := append([]string{item.Article}, item.Barcodes...)
		found := false
		for _, key := range keys {
			key = strings.ToLower(strings.TrimSpace(key))
			if key == "" {
				continue
			}
			result.ChannelKeys++
			if ambiguous[key] {
				continue
			}
			if sabyID, ok := bySabyKey[key]; ok && !found {
				matched[sabyID] = item.ExternalID
				found = true
			}
		}
		if !found && len(result.ChannelSamples) < 5 {
			sample := strings.TrimSpace(item.Article)
			if len(item.Barcodes) > 0 {
				sample += " / " + item.Barcodes[0]
			}
			result.ChannelSamples = append(result.ChannelSamples, strings.TrimSpace(sample))
		}
	}
	result.Unmatched = len(items) - len(matched)

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ChannelLinkResult{}, fmt.Errorf("begin channel link: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for sabyID, externalID := range matched {
		var command pgconn.CommandTag
		if channel == "wb" {
			nmID, convErr := strconv.ParseInt(externalID, 10, 64)
			if convErr != nil {
				continue
			}
			command, err = tx.Exec(ctx, `
				INSERT INTO procurement_product_channels (saby_id, wb_nm_id, updated_by)
				VALUES ($1, $2, $3)
				ON CONFLICT (saby_id) DO UPDATE SET wb_nm_id = EXCLUDED.wb_nm_id,
					updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
				WHERE procurement_product_channels.wb_nm_id IS NULL
			`, sabyID, nmID, actor.CustomerID)
		} else {
			command, err = tx.Exec(ctx, `
				INSERT INTO procurement_product_channels (saby_id, ozon_offer_id, updated_by)
				VALUES ($1, $2, $3)
				ON CONFLICT (saby_id) DO UPDATE SET ozon_offer_id = EXCLUDED.ozon_offer_id,
					updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
				WHERE procurement_product_channels.ozon_offer_id = ''
			`, sabyID, externalID, actor.CustomerID)
		}
		if err != nil {
			return ChannelLinkResult{}, fmt.Errorf("link channel product: %w", err)
		}
		result.Linked += int(command.RowsAffected())
	}
	if err := audit(ctx, tx, actor, "procurement.channel.link", "procurement_product_channels", 0, result); err != nil {
		return ChannelLinkResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelLinkResult{}, fmt.Errorf("commit channel link: %w", err)
	}
	return result, nil
}
