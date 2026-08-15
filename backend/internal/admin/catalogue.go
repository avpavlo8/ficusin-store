package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidInput — данные, с которыми карточку не завести. Отдельная ошибка
// нужна, чтобы панель показала внятную причину, а не «внутренняя ошибка».
var ErrInvalidInput = errors.New("некорректные данные")

// seed — то, из чего рождается карточка: своя или пришедшая из справочника
// СБИС. Оба пути ведут в одну функцию, поэтому товар, заведённый руками, и
// товар, привезённый импортом, устроены одинаково.
type seed struct {
	name           string
	latinName      string
	description    string
	categoryID     *int64
	catalogSection string
	priceMinor     int64
	stock          int
	images         []string
	sabyID         string
	sabyFields     []string
	heightCM       *int
	potDiameterCM  *int
	packageLengthCM *int
	packageWidthCM *int
	packageHeightCM *int
	packageWeightGrams *int
}

// CreateProduct заводит карточку в магазине.
//
// Товар, созданный здесь, с СБИС не связан вовсе: ни цена, ни остаток оттуда
// не придут. Связь появляется только у импортированных товаров, и даже у них
// СБИС по умолчанию распоряжается одним остатком.
func (repository *PostgresRepository) CreateProduct(
	ctx context.Context,
	actor Actor,
	input ProductCreate,
) (Product, error) {
	if !Can(actor.Role, PermissionProductsEdit) {
		return Product{}, ErrForbidden
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Product{}, fmt.Errorf("%w: название товара обязательно", ErrInvalidInput)
	}
	if input.PriceMinor < 0 || input.Stock < 0 {
		return Product{}, fmt.Errorf("%w: цена и остаток не могут быть отрицательными", ErrInvalidInput)
	}
	for label, value := range map[string]*int{
		"высота": input.HeightCM, "диаметр горшка": input.PotDiameterCM,
		"длина упаковки": input.PackageLengthCM, "ширина упаковки": input.PackageWidthCM,
		"высота упаковки": input.PackageHeightCM, "вес упаковки": input.PackageWeightGrams,
	} {
		if value != nil && (*value < 0 || *value > 100000) {
			return Product{}, fmt.Errorf("%w: неверное значение поля «%s»", ErrInvalidInput, label)
		}
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	images := make([]string, 0, 1)
	if picture := strings.TrimSpace(input.Image); picture != "" {
		images = append(images, picture)
	}
	id, err := createProduct(ctx, tx, seed{
		name:           name,
		latinName:      strings.TrimSpace(input.LatinName),
		description:    strings.TrimSpace(input.Description),
		categoryID:     input.CategoryID,
		catalogSection: strings.TrimSpace(input.CatalogSection),
		priceMinor:     input.PriceMinor,
		stock:          input.Stock,
		images:         images,
		sabyFields:     []string{},
		heightCM: input.HeightCM, potDiameterCM: input.PotDiameterCM,
		packageLengthCM: input.PackageLengthCM, packageWidthCM: input.PackageWidthCM,
		packageHeightCM: input.PackageHeightCM, packageWeightGrams: input.PackageWeightGrams,
	})
	if err != nil {
		return Product{}, err
	}
	if err := saveProductAttributes(ctx, tx, id, input.Attributes); err != nil {
		return Product{}, err
	}
	if err := validateRequiredAttributes(ctx, tx, id); err != nil { return Product{}, err }
	after, err := productAuditData(ctx, tx, id)
	if err != nil {
		return Product{}, err
	}
	if err := insertAudit(ctx, tx, actor, "product.create", "product", fmt.Sprint(id), nil, after); err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return repository.productByID(ctx, id)
}

// saveProductAttributes is the single validation path for category-driven
// values. A value must have the declared JSON type, belong to the product's
// effective category schema, and (for enums) use only configured options.
func saveProductAttributes(ctx context.Context, tx pgx.Tx, productID int64, attributes map[string]any) error {
	for code, value := range attributes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode product attribute %s: %w", code, err)
		}
		if string(raw) == "null" || string(raw) == `""` || string(raw) == "[]" {
			if _, err := tx.Exec(ctx, `DELETE FROM product_attribute_values v USING attribute_definitions a
				WHERE v.attribute_id=a.id AND v.product_id=$1 AND a.code=$2`, productID, code); err != nil {
				return fmt.Errorf("clear product attribute %s: %w", code, err)
			}
			if err := clearLegacyAttribute(ctx, tx, productID, code); err != nil {
				return err
			}
			continue
		}
		tag, err := tx.Exec(ctx, `
			WITH RECURSIVE ancestors AS (
				SELECT category_id AS id FROM products WHERE id=$1
				UNION ALL SELECT c.parent_id FROM categories c JOIN ancestors a ON c.id=a.id
				WHERE c.parent_id IS NOT NULL
			)
			INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
			SELECT $1,d.id,$3::jsonb,'local',CURRENT_TIMESTAMP
			FROM attribute_definitions d
			WHERE d.code=$2
			  AND EXISTS (SELECT 1 FROM category_attributes ca JOIN ancestors a ON a.id=ca.category_id
				WHERE ca.attribute_id=d.id)
			  AND CASE d.data_type
				WHEN 'number' THEN jsonb_typeof($3::jsonb)='number'
				WHEN 'boolean' THEN jsonb_typeof($3::jsonb)='boolean'
				WHEN 'enum' THEN jsonb_typeof($3::jsonb)='string' AND d.options ? ($3::jsonb #>> '{}')
				WHEN 'multi_enum' THEN jsonb_typeof($3::jsonb)='array' AND NOT EXISTS (
					SELECT 1 FROM jsonb_array_elements_text($3::jsonb) item WHERE NOT d.options ? item)
				ELSE jsonb_typeof($3::jsonb)='string' END
			ON CONFLICT(product_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,
				source='local',updated_at=CURRENT_TIMESTAMP
		`, productID, code, string(raw))
		if err != nil {
			return fmt.Errorf("save product attribute %s: %w", code, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: атрибут %s не разрешён для категории или имеет неверное значение", ErrInvalidInput, code)
		}
	}
	// Legacy columns remain the delivery/filter read path during the gradual
	// migration. Mirror generic writes back so both models stay consistent.
	if _, err := tx.Exec(ctx, `
		UPDATE products p SET
			light_level=COALESCE((SELECT value #>> '{}' FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=p.id AND d.code='light_level'),p.light_level),
			watering=COALESCE((SELECT value #>> '{}' FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=p.id AND d.code='watering'),p.watering),
			care_level=COALESCE((SELECT value #>> '{}' FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=p.id AND d.code='care_level'),p.care_level),
			pet_safety=COALESCE((SELECT value #>> '{}' FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=p.id AND d.code='pet_safety'),p.pet_safety),
			placement=COALESCE((SELECT value->>0 FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=p.id AND d.code='placement'),p.placement),
			growth_habit=COALESCE((SELECT value #>> '{}' FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=p.id AND d.code='growth_habit'),p.growth_habit)
		WHERE p.id=$1
	`, productID); err != nil {
		return fmt.Errorf("mirror customer attributes: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE product_variants pv SET
			height_cm=COALESCE((SELECT (value #>> '{}')::int FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=$1 AND d.code='height_cm'),pv.height_cm),
			pot_diameter_cm=COALESCE((SELECT (value #>> '{}')::int FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=$1 AND d.code='pot_diameter_cm'),pv.pot_diameter_cm),
			package_length_cm=COALESCE((SELECT (value #>> '{}')::int FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=$1 AND d.code='package_length_cm'),pv.package_length_cm),
			package_width_cm=COALESCE((SELECT (value #>> '{}')::int FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=$1 AND d.code='package_width_cm'),pv.package_width_cm),
			package_height_cm=COALESCE((SELECT (value #>> '{}')::int FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=$1 AND d.code='package_height_cm'),pv.package_height_cm),
			package_weight_grams=COALESCE((SELECT (value #>> '{}')::int FROM product_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_id WHERE v.product_id=$1 AND d.code='package_weight_grams'),pv.package_weight_grams)
		WHERE pv.id=(SELECT id FROM product_variants WHERE product_id=$1 ORDER BY is_active DESC,id LIMIT 1)
	`, productID); err != nil {
		return fmt.Errorf("mirror technical attributes: %w", err)
	}
	return nil
}

// validateRequiredAttributes is intentionally enforced in the application,
// not as a database CHECK: required fields depend on the inherited category
// schema. Existing published products are grandfathered until an editor
// changes their category/attributes or explicitly republishes them.
func validateRequiredAttributes(ctx context.Context, tx pgx.Tx, productID int64) error {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT category_id id,0 depth FROM products WHERE id=$1
			UNION ALL SELECT c.parent_id,a.depth+1 FROM categories c JOIN ancestors a ON c.id=a.id WHERE c.parent_id IS NOT NULL
		), effective AS (
			SELECT DISTINCT ON (d.id) d.id,d.code,d.name,ca.is_required,a.depth
			FROM ancestors a JOIN category_attributes ca ON ca.category_id=a.id
			JOIN attribute_definitions d ON d.id=ca.attribute_id
			ORDER BY d.id,a.depth
		)
		SELECT e.name FROM effective e LEFT JOIN product_attribute_values v
			ON v.attribute_id=e.id AND v.product_id=$1
		WHERE e.is_required AND (v.value IS NULL OR v.value='null'::jsonb OR v.value='""'::jsonb OR v.value='[]'::jsonb)
		ORDER BY e.name
	`, productID)
	if err != nil { return fmt.Errorf("validate required attributes: %w",err) }
	defer rows.Close()
	missing:=[]string{}
	for rows.Next(){var name string;if err:=rows.Scan(&name);err!=nil{return err};missing=append(missing,name)}
	if err:=rows.Err();err!=nil{return err}
	if len(missing)>0{return fmt.Errorf("%w: заполните обязательные характеристики: %s",ErrInvalidInput,strings.Join(missing,", "))}
	return nil
}

func clearLegacyAttribute(ctx context.Context, tx pgx.Tx, productID int64, code string) error {
	productColumns := map[string]string{
		"light_level": "light_level", "watering": "watering", "care_level": "care_level",
		"pet_safety": "pet_safety", "placement": "placement", "growth_habit": "growth_habit",
	}
	variantColumns := map[string]string{
		"height_cm": "height_cm", "pot_diameter_cm": "pot_diameter_cm",
		"package_length_cm": "package_length_cm", "package_width_cm": "package_width_cm",
		"package_height_cm": "package_height_cm", "package_weight_grams": "package_weight_grams",
	}
	if column, ok := productColumns[code]; ok {
		if _, err := tx.Exec(ctx, "UPDATE products SET "+column+"=NULL WHERE id=$1", productID); err != nil {
			return fmt.Errorf("clear legacy product attribute %s: %w", code, err)
		}
	}
	if column, ok := variantColumns[code]; ok {
		if _, err := tx.Exec(ctx, "UPDATE product_variants SET "+column+"=NULL WHERE id=(SELECT id FROM product_variants WHERE product_id=$1 ORDER BY is_active DESC,id LIMIT 1)", productID); err != nil {
			return fmt.Errorf("clear legacy variant attribute %s: %w", code, err)
		}
	}
	return nil
}

// ImportProducts заводит карточки по кодам товаров из справочника СБИС.
//
// Ищем в справочнике, а не в самом СБИС: обмен уже принёс всю номенклатуру,
// поэтому импорт мгновенный и работает, даже когда СБИС недоступен. Обратная
// сторона — товар, заведённый в СБИС пять минут назад, приедет со следующим
// обменом.
func (repository *PostgresRepository) ImportProducts(
	ctx context.Context,
	actor Actor,
	request ImportRequest,
) (ImportResult, error) {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ImportResult{}, ErrForbidden
	}
	codes := normalizeCodes(request.Codes)
	result := ImportResult{Entries: make([]ImportEntry, 0, len(codes))}
	if len(codes) == 0 {
		return result, nil
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Код товара зовётся в выгрузке по-разному, поэтому ищем сразу по всем
	// колонкам: менеджеру не нужно помнить, что именно он скопировал.
	rows, err := tx.Query(ctx, `
		SELECT saby_id, code, article, barcode, name, description, price_minor, balance, images
		FROM saby_nomenclature
		WHERE UPPER(code) = ANY($1::text[]) OR UPPER(article) = ANY($1::text[])
			OR UPPER(barcode) = ANY($1::text[]) OR UPPER(saby_id) = ANY($1::text[])
	`, codes)
	if err != nil {
		return ImportResult{}, fmt.Errorf("search Saby nomenclature: %w", err)
	}
	type found struct {
		sabyID      string
		name        string
		description string
		priceMinor  int64
		balance     int
		images      []string
	}
	byCode := make(map[string]found)
	for rows.Next() {
		var item found
		var code, article, barcode string
		if err := rows.Scan(&item.sabyID, &code, &article, &barcode, &item.name,
			&item.description, &item.priceMinor, &item.balance, &item.images); err != nil {
			rows.Close()
			return ImportResult{}, fmt.Errorf("scan Saby nomenclature: %w", err)
		}
		for _, key := range []string{code, article, barcode, item.sabyID} {
			if key = strings.ToUpper(strings.TrimSpace(key)); key != "" {
				byCode[key] = item
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ImportResult{}, fmt.Errorf("read Saby nomenclature: %w", err)
	}
	rows.Close()

	for _, code := range codes {
		item, ok := byCode[code]
		if !ok {
			result.Entries = append(result.Entries, ImportEntry{Code: code, Status: "missing"})
			continue
		}
		entry := ImportEntry{
			Code:  code,
			Name:  item.name,
			Price: float64(item.priceMinor) / 100,
			Stock: item.balance,
		}
		var existingID int64
		var existingSlug string
		err := tx.QueryRow(ctx,
			"SELECT id, slug FROM products WHERE saby_id = $1", item.sabyID,
		).Scan(&existingID, &existingSlug)
		if err == nil {
			entry.Status = "exists"
			entry.ProductID = &existingID
			entry.Slug = existingSlug
			result.Entries = append(result.Entries, entry)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return ImportResult{}, fmt.Errorf("look up imported product: %w", err)
		}
		entry.Status = "new"
		if request.DryRun {
			result.Entries = append(result.Entries, entry)
			continue
		}
		id, err := createProduct(ctx, tx, seed{
			name:        item.name,
			description: item.description,
			categoryID:  request.CategoryID,
			priceMinor:  item.priceMinor,
			stock:       item.balance,
			images:      item.images,
			sabyID:      item.sabyID,
			// Из СБИС и дальше берём только остаток: название и цену с этой
			// минуты ведёт магазин.
			sabyFields: []string{"stock"},
		})
		if err != nil {
			return ImportResult{}, err
		}
		entry.ProductID = &id
		result.Created++
		result.Entries = append(result.Entries, entry)
	}

	if !request.DryRun && result.Created > 0 {
		if err := insertAudit(ctx, tx, actor, "product.import", "product", "", nil,
			map[string]any{"created": result.Created, "codes": codes}); err != nil {
			return ImportResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func createProduct(ctx context.Context, tx pgx.Tx, item seed) (int64, error) {
	slug, err := freeSlug(ctx, tx, item.name)
	if err != nil {
		return 0, err
	}
	section := item.catalogSection
	if section == "" {
		section = "plants"
	}
	fields := item.sabyFields
	if fields == nil {
		fields = []string{}
	}
	var sabyID *string
	if item.sabyID != "" {
		sabyID = &item.sabyID
	}

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO products (
			saby_id, slug, name, latin_name, short_description, description,
			care_instructions, search_text, status, catalog_section, category_id,
			saby_fields, is_featured, updated_at
		)
		VALUES ($1, $2, $3, $4, '', $5, '', $3, 'published', $6, $7, $8, 0, CURRENT_TIMESTAMP)
		RETURNING id
	`, sabyID, slug, item.name, item.latinName, item.description, section,
		item.categoryID, fields).Scan(&id); err != nil {
		return 0, fmt.Errorf("create product: %w", err)
	}

	var variantID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO product_variants (
			product_id, saby_id, sku, label, base_price_minor, height_cm, pot_diameter_cm,
			package_length_cm, package_width_cm, package_height_cm, package_weight_grams,
			is_active, updated_at
		)
		VALUES ($1, $2, 'FIC-' || LPAD(nextval('ficusin_sku_seq')::TEXT, 6, '0'),
			'Основной вариант', $3, $4, $5, $6, $7, $8, $9, 1, CURRENT_TIMESTAMP)
		RETURNING id
	`, id, sabyID, item.priceMinor, item.heightCM, item.potDiameterCM, item.packageLengthCM,
		item.packageWidthCM, item.packageHeightCM, item.packageWeightGrams).Scan(&variantID); err != nil {
		return 0, fmt.Errorf("create product variant: %w", err)
	}
	if item.sabyID != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_external_ids(product_id, variant_id, provider, id_type, external_id)
			VALUES ($1,$2,'saby','id',$3)
			ON CONFLICT(provider,id_type,external_id) DO UPDATE SET
				product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id,
				updated_at=CURRENT_TIMESTAMP
		`, id, variantID, item.sabyID); err != nil {
			return 0, fmt.Errorf("map Saby product identity: %w", err)
		}
	}

	// Склад заводим на всякий случай: без него товару некуда положить
	// остаток, и карточка окажется вечно «под заказ».
	if _, err := tx.Exec(ctx, `
		INSERT INTO warehouses (saby_id, name, city, address, is_active)
		VALUES ('saby-ryazan-main', 'Основной склад', 'Рязань', 'Новосёлов, 40А', 1)
		ON CONFLICT (saby_id) DO NOTHING
	`); err != nil {
		return 0, fmt.Errorf("ensure warehouse: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO inventory (warehouse_id, variant_id, available_qty, reserved_qty, synced_at)
		SELECT id, $1, $2, 0, CURRENT_TIMESTAMP FROM warehouses WHERE saby_id = 'saby-ryazan-main'
		ON CONFLICT (warehouse_id, variant_id) DO UPDATE SET available_qty = EXCLUDED.available_qty
	`, variantID, item.stock); err != nil {
		return 0, fmt.Errorf("create product stock: %w", err)
	}

	for index, image := range item.images {
		if image = strings.TrimSpace(image); image == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_media (product_id, object_key, alt_text, sort_order, is_primary)
			VALUES ($1, $2, $3, $4, $5)
		`, id, image, item.name, index, boolToSmallInt(index == 0)); err != nil {
			return 0, fmt.Errorf("create product media: %w", err)
		}
	}
	return id, nil
}

// freeSlug подбирает свободный адрес страницы. Занятый адрес — не ошибка
// ввода: два фикуса Бенджамина в магазине — обычное дело.
func freeSlug(ctx context.Context, tx pgx.Tx, name string) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "tovar"
	}
	candidate := base
	for attempt := 2; attempt < 100; attempt++ {
		var taken bool
		if err := tx.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM products WHERE slug = $1)", candidate,
		).Scan(&taken); err != nil {
			return "", fmt.Errorf("check product slug: %w", err)
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, attempt)
	}
	return "", fmt.Errorf("%w: не подобрать адрес страницы для «%s»", ErrInvalidInput, name)
}

// slugify превращает название в адрес страницы.
//
// Кириллицу переводим в латиницу, а не выбрасываем: адрес /product/fikus
// человек прочтёт и перескажет по телефону, а /product/saby-1150532 — нет.
func slugify(name string) string {
	letters := map[rune]string{
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
		'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
		'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
		'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
		'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	}
	var out strings.Builder
	previousDash := true
	for _, symbol := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= '0' && symbol <= '9':
			out.WriteRune(symbol)
			previousDash = false
		case letters[symbol] != "":
			out.WriteString(letters[symbol])
			previousDash = false
		case symbol == 'ъ' || symbol == 'ь':
			// Мягкий и твёрдый знаки просто исчезают, не превращаясь в дефис.
		default:
			if !previousDash {
				out.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

// normalizeCodes разбирает то, что менеджер вставил в поле: список из СБИС
// приходит то через запятую, то каждый код с новой строки, то с лишними
// пробелами. Порядок сохраняем — по нему потом читается отчёт.
func normalizeCodes(raw []string) []string {
	seen := make(map[string]bool)
	codes := make([]string, 0, len(raw))
	for _, chunk := range raw {
		for _, part := range strings.FieldsFunc(chunk, func(symbol rune) bool {
			return symbol == ',' || symbol == ';' || symbol == '\n' ||
				symbol == '\r' || symbol == '\t' || symbol == ' '
		}) {
			code := strings.ToUpper(strings.TrimSpace(part))
			if code == "" || seen[code] {
				continue
			}
			seen[code] = true
			codes = append(codes, code)
		}
	}
	return codes
}

func boolToSmallInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
