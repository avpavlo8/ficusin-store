package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (repository *PostgresRepository) ListCustomers(ctx context.Context) ([]Customer, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT c.id, COALESCE(c.email, ''), c.phone, c.full_name, c.last_name,
			c.patronymic, c.delivery_address, c.account_type, c.wholesale_status,
			c.retail_discount_bps, c.lifetime_spend_minor::DOUBLE PRECISION / 100,
			c.is_active, COALESCE(au.role, ''), COUNT(o.id)::INTEGER, c.created_at
		FROM customers c
		LEFT JOIN LATERAL (
			SELECT role FROM admin_users
			WHERE is_active = TRUE AND customer_id = c.id LIMIT 1
		) au ON TRUE
		LEFT JOIN orders o ON o.customer_id = c.id
		GROUP BY c.id, au.role
		ORDER BY c.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query admin customers: %w", err)
	}
	defer rows.Close()
	result := make([]Customer, 0)
	for rows.Next() {
		var item Customer
		if err := rows.Scan(
			&item.ID, &item.Email, &item.Phone, &item.FullName, &item.LastName,
			&item.Patronymic, &item.DeliveryAddress, &item.AccountType,
			&item.WholesaleStatus, &item.RetailDiscountBPS, &item.LifetimeSpend,
			&item.Active, &item.AdminRole, &item.OrdersCount, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admin customer: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) UpdateCustomer(
	ctx context.Context,
	actor Actor,
	id int64,
	update CustomerUpdate,
) (Customer, error) {
	if !Can(actor.Role, PermissionCustomersEdit) {
		return Customer{}, ErrForbidden
	}
	if update.AdminRole != nil && actor.Role != RoleOwner {
		return Customer{}, ErrForbidden
	}
	if (update.RetailDiscountBPS != nil || update.Active != nil) && actor.Role != RoleOwner {
		return Customer{}, ErrForbidden
	}
	if id == actor.CustomerID && ((update.Active != nil && !*update.Active) ||
		(update.AdminRole != nil && *update.AdminRole != RoleOwner)) {
		return Customer{}, ErrForbidden
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Customer{}, fmt.Errorf("begin customer update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'fullName', full_name, 'email', email, 'address', delivery_address,
			'accountType', account_type, 'wholesaleStatus', wholesale_status,
			'discountBps', retail_discount_bps, 'active', is_active
		) FROM customers WHERE id = $1 FOR UPDATE
	`, id).Scan(&before); err != nil {
		return Customer{}, fmt.Errorf("lock customer: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE customers SET
			full_name = COALESCE($2, full_name), last_name = COALESCE($3, last_name),
			patronymic = COALESCE($4, patronymic), email = COALESCE($5, email),
			delivery_address = COALESCE($6, delivery_address),
			account_type = COALESCE($7, account_type),
			wholesale_status = COALESCE($8, wholesale_status),
			retail_discount_bps = COALESCE($9, retail_discount_bps),
			is_active = COALESCE($10, is_active), updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, update.FullName, update.LastName, update.Patronymic, update.Email,
		update.DeliveryAddress, update.AccountType, update.WholesaleStatus,
		update.RetailDiscountBPS, update.Active)
	if err != nil {
		return Customer{}, fmt.Errorf("update customer: %w", err)
	}

	if update.AdminRole != nil {
		if *update.AdminRole == "" {
			if _, err := tx.Exec(ctx, "DELETE FROM admin_users WHERE customer_id = $1", id); err != nil {
				return Customer{}, fmt.Errorf("remove admin role: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx, `
				INSERT INTO admin_users (customer_id, email, role, is_active, updated_at)
				SELECT id, email, $2, TRUE, CURRENT_TIMESTAMP FROM customers WHERE id = $1
				ON CONFLICT (customer_id) WHERE customer_id IS NOT NULL DO UPDATE SET
					role = EXCLUDED.role, email = EXCLUDED.email,
					is_active = TRUE, updated_at = CURRENT_TIMESTAMP
			`, id, *update.AdminRole); err != nil {
				return Customer{}, fmt.Errorf("assign admin role: %w", err)
			}
		}
	}

	var after map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'fullName', c.full_name, 'email', c.email, 'address', c.delivery_address,
			'accountType', c.account_type, 'wholesaleStatus', c.wholesale_status,
			'discountBps', c.retail_discount_bps, 'active', c.is_active,
			'adminRole', COALESCE(au.role, '')
		) FROM customers c LEFT JOIN admin_users au ON au.customer_id = c.id
		WHERE c.id = $1
	`, id).Scan(&after); err != nil {
		return Customer{}, fmt.Errorf("read updated customer: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "customer.update", "customer", fmt.Sprint(id), before, after); err != nil {
		return Customer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Customer{}, fmt.Errorf("commit customer update: %w", err)
	}
	customers, err := repository.ListCustomers(ctx)
	if err != nil {
		return Customer{}, err
	}
	for _, customer := range customers {
		if customer.ID == id {
			return customer, nil
		}
	}
	return Customer{}, pgx.ErrNoRows
}

func (repository *PostgresRepository) ListOrders(ctx context.Context) ([]Order, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id, order_number, customer_id, customer_name, phone, email, address,
			comment, delivery_method, delivery_fee_pending = 1,
			delivery_repack_requested = 1, payment_method, payment_status,
			COALESCE(cdek_track_number, ''), has_preorder = 1, status,
			total::DOUBLE PRECISION, created_at
		FROM orders ORDER BY created_at DESC LIMIT 1000
	`)
	if err != nil {
		return nil, fmt.Errorf("query admin orders: %w", err)
	}
	defer rows.Close()
	orders := make([]Order, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var item Order
		if err := rows.Scan(&item.ID, &item.OrderNumber, &item.CustomerID,
			&item.CustomerName, &item.Phone, &item.Email, &item.Address, &item.Comment,
			&item.DeliveryMethod, &item.DeliveryFeePending, &item.RepackRequested,
			&item.PaymentMethod, &item.PaymentStatus, &item.TrackNumber,
			&item.HasPreorder, &item.Status, &item.Total,
			&item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan admin order: %w", err)
		}
		item.Items = []OrderItem{}
		ids = append(ids, item.ID)
		orders = append(orders, item)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return orders, err
	}
	itemRows, err := repository.pool.Query(ctx, `
		SELECT order_id, product_id, sku, variant_label, product_name, unit_price::DOUBLE PRECISION, quantity
		FROM order_items WHERE order_id = ANY($1::bigint[]) ORDER BY id
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query admin order items: %w", err)
	}
	defer itemRows.Close()
	byID := make(map[int64]*Order, len(orders))
	for index := range orders {
		byID[orders[index].ID] = &orders[index]
	}
	for itemRows.Next() {
		var orderID int64
		var item OrderItem
		if err := itemRows.Scan(&orderID, &item.ProductID, &item.SKU, &item.VariantLabel, &item.ProductName,
			&item.UnitPrice, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan admin order item: %w", err)
		}
		byID[orderID].Items = append(byID[orderID].Items, item)
	}
	return orders, itemRows.Err()
}

func (repository *PostgresRepository) UpdateOrderStatus(
	ctx context.Context,
	actor Actor,
	id int64,
	status, paymentStatus string,
) (Order, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return Order{}, ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin order update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object('status', status, 'paymentStatus', payment_status)
		FROM orders WHERE id = $1 FOR UPDATE
	`, id).Scan(&before); err != nil {
		return Order{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE orders SET status = COALESCE(NULLIF($2, ''), status),
			payment_status = COALESCE(NULLIF($3, ''), payment_status)
		WHERE id = $1
	`, id, status, paymentStatus); err != nil {
		return Order{}, fmt.Errorf("update order status: %w", err)
	}
	if status == "cancelled" {
		if err := order.ReleaseStock(ctx, tx, id); err != nil {
			return Order{}, err
		}
		// An unfinished payment for a cancelled order is over. Left open it
		// would keep the reconciliation loop asking YooKassa about it every
		// minute for nothing.
		if _, err := tx.Exec(ctx, `
			UPDATE payments SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
			WHERE order_id = $1 AND status = 'pending'
		`, id); err != nil {
			return Order{}, fmt.Errorf("cancel payments: %w", err)
		}
	}
	var after map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object('status', status, 'paymentStatus', payment_status)
		FROM orders WHERE id = $1
	`, id).Scan(&after); err != nil {
		return Order{}, err
	}
	if err := insertAudit(ctx, tx, actor, "order.status.update", "order", fmt.Sprint(id), before, after); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit order update: %w", err)
	}
	// A letter about the same milestone. Queued, not sent: the customer
	// hears about it either way, and a mail server having a bad minute must
	// not undo a status change.
	if status != "" {
		_ = repository.queueStatusLetter(ctx, id, status)
	}
	// Told after the commit, and never at the cost of the update: a
	// notification that fails to send must not undo a status change.
	if status != "" && repository.notifier != nil {
		var customerID *int64
		var orderNumber string
		if err := repository.pool.QueryRow(ctx,
			"SELECT customer_id, order_number FROM orders WHERE id = $1", id,
		).Scan(&customerID, &orderNumber); err == nil && customerID != nil {
			_ = repository.notifier.NotifyOrderStatus(ctx, *customerID, orderNumber, status)
		}
	}
	orders, err := repository.ListOrders(ctx)
	if err != nil {
		return Order{}, err
	}
	for _, order := range orders {
		if order.ID == id {
			return order, nil
		}
	}
	return Order{}, pgx.ErrNoRows
}

func (repository *PostgresRepository) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT p.id, COALESCE(p.saby_id, ''), p.slug, p.name, p.latin_name,
			p.short_description, p.description, p.care_instructions, p.status,
			p.is_featured <> 0, p.catalog_section, COALESCE(p.plant_kind, ''),
			COALESCE(p.light_level, ''), COALESCE(p.watering, ''),
			COALESCE(p.height_class, ''), COALESCE(p.care_level, ''),
			COALESCE(p.placement, ''), COALESCE(p.pet_safety, ''),
			COALESCE(p.growth_habit, ''),
			COALESCE((SELECT COALESCE(mm.card_url, pm.object_key) FROM product_media pm
				LEFT JOIN media_mirror mm ON mm.source_url=pm.object_key WHERE pm.product_id = p.id
				ORDER BY pm.is_primary DESC, pm.sort_order LIMIT 1), ''),
			COALESCE(pv.base_price_minor, 0)::DOUBLE PRECISION / 100,
			COALESCE((SELECT SUM(GREATEST(available_qty - reserved_qty, 0))
				FROM inventory WHERE variant_id = pv.id), 0)::INTEGER,
			COALESCE(pv.sku, ''), COALESCE(pv.label, ''), pv.height_cm,
			pv.pot_diameter_cm, pv.package_length_cm, pv.package_width_cm,
			pv.package_height_cm, pv.package_weight_grams,
			COALESCE(pv.wholesale_min_qty, 1),
			ARRAY(SELECT DISTINCT unnest(p.override_fields || COALESCE(pv.override_fields, '{}'))),
			p.saby_fields,
			COALESCE((SELECT source.code FROM saby_nomenclature source WHERE source.saby_id = p.saby_id), ''),
			p.saby_updated_at, p.category_id, p.plant_passport, p.important_warnings,
			COALESCE((SELECT jsonb_agg(jsonb_build_object(
				'provider', e.provider, 'type', e.id_type, 'externalId', e.external_id)
				ORDER BY e.provider,e.id_type) FROM product_external_ids e WHERE e.product_id=p.id), '[]'::jsonb),
			COALESCE((SELECT jsonb_object_agg(a.code,v.value) FROM product_attribute_values v
				JOIN attribute_definitions a ON a.id=v.attribute_id WHERE v.product_id=p.id), '{}'::jsonb)
		FROM products p
		LEFT JOIN LATERAL (
			SELECT * FROM product_variants WHERE product_id = p.id
			ORDER BY is_active DESC, id LIMIT 1
		) pv ON TRUE
		ORDER BY p.updated_at DESC, p.name
	`)
	if err != nil {
		return nil, fmt.Errorf("query admin products: %w", err)
	}
	defer rows.Close()
	products := make([]Product, 0)
	for rows.Next() {
		var item Product
		var externalIDs []byte
		var attributes []byte
		if err := rows.Scan(&item.ID, &item.SabyID, &item.Slug, &item.Name,
			&item.LatinName, &item.ShortDescription, &item.Description,
			&item.CareInstructions, &item.Status, &item.Featured,
			&item.CatalogSection, &item.PlantKind, &item.LightLevel, &item.Watering,
			&item.HeightClass, &item.CareLevel, &item.Placement, &item.PetSafety,
			&item.GrowthHabit, &item.Image, &item.Price, &item.Stock, &item.SKU, &item.VariantLabel, &item.HeightCM,
			&item.PotDiameterCM, &item.PackageLengthCM, &item.PackageWidthCM,
			&item.PackageHeightCM, &item.PackageWeightGrams, &item.WholesaleMinQty,
			&item.OverrideFields, &item.SabyFields, &item.SabyCode,
			&item.SabyUpdatedAt, &item.CategoryID, &item.Passport, &item.ImportantWarnings,
			&externalIDs, &attributes); err != nil {
			return nil, fmt.Errorf("scan admin product: %w", err)
		}
		if err := json.Unmarshal(externalIDs, &item.ExternalIDs); err != nil {
			return nil, fmt.Errorf("decode product external IDs: %w", err)
		}
		if err := json.Unmarshal(attributes, &item.Attributes); err != nil {
			return nil, fmt.Errorf("decode product attributes: %w", err)
		}
		products = append(products, item)
	}
	return products, rows.Err()
}

func (repository *PostgresRepository) UpdateProduct(
	ctx context.Context,
	actor Actor,
	id int64,
	update ProductUpdate,
) (Product, error) {
	if !Can(actor.Role, PermissionProductsEdit) {
		return Product{}, ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := productAuditData(ctx, tx, id)
	if err != nil {
		return Product{}, err
	}
	productFields := changedProductFields(update)
	variantFields := changedVariantFields(update)
	_, err = tx.Exec(ctx, `
		UPDATE products SET name = COALESCE($2, name), latin_name = COALESCE($3, latin_name),
			short_description = COALESCE($4, short_description),
			description = COALESCE($5, description),
			care_instructions = COALESCE($6, care_instructions),
			status = COALESCE($7, status), is_featured = COALESCE($8, is_featured <> 0)::int,
			override_fields = ARRAY(SELECT DISTINCT unnest(override_fields || $9::text[])),
			updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, id, update.Name, update.LatinName, update.ShortDescription, update.Description,
		update.CareInstructions, update.Status, update.Featured, productFields)
	if err != nil {
		return Product{}, fmt.Errorf("update product: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE products SET
			catalog_section = COALESCE(NULLIF($2, ''), catalog_section),
			plant_kind = CASE WHEN $3::text IS NULL THEN plant_kind ELSE NULLIF($3, '') END,
			light_level = CASE WHEN $4::text IS NULL THEN light_level ELSE NULLIF($4, '') END,
			watering = CASE WHEN $5::text IS NULL THEN watering ELSE NULLIF($5, '') END,
			height_class = CASE WHEN $6::text IS NULL THEN height_class ELSE NULLIF($6, '') END,
			care_level = CASE WHEN $7::text IS NULL THEN care_level ELSE NULLIF($7, '') END,
			placement = CASE WHEN $8::text IS NULL THEN placement ELSE NULLIF($8, '') END,
			pet_safety = CASE WHEN $9::text IS NULL THEN pet_safety ELSE NULLIF($9, '') END,
			growth_habit = CASE WHEN $10::text IS NULL THEN growth_habit ELSE NULLIF($10, '') END,
			category_id = COALESCE($11, category_id),
			saby_fields = COALESCE($12, saby_fields),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, update.CatalogSection, update.PlantKind, update.LightLevel, update.Watering,
		update.HeightClass, update.CareLevel, update.Placement, update.PetSafety,
		update.GrowthHabit, update.CategoryID, update.SabyFields)
	if err != nil {
		return Product{}, fmt.Errorf("update product attributes: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE products SET plant_passport=COALESCE($2, plant_passport), important_warnings=COALESCE($3, important_warnings), updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, update.Passport, update.ImportantWarnings)
	if err != nil { return Product{}, fmt.Errorf("update plant passport: %w", err) }
	_, err = tx.Exec(ctx, `
		UPDATE product_variants SET label = COALESCE($2, label),
			base_price_minor = COALESCE($3, base_price_minor),
			height_cm = COALESCE($4, height_cm), pot_diameter_cm = COALESCE($5, pot_diameter_cm),
			package_length_cm = COALESCE($6, package_length_cm),
			package_width_cm = COALESCE($7, package_width_cm),
			package_height_cm = COALESCE($8, package_height_cm),
			package_weight_grams = COALESCE($9, package_weight_grams),
			wholesale_min_qty = COALESCE($10, wholesale_min_qty),
			override_fields = ARRAY(SELECT DISTINCT unnest(override_fields || $11::text[])),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT id FROM product_variants WHERE product_id = $1 ORDER BY is_active DESC, id LIMIT 1)
	`, id, update.VariantLabel, update.PriceMinor, update.HeightCM, update.PotDiameterCM,
		update.PackageLengthCM, update.PackageWidthCM, update.PackageHeightCM,
		update.PackageWeightGrams, update.WholesaleMinQty, variantFields)
	if err != nil {
		return Product{}, fmt.Errorf("update product variant: %w", err)
	}
	// Остаток правим руками только у товаров, которым СБИС его не приносит:
	// иначе ближайший обмен молча вернёт прежнее число, и правка исчезнет.
	if update.Stock != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO inventory (warehouse_id, variant_id, available_qty, reserved_qty, synced_at)
			SELECT w.id, pv.id, $2, 0, CURRENT_TIMESTAMP
			FROM product_variants pv
			JOIN products p ON p.id = pv.product_id
			CROSS JOIN (SELECT id FROM warehouses WHERE saby_id = 'saby-ryazan-main') w
			WHERE pv.product_id = $1 AND NOT ('stock' = ANY(p.saby_fields))
			ON CONFLICT (warehouse_id, variant_id) DO UPDATE SET
				available_qty = EXCLUDED.available_qty, synced_at = CURRENT_TIMESTAMP
		`, id, *update.Stock); err != nil {
			return Product{}, fmt.Errorf("update product stock: %w", err)
		}
	}
	if err := saveProductAttributes(ctx, tx, id, update.Attributes); err != nil {
		return Product{}, err
	}
	if update.Status != nil && *update.Status == "published" || update.CategoryID != nil || update.Attributes != nil {
		var status string
		if err := tx.QueryRow(ctx,"SELECT status FROM products WHERE id=$1",id).Scan(&status); err != nil { return Product{},err }
		if status == "published" { if err := validateRequiredAttributes(ctx,tx,id); err != nil { return Product{},err } }
	}
	if update.ExternalIDs != nil { if err := replaceEditableExternalIDs(ctx,tx,id,*update.ExternalIDs); err != nil { return Product{},err } }
	if update.Image != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM product_media WHERE product_id = $1`, id); err != nil {
			return Product{}, err
		}
		if strings.TrimSpace(*update.Image) != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO product_media(product_id, object_key, alt_text, is_primary)
				SELECT id, $2, name, 1 FROM products WHERE id = $1
			`, id, strings.TrimSpace(*update.Image)); err != nil {
				return Product{}, err
			}
		}
	}
	after, err := productAuditData(ctx, tx, id)
	if err != nil {
		return Product{}, err
	}
	if err := insertAudit(ctx, tx, actor, "product.update", "product", fmt.Sprint(id), before, after); err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return repository.productByID(ctx, id)
}

// replaceEditableExternalIDs replaces only manager-owned mappings. Ficusin
// and Saby identities are maintained by migrations/import and cannot be
// deleted or forged from the product editor.
func replaceEditableExternalIDs(ctx context.Context, tx pgx.Tx, productID int64, mappings []ExternalID) error {
	if _,err:=tx.Exec(ctx,"DELETE FROM product_external_ids WHERE product_id=$1 AND provider NOT IN ('ficusin','saby')",productID);err!=nil{return fmt.Errorf("clear external mappings: %w",err)}
	for _,mapping:=range mappings{
		provider:=strings.ToLower(strings.TrimSpace(mapping.Provider));kind:=strings.ToLower(strings.TrimSpace(mapping.Type));external:=strings.TrimSpace(mapping.ExternalID)
		if provider=="ficusin"||provider=="saby"{continue}
		if !safeMappingToken(provider)||!safeMappingToken(kind)||external==""||len(external)>240{return fmt.Errorf("%w: неверный внешний идентификатор",ErrInvalidInput)}
		tag,err:=tx.Exec(ctx,`INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id)
			SELECT $1,pv.id,$2,$3,$4 FROM product_variants pv WHERE pv.product_id=$1 ORDER BY pv.is_active DESC,pv.id LIMIT 1
			ON CONFLICT(provider,id_type,external_id) DO NOTHING`,productID,provider,kind,external)
		if err!=nil{return fmt.Errorf("save external mapping: %w",err)}
		if tag.RowsAffected()!=1{return fmt.Errorf("%w: внешний идентификатор уже связан с другим товаром",ErrInvalidInput)}
	}
	return nil
}

func safeMappingToken(value string) bool {
	if value==""||len(value)>40{return false}
	for _,char:=range value{if !(char>='a'&&char<='z'||char>='0'&&char<='9'||char=='_'||char=='-'){return false}}
	return true
}

func (repository *PostgresRepository) CatalogMediaHealth(ctx context.Context) (MediaHealth,error) {
	var result MediaHealth
	err:=repository.pool.QueryRow(ctx,`
		SELECT
			(SELECT COUNT(*) FROM product_media)::int,
			(SELECT COUNT(*) FROM product_media WHERE object_key ~ '^https?://')::int,
			(SELECT COUNT(*) FROM product_media pm JOIN media_mirror mm ON mm.source_url=pm.object_key WHERE mm.card_url IS NOT NULL AND mm.large_url IS NOT NULL)::int,
			(SELECT COUNT(*) FROM product_media pm LEFT JOIN media_mirror mm ON mm.source_url=pm.object_key WHERE pm.object_key ~ '^https?://' AND (mm.source_url IS NULL OR mm.card_url IS NULL OR mm.large_url IS NULL) AND COALESCE(mm.attempts,0)<5)::int,
			(SELECT COUNT(*) FROM product_media pm JOIN media_mirror mm ON mm.source_url=pm.object_key WHERE pm.object_key ~ '^https?://' AND (mm.card_url IS NULL OR mm.large_url IS NULL) AND mm.attempts>=5)::int,
			(SELECT COUNT(*) FROM products p WHERE NOT EXISTS(SELECT 1 FROM product_media pm WHERE pm.product_id=p.id))::int,
			(SELECT COUNT(*) FROM media_mirror mm WHERE NOT EXISTS(SELECT 1 FROM product_media pm WHERE pm.object_key=mm.source_url))::int
	`).Scan(&result.References,&result.External,&result.Mirrored,&result.Pending,&result.Exhausted,&result.ProductsWithoutMedia,&result.OrphanMappings)
	if err!=nil{return MediaHealth{},fmt.Errorf("catalog media health: %w",err)}
	return result,nil
}

func (repository *PostgresRepository) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT c.id, c.parent_id, c.name, c.slug, c.sort_order, c.icon,
			(SELECT COUNT(*) FROM products p WHERE p.category_id=c.id)::int,
			(SELECT COUNT(*) FROM categories ch WHERE ch.parent_id=c.id)::int
		FROM categories c WHERE c.active=1 ORDER BY c.sort_order,c.name
	`)
	if err != nil { return nil, fmt.Errorf("query admin categories: %w", err) }
	defer rows.Close()
	result:=make([]Category,0)
	for rows.Next(){ var c Category; if err:=rows.Scan(&c.ID,&c.ParentID,&c.Name,&c.Slug,&c.SortOrder,&c.Icon,&c.ProductsCount,&c.ChildrenCount); err!=nil{return nil,err}; result=append(result,c)}
	return result,rows.Err()
}

// ListCategoryAttributes returns the effective category schema. Definitions
// on the nearest category override the same code inherited from an ancestor.
func (repository *PostgresRepository) ListCategoryAttributes(ctx context.Context, categoryID int64) ([]CategoryAttribute, error) {
	items, err := repository.EffectiveCategoryAttributes(ctx, categoryID)
	if err != nil { return nil, err }
	result := make([]CategoryAttribute, 0, len(items))
	for _, definition := range items {
		if !definition.Active || definition.Excluded { continue }
		options := make([]string, 0, len(definition.Options))
		for _, option := range definition.Options { if option.Active { options = append(options, option.Code) } }
		result = append(result, CategoryAttribute{
			Code: definition.Code, Name: definition.Name, DataType: definition.DataType,
			Unit: definition.Unit, Options: options, Audience: definition.Audience, Scope: definition.Scope,
			Required: definition.Required, Filterable: definition.Filterable,
			ShowOnPDP: definition.ShowOnPDP, Badge: definition.Badge, SortOrder: definition.SortOrder,
		})
	}
	return result, nil
}

func (repository *PostgresRepository) CreateCategory(ctx context.Context, actor Actor, input CategoryCreate) (Category,error) {
	if actor.Role != RoleOwner{return Category{},ErrForbidden}
	input.Name=strings.TrimSpace(input.Name); input.Slug=strings.TrimSpace(input.Slug)
	var id int64
	err:=repository.pool.QueryRow(ctx,`
		INSERT INTO categories(parent_id,name,slug,sort_order) VALUES($1,$2,$3,$4) RETURNING id
	`,input.ParentID,input.Name,input.Slug,input.SortOrder).Scan(&id)
	if err!=nil{return Category{},fmt.Errorf("create category: %w",err)}
	return repository.categoryByID(ctx,id)
}

func (repository *PostgresRepository) UpdateCategory(ctx context.Context, actor Actor,id int64,input CategoryUpdate)(Category,error){
	if actor.Role != RoleOwner{return Category{},ErrForbidden}
	_,err:=repository.pool.Exec(ctx,`
		UPDATE categories SET name=COALESCE(NULLIF(TRIM($2),''),name),
			slug=COALESCE(NULLIF(TRIM($3),''),slug),sort_order=COALESCE($4,sort_order),updated_at=NOW()
		WHERE id=$1
	`,id,input.Name,input.Slug,input.SortOrder)
	if err!=nil{return Category{},fmt.Errorf("update category: %w",err)}
	return repository.categoryByID(ctx,id)
}

func (repository *PostgresRepository) DeleteCategory(ctx context.Context,actor Actor,id int64)error{
	if actor.Role != RoleOwner{return ErrForbidden}
	var children,products int
	if err:=repository.pool.QueryRow(ctx,`
		SELECT (SELECT COUNT(*) FROM categories WHERE parent_id=$1)::int,
			(SELECT COUNT(*) FROM products WHERE category_id=$1)::int
	`,id).Scan(&children,&products);err!=nil{return err}
	if children>0||products>0{return ErrCategoryNotEmpty}
	command,err:=repository.pool.Exec(ctx,"DELETE FROM categories WHERE id=$1",id)
	if err!=nil{return fmt.Errorf("delete category: %w",err)}
	if command.RowsAffected()==0{return pgx.ErrNoRows}
	return nil
}

func(repository *PostgresRepository) categoryByID(ctx context.Context,id int64)(Category,error){
	var c Category
	err:=repository.pool.QueryRow(ctx,`
		SELECT c.id,c.parent_id,c.name,c.slug,c.sort_order,
			(SELECT COUNT(*) FROM products WHERE category_id=c.id)::int,
			(SELECT COUNT(*) FROM categories WHERE parent_id=c.id)::int
		FROM categories c WHERE c.id=$1
	`,id).Scan(&c.ID,&c.ParentID,&c.Name,&c.Slug,&c.SortOrder,&c.ProductsCount,&c.ChildrenCount)
	return c,err
}

// SyncProducts подтягивает выбранные поля из справочника СБИС.
//
// Это разовое действие по кнопке, а не подписка: подтянув описание один раз,
// товар не начинает получать его при каждом обмене. Постоянную связь задаёт
// список разрешённых полей у самого товара.
func (repository *PostgresRepository) SyncProducts(
	ctx context.Context,
	actor Actor,
	request SyncRequest,
) (SyncResult, error) {
	if !Can(actor.Role, PermissionProductsSync) {
		return SyncResult{}, ErrForbidden
	}
	result := SyncResult{Skipped: []int64{}}
	for _, id := range request.ProductIDs {
		var name, description string
		var priceMinor int64
		var images []string
		err := repository.pool.QueryRow(ctx, `
			SELECT source.name, source.description, source.price_minor, source.images
			FROM products p
			JOIN saby_nomenclature source ON source.saby_id = p.saby_id
			WHERE p.id = $1
		`, id).Scan(&name, &description, &priceMinor, &images)
		if err != nil {
			// Товар не связан с СБИС или в справочнике его нет — пропускаем,
			// а не роняем весь список.
			result.Skipped = append(result.Skipped, id)
			continue
		}

		update := ProductUpdate{}
		applied := 0
		wantPhoto := false
		for _, field := range request.Fields {
			switch field {
			case "name":
				if name != "" {
					update.Name = &name
					applied++
				}
			case "description":
				if description != "" {
					update.Description = &description
					applied++
				}
			case "price":
				if priceMinor > 0 {
					update.PriceMinor = &priceMinor
					applied++
				}
			case "photo":
				if len(images) > 0 {
					wantPhoto = true
					applied++
				}
			}
		}
		if applied == 0 {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if _, err := repository.UpdateProduct(ctx, actor, id, update); err != nil {
			return result, err
		}
		if wantPhoto {
			if err := replacePhotos(ctx, repository, id, name, images); err != nil {
				return result, err
			}
		}
		result.Updated++
	}
	return result, nil
}

// replacePhotos ставит карточке весь набор снимков из СБИС, а не первый:
// у растения обычно несколько ракурсов, и терять их при подтягивании обидно.
func replacePhotos(ctx context.Context, repository *PostgresRepository, id int64, name string, images []string) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "DELETE FROM product_media WHERE product_id = $1", id); err != nil {
		return fmt.Errorf("replace product media: %w", err)
	}
	for index, image := range images {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_media (product_id, object_key, alt_text, sort_order, is_primary)
			VALUES ($1, $2, $3, $4, $5)
		`, id, image, name, index, boolToSmallInt(index == 0)); err != nil {
			return fmt.Errorf("insert product media: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (repository *PostgresRepository) productByID(ctx context.Context, id int64) (Product, error) {
	products, err := repository.ListProducts(ctx)
	if err != nil {
		return Product{}, err
	}
	for _, product := range products {
		if product.ID == id {
			return product, nil
		}
	}
	return Product{}, pgx.ErrNoRows
}

func changedProductFields(update ProductUpdate) []string {
	result := []string{}
	if update.Name != nil {
		result = append(result, "name")
	}
	if update.Description != nil {
		result = append(result, "description")
	}
	if update.Image != nil {
		result = append(result, "photo")
	}
	if update.LatinName != nil {
		result = append(result, "latinName")
	}
	if update.ShortDescription != nil {
		result = append(result, "shortDescription")
	}
	if update.CareInstructions != nil {
		result = append(result, "careInstructions")
	}
	return result
}

func changedVariantFields(update ProductUpdate) []string {
	result := []string{}
	if update.PriceMinor != nil {
		result = append(result, "price")
	}
	if update.HeightCM != nil || update.PotDiameterCM != nil || update.PackageLengthCM != nil ||
		update.PackageWidthCM != nil || update.PackageHeightCM != nil || update.PackageWeightGrams != nil {
		result = append(result, "dimensions")
	}
	return result
}

func insertAudit(ctx context.Context, executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, actor Actor, action, entityType, entityID string, before, after any) error {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, err := executor.Exec(ctx, `
		INSERT INTO admin_audit_log(
			actor_customer_id, actor_role, action, entity_type, entity_id, before_data, after_data
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, actor.CustomerID, actor.Role, action, entityType, entityID, beforeJSON, afterJSON)
	if err != nil {
		return fmt.Errorf("write admin audit: %w", err)
	}
	return nil
}

func productAuditData(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (map[string]any, error) {
	var value map[string]any
	err := query.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'name', p.name, 'description', p.description, 'status', p.status,
			'featured', p.is_featured, 'catalogSection', p.catalog_section,
			'plantKind', p.plant_kind, 'lightLevel', p.light_level,
			'watering', p.watering, 'heightClass', p.height_class,
			'careLevel', p.care_level, 'placement', p.placement,
			'petSafety', p.pet_safety, 'growthHabit', p.growth_habit,
			'priceMinor', pv.base_price_minor,
			'variantLabel', pv.label, 'wholesaleMinQty', pv.wholesale_min_qty
		) FROM products p LEFT JOIN LATERAL (
			SELECT * FROM product_variants WHERE product_id = p.id ORDER BY is_active DESC, id LIMIT 1
		) pv ON TRUE WHERE p.id = $1
	`, id).Scan(&value)
	return value, err
}

// SetDeliveryFee finishes an order the shop could not price itself: no box
// dimensions, CDEK unavailable, or a customer asking whether the plants fit
// into one box. Once the manager names the price the order becomes payable
// like any other, and the customer is told.
func (repository *PostgresRepository) SetDeliveryFee(
	ctx context.Context,
	actor Actor,
	id int64,
	fee float64,
) (Order, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return Order{}, ErrForbidden
	}
	if fee < 0 || fee > 100000 {
		return Order{}, fmt.Errorf("стоимость доставки вне разумных пределов")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin delivery fee update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object('deliveryFee', delivery_fee, 'pending', delivery_fee_pending)
		FROM orders WHERE id = $1 FOR UPDATE
	`, id).Scan(&before); err != nil {
		return Order{}, err
	}
	// The total is rebuilt from the goods rather than adjusted, so setting
	// the fee twice cannot stack two deliveries onto one order.
	if _, err := tx.Exec(ctx, `
		UPDATE orders
		SET delivery_fee = $2,
			delivery_fee_pending = 0,
			total = subtotal + $2
		WHERE id = $1
	`, id, fee); err != nil {
		return Order{}, fmt.Errorf("update delivery fee: %w", err)
	}
	var after map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object('deliveryFee', delivery_fee, 'pending', delivery_fee_pending)
		FROM orders WHERE id = $1
	`, id).Scan(&after); err != nil {
		return Order{}, err
	}
	if err := insertAudit(
		ctx, tx, actor, "order.delivery_fee.set", "order", fmt.Sprint(id), before, after,
	); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit delivery fee: %w", err)
	}
	if repository.notifier != nil {
		var customerID *int64
		var orderNumber string
		var paymentStatus string
		if err := repository.pool.QueryRow(ctx,
			"SELECT customer_id, order_number, payment_status FROM orders WHERE id = $1", id,
		).Scan(&customerID, &orderNumber, &paymentStatus); err == nil && customerID != nil {
			// Only someone who still owes money needs to hear about it.
			if paymentStatus == "pending" {
				_ = repository.notifier.NotifyOrderStatus(
					ctx, *customerID, orderNumber, "delivery_priced",
				)
			}
		}
	}
	orders, err := repository.ListOrders(ctx)
	if err != nil {
		return Order{}, err
	}
	for _, order := range orders {
		if order.ID == id {
			return order, nil
		}
	}
	return Order{}, pgx.ErrNoRows
}

// queueStatusLetter writes the letter about a status change into the outbox.
// Statuses the customer does not need to hear about produce nothing.
func (repository *PostgresRepository) queueStatusLetter(
	ctx context.Context,
	orderID int64,
	status string,
) error {
	var letter mail.OrderLetter
	var email string
	if err := repository.pool.QueryRow(ctx, `
		SELECT order_number, customer_name, COALESCE(email, ''), delivery_method,
			address, COALESCE(cdek_track_number, '')
		FROM orders WHERE id = $1
	`, orderID).Scan(
		&letter.Number, &letter.CustomerName, &email,
		&letter.Delivery, &letter.Address, &letter.TrackNumber,
	); err != nil {
		return err
	}
	if email == "" {
		return nil
	}
	message, worth := mail.StatusChange(letter, status)
	if !worth {
		return nil
	}
	_, err := repository.pool.Exec(ctx, `
		INSERT INTO outbox (recipient, subject, body) VALUES ($1, $2, $3)
	`, email, message.Subject, message.Body)
	return err
}

// AdminCollection is a hand-made set as the panel edits it.
type AdminCollection struct {
	ID       int64   `json:"id"`
	Slug     string  `json:"slug"`
	Title    string  `json:"title"`
	Note     string  `json:"note"`
	Active   bool    `json:"active"`
	Products []int64 `json:"products"`
}

func (repository *PostgresRepository) ListAdminCollections(
	ctx context.Context,
) ([]AdminCollection, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT c.id, c.slug, c.title, c.note, c.is_active = 1,
			COALESCE((
				SELECT ARRAY_AGG(cp.product_id ORDER BY cp.sort_order, cp.product_id)
				FROM collection_products cp WHERE cp.collection_id = c.id
			), ARRAY[]::BIGINT[])
		FROM collections c
		ORDER BY c.sort_order, c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query collections: %w", err)
	}
	defer rows.Close()
	result := make([]AdminCollection, 0)
	for rows.Next() {
		var item AdminCollection
		if err := rows.Scan(
			&item.ID, &item.Slug, &item.Title, &item.Note, &item.Active, &item.Products,
		); err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// SetCollectionProducts replaces the contents of a collection outright. The
// panel always sends the whole list, so a product removed there disappears
// here — merging would leave ghosts nobody could delete.
func (repository *PostgresRepository) SetCollectionProducts(
	ctx context.Context,
	actor Actor,
	collectionID int64,
	productIDs []int64,
) error {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		"DELETE FROM collection_products WHERE collection_id = $1", collectionID,
	); err != nil {
		return fmt.Errorf("clear collection: %w", err)
	}
	for index, productID := range productIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO collection_products (collection_id, product_id, sort_order)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, collectionID, productID, index); err != nil {
			return fmt.Errorf("fill collection: %w", err)
		}
	}
	if err := insertAudit(
		ctx, tx, actor, "collection.products.set", "collection",
		fmt.Sprint(collectionID), nil, map[string]any{"count": len(productIDs)},
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
