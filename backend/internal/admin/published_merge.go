package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PublishedMergeRequest is deliberately separate from the draft merger. The
// caller must review the exact botanical identity and every SKU before any
// published card can be absorbed. Matching names are not evidence by itself.
type PublishedMergeRequest struct {
	TargetProductID  int64    `json:"targetProductId"`
	SourceProductIDs []int64  `json:"sourceProductIds"`
	ExpectedSKUs     []string `json:"expectedSkus"`
	ReviewedIdentity string   `json:"reviewedIdentity"`
}

type PublishedMergePreview struct {
	Ready              bool     `json:"ready"`
	TargetProductCode  string   `json:"targetProductCode,omitempty"`
	SourceProductCodes []string `json:"sourceProductCodes,omitempty"`
	SKUs               []string `json:"skus"`
	Blockers           []string `json:"blockers"`
}

// PreviewPublishedMerge uses the same validation as the write operation in a
// database-enforced read-only transaction. A preview can become stale after
// it returns; MergePublishedProducts therefore repeats every check under locks.
func (repository *PostgresRepository) PreviewPublishedMerge(ctx context.Context, actor Actor, request PublishedMergeRequest) (PublishedMergePreview, error) {
	if actor.Role != RoleOwner {
		return PublishedMergePreview{}, ErrForbidden
	}
	ids, err := validatePublishedMergeRequest(request)
	if err != nil {
		return PublishedMergePreview{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PublishedMergePreview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	preview := PublishedMergePreview{SKUs: request.ExpectedSKUs, Blockers: []string{}}
	products, err := loadMergeProducts(ctx, tx, ids, false)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			preview.Blockers = append(preview.Blockers, err.Error())
			return preview, nil
		}
		return preview, err
	}
	preview.TargetProductCode = products[request.TargetProductID].Code
	preview.SourceProductCodes = sourceCodes(products, request.SourceProductIDs)
	for _, check := range []func(context.Context, pgx.Tx) error{
		func(ctx context.Context, tx pgx.Tx) error { return checkMergeProducts(ctx, tx, products, request) },
		checkKnownProductReferences,
	} {
		if err := check(ctx, tx); err != nil {
			if errors.Is(err, ErrInvalidInput) {
				preview.Blockers = append(preview.Blockers, err.Error())
				continue
			}
			return preview, err
		}
	}
	preview.Ready = len(preview.Blockers) == 0
	return preview, nil
}

type mergeProduct struct {
	ID             int64
	Code           string
	Slug           string
	Name           string
	Latin          string
	Status         string
	Section        string
	CategoryID     *int64
	SabyID         string
	Snapshot       json.RawMessage
	VariantIDs     []int64
	VariantSKUs    []string
	VariantLabels  []string
	VariantSabyIDs []string
}

// MergePublishedProducts preserves immutable SKU/variant identity and order
// snapshots while replacing several published PRODUCT identities with one.
// All checks and writes run in one serializable transaction. Any uncertainty
// rejects the entire merge; no source is deleted on a partial result.
func (repository *PostgresRepository) MergePublishedProducts(ctx context.Context, actor Actor, request PublishedMergeRequest) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	ids, err := validatePublishedMergeRequest(request)
	if err != nil {
		return err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock in one order, including the target, to avoid competing merges and
	// source catalogue updates passing validation midway through this action.
	products, err := loadMergeProducts(ctx, tx, ids, true)
	if err != nil {
		return err
	}
	if err = checkMergeProducts(ctx, tx, products, request); err != nil {
		return err
	}
	if err = checkKnownProductReferences(ctx, tx); err != nil {
		return err
	}
	target := products[request.TargetProductID]
	targetSnapshot, err := mergeAuditSnapshot(ctx, tx, target.ID)
	if err != nil {
		return err
	}
	before := map[string]any{"target": targetSnapshot, "sources": map[string]any{}, "reviewedIdentity": strings.TrimSpace(request.ReviewedIdentity)}
	for _, sourceID := range request.SourceProductIDs {
		snapshot, snapshotErr := mergeAuditSnapshot(ctx, tx, sourceID)
		if snapshotErr != nil {
			return snapshotErr
		}
		before["sources"].(map[string]any)[fmt.Sprint(sourceID)] = snapshot
	}

	for _, sourceID := range request.SourceProductIDs {
		source := products[sourceID]
		// Carry complementary editorial content forward. Conflicting nonempty
		// descriptions and care advice are rejected during preflight below.
		if _, err = tx.Exec(ctx, `UPDATE products target SET
			short_description=COALESCE(NULLIF(target.short_description,''),source.short_description),
			description=COALESCE(NULLIF(target.description,''),source.description),
			care_instructions=COALESCE(NULLIF(target.care_instructions,''),source.care_instructions),
			plant_kind=COALESCE(NULLIF(target.plant_kind,''),source.plant_kind),
			light_level=COALESCE(NULLIF(target.light_level,''),source.light_level),
			watering=COALESCE(NULLIF(target.watering,''),source.watering),
			height_class=COALESCE(NULLIF(target.height_class,''),source.height_class),
			care_level=COALESCE(NULLIF(target.care_level,''),source.care_level),
			placement=COALESCE(NULLIF(target.placement,''),source.placement),
			pet_safety=COALESCE(NULLIF(target.pet_safety,''),source.pet_safety),
			growth_habit=COALESCE(NULLIF(target.growth_habit,''),source.growth_habit),
			plant_passport=CASE WHEN target.plant_passport='{}'::jsonb THEN source.plant_passport ELSE target.plant_passport END,
			important_warnings=CASE WHEN cardinality(target.important_warnings)=0 THEN source.important_warnings ELSE target.important_warnings END
			FROM products source WHERE target.id=$1 AND source.id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve editorial content: %w", err)
		}
		// Stock sync is gated at PRODUCT level even though the physical Saby
		// identity belongs to each variant. Keep the gate for moved SKUs.
		if _, err = tx.Exec(ctx, `UPDATE products target SET saby_fields=CASE
			WHEN 'stock'=ANY(target.saby_fields) THEN target.saby_fields
			ELSE array_append(target.saby_fields,'stock') END
			WHERE target.id=$1 AND EXISTS(SELECT 1 FROM products source
				WHERE source.id=$2 AND 'stock'=ANY(source.saby_fields))`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve Saby stock sync: %w", err)
		}
		// The old public code and all former slugs must redirect to the
		// surviving PRODUCT before the source row is removed.
		for _, alias := range []string{source.Code, source.Slug} {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if alias == "" {
				continue
			}
			var ownerID int64
			err = tx.QueryRow(ctx, `SELECT product_id FROM product_url_aliases WHERE alias=$1 FOR UPDATE`, alias).Scan(&ownerID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("check old URL %q: %w", alias, err)
			}
			if err == nil && ownerID != sourceID && ownerID != target.ID {
				return fmt.Errorf("%w: старый адрес %q уже принадлежит другому товару", ErrInvalidInput, alias)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO product_url_aliases(alias,product_id) VALUES($1,$2)
				ON CONFLICT(alias) DO UPDATE SET product_id=EXCLUDED.product_id`, alias, target.ID); err != nil {
				return fmt.Errorf("preserve old URL: %w", err)
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE product_url_aliases SET product_id=$1 WHERE product_id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("move existing URL aliases: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO collection_products(collection_id,product_id,sort_order)
			SELECT collection_id,$1,sort_order FROM collection_products WHERE product_id=$2
			ON CONFLICT(collection_id,product_id) DO NOTHING`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve collections: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id)
			SELECT item_id,$1 FROM avito_listing_products WHERE product_id=$2
			ON CONFLICT(item_id,product_id) DO NOTHING`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve Avito links: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
			SELECT $1,attribute_id,value,source,CURRENT_TIMESTAMP FROM product_attribute_values WHERE product_id=$2
			ON CONFLICT(product_id,attribute_id) DO NOTHING`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve product attributes: %w", err)
		}
		// Keep historic order prices, names, SKU, variant and review IDs. Only
		// their PRODUCT FK changes, so deleting the source cannot orphan them.
		if _, err = tx.Exec(ctx, `UPDATE order_items SET product_id=$1 WHERE product_id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve order history: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE product_reviews SET product_id=$1 WHERE product_id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("preserve reviews: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE product_variants SET product_id=$1,updated_at=CURRENT_TIMESTAMP WHERE product_id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("move SKU variants: %w", err)
		}
		// A product-level primary ID from each source cannot remain product-level:
		// one merged card may have only one primary per provider/type. A source
		// with one SKU has an unambiguous owner, so scope its IDs to that SKU.
		// Change both FKs in this statement: the immediate constraint trigger
		// checks that variant_id belongs to product_id after every UPDATE.
		if len(source.VariantIDs) == 1 {
			if _, err = tx.Exec(ctx, `UPDATE product_external_ids external SET
				is_primary=CASE WHEN EXISTS(SELECT 1 FROM product_external_ids current
					WHERE current.variant_id=$1 AND current.provider=external.provider
					AND current.id_type=external.id_type AND current.status='active' AND current.is_primary)
					THEN FALSE ELSE external.is_primary END,
				variant_id=$1,product_id=$3,updated_at=CURRENT_TIMESTAMP
				WHERE external.product_id=$2 AND external.variant_id IS NULL`, source.VariantIDs[0], sourceID, target.ID); err != nil {
				return fmt.Errorf("scope external IDs to SKU: %w", err)
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE product_external_ids SET product_id=$1,updated_at=CURRENT_TIMESTAMP WHERE product_id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("move external IDs: %w", err)
		}
		if len(source.VariantIDs) == 1 {
			if _, err = tx.Exec(ctx, `UPDATE product_media SET variant_id=$1 WHERE product_id=$2 AND variant_id IS NULL`, source.VariantIDs[0], sourceID); err != nil {
				return fmt.Errorf("scope source photos to SKU: %w", err)
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE product_media SET product_id=$1 WHERE product_id=$2`, target.ID, sourceID); err != nil {
			return fmt.Errorf("move product media: %w", err)
		}
		// The job is transient, but its state is retained in the audit
		// snapshot. A worker must not enrich a card being removed.
		if _, err = tx.Exec(ctx, `DELETE FROM catalog_ai_enrichment_jobs WHERE product_id=$1`, sourceID); err != nil {
			return fmt.Errorf("stop source enrichment: %w", err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM products WHERE id=$1`, sourceID); err != nil {
			return fmt.Errorf("remove merged card: %w", err)
		}
	}
	canonicalName := stripVariantSize(target.Name)
	if _, err = tx.Exec(ctx, `UPDATE products SET name=$2,
		saby_fields=array_remove(array_remove(array_remove(saby_fields,'name'),'description'),'photo'),
		updated_at=CURRENT_TIMESTAMP WHERE id=$1`, target.ID, canonicalName); err != nil {
		return fmt.Errorf("normalize merged title: %w", err)
	}
	if err = insertAudit(ctx, tx, actor, "product.published.merge", "product", fmt.Sprint(target.ID), before,
		map[string]any{"targetProductId": target.ID, "sourceProductIds": request.SourceProductIDs,
			"targetProductCode": target.Code, "sourceProductCodes": sourceCodes(products, request.SourceProductIDs),
			"skus": request.ExpectedSKUs, "reviewedIdentity": strings.TrimSpace(request.ReviewedIdentity),
			"canonicalName": canonicalName}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func mergeAuditSnapshot(ctx context.Context, tx pgx.Tx, productID int64) (json.RawMessage, error) {
	var snapshot json.RawMessage
	err := tx.QueryRow(ctx, `SELECT jsonb_build_object(
		'product',to_jsonb(product),
		'variants',COALESCE((SELECT jsonb_agg(to_jsonb(variant) ORDER BY variant.id) FROM product_variants variant WHERE variant.product_id=product.id),'[]'::jsonb),
		'externalIds',COALESCE((SELECT jsonb_agg(to_jsonb(external) ORDER BY external.id) FROM product_external_ids external WHERE external.product_id=product.id),'[]'::jsonb),
		'attributes',COALESCE((SELECT jsonb_agg(to_jsonb(attribute) ORDER BY attribute.attribute_id) FROM product_attribute_values attribute WHERE attribute.product_id=product.id),'[]'::jsonb),
		'media',COALESCE((SELECT jsonb_agg(to_jsonb(media) ORDER BY media.id) FROM product_media media WHERE media.product_id=product.id),'[]'::jsonb),
		'urlAliases',COALESCE((SELECT jsonb_agg(alias ORDER BY alias) FROM product_url_aliases WHERE product_id=product.id),'[]'::jsonb),
		'collectionIds',COALESCE((SELECT jsonb_agg(collection_id ORDER BY collection_id) FROM collection_products WHERE product_id=product.id),'[]'::jsonb),
		'orderItemIds',COALESCE((SELECT jsonb_agg(id ORDER BY id) FROM order_items WHERE product_id=product.id),'[]'::jsonb),
		'reviewIds',COALESCE((SELECT jsonb_agg(id ORDER BY id) FROM product_reviews WHERE product_id=product.id),'[]'::jsonb),
		'avitoListingIds',COALESCE((SELECT jsonb_agg(item_id ORDER BY item_id) FROM avito_listing_products WHERE product_id=product.id),'[]'::jsonb),
		'aiJob',(SELECT to_jsonb(job) FROM catalog_ai_enrichment_jobs job WHERE job.product_id=product.id)
	) FROM products product WHERE product.id=$1`, productID).Scan(&snapshot)
	if err != nil {
		return nil, fmt.Errorf("snapshot merged card %d: %w", productID, err)
	}
	return snapshot, nil
}

func validatePublishedMergeRequest(request PublishedMergeRequest) ([]int64, error) {
	if request.TargetProductID <= 0 || len(request.SourceProductIDs) == 0 || len(request.SourceProductIDs) > 20 ||
		len(request.ExpectedSKUs) < 2 || len(request.ExpectedSKUs) > 100 ||
		len([]rune(strings.TrimSpace(request.ReviewedIdentity))) < 8 {
		return nil, fmt.Errorf("%w: укажите основную карточку, источники, все SKU и проверенное название вида/сорта", ErrInvalidInput)
	}
	seen := map[int64]bool{request.TargetProductID: true}
	ids := []int64{request.TargetProductID}
	for _, id := range request.SourceProductIDs {
		if id <= 0 || seen[id] {
			return nil, fmt.Errorf("%w: неверный или повторный источник", ErrInvalidInput)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func sourceCodes(products map[int64]*mergeProduct, sourceIDs []int64) []string {
	codes := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		codes = append(codes, products[id].Code)
	}
	return codes
}

func loadMergeProducts(ctx context.Context, tx pgx.Tx, ids []int64, lock bool) (map[int64]*mergeProduct, error) {
	productQuery := `SELECT id,product_code::TEXT,slug,name,latin_name,status,catalog_section,category_id,
		COALESCE(saby_id,''),to_jsonb(products)
		FROM products WHERE id=ANY($1) ORDER BY id`
	variantQuery := `SELECT product_id,id,sku,COALESCE(saby_id,''),label
		FROM product_variants WHERE product_id=ANY($1) ORDER BY product_id,id`
	if lock {
		productQuery += ` FOR UPDATE`
		variantQuery += ` FOR UPDATE`
	}
	rows, err := tx.Query(ctx, productQuery, ids)
	if err != nil {
		return nil, fmt.Errorf("load merge cards: %w", err)
	}
	products := make(map[int64]*mergeProduct, len(ids))
	for rows.Next() {
		item := &mergeProduct{}
		if err = rows.Scan(&item.ID, &item.Code, &item.Slug, &item.Name, &item.Latin,
			&item.Status, &item.Section, &item.CategoryID, &item.SabyID, &item.Snapshot); err != nil {
			rows.Close()
			return nil, err
		}
		products[item.ID] = item
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(products) != len(ids) {
		return nil, fmt.Errorf("%w: одна из карточек не найдена", ErrInvalidInput)
	}
	rows, err = tx.Query(ctx, variantQuery, ids)
	if err != nil {
		return nil, fmt.Errorf("load merge variants: %w", err)
	}
	for rows.Next() {
		var productID, variantID int64
		var sku, sabyID, label string
		if err = rows.Scan(&productID, &variantID, &sku, &sabyID, &label); err != nil {
			rows.Close()
			return nil, err
		}
		item := products[productID]
		item.VariantIDs = append(item.VariantIDs, variantID)
		item.VariantSKUs = append(item.VariantSKUs, sku)
		item.VariantLabels = append(item.VariantLabels, label)
		item.VariantSabyIDs = append(item.VariantSabyIDs, sabyID)
		if strings.TrimSpace(label) == "" || strings.EqualFold(strings.TrimSpace(label), "Основной вариант") {
			rows.Close()
			return nil, fmt.Errorf("%w: у SKU %s нет отличимого размера", ErrInvalidInput, sku)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return products, nil
}

func checkMergeProducts(ctx context.Context, tx pgx.Tx, products map[int64]*mergeProduct, request PublishedMergeRequest) error {
	target := products[request.TargetProductID]
	if target.Status != "published" || target.Section != "plants" || target.CategoryID == nil {
		return fmt.Errorf("%w: основная карточка должна быть опубликованным растением с категорией", ErrInvalidInput)
	}
	identity := normalizeMergeName(target.Name)
	if normalizeMergeName(request.ReviewedIdentity) != identity {
		return fmt.Errorf("%w: подтверждённый вид/сорт не совпадает с названием основной карточки", ErrInvalidInput)
	}
	if !completeMergeLatinIdentity(target.Latin) {
		return fmt.Errorf("%w: нужен подтверждённый вид или именованный сорт в латинском названии, одного рода недостаточно", ErrInvalidInput)
	}
	allSKUs := make([]string, 0)
	labels := make(map[string]bool)
	for _, item := range products {
		if item.Status != "published" || item.Section != "plants" || item.CategoryID == nil || *item.CategoryID != *target.CategoryID {
			return fmt.Errorf("%w: карточка %s не опубликована или находится в другой категории", ErrInvalidInput, item.Code)
		}
		if normalizeMergeName(item.Name) != identity {
			return fmt.Errorf("%w: названия карточек %s и %s различаются; сначала проверьте вид/сорт и приведите названия к одному виду", ErrInvalidInput, target.Code, item.Code)
		}
		if strings.TrimSpace(item.Latin) == "" || strings.TrimSpace(target.Latin) == "" ||
			!strings.EqualFold(strings.TrimSpace(item.Latin), strings.TrimSpace(target.Latin)) {
			return fmt.Errorf("%w: нужен одинаковый подтверждённый латинский вид/сорт для %s и %s", ErrInvalidInput, target.Code, item.Code)
		}
		if len(item.VariantIDs) == 0 {
			return fmt.Errorf("%w: у карточки %s нет вариантов", ErrInvalidInput, item.Code)
		}
		for index, sabyID := range item.VariantSabyIDs {
			if strings.TrimSpace(sabyID) == "" {
				return fmt.Errorf("%w: SKU %s карточки %s не связан со СБИС", ErrInvalidInput, item.VariantSKUs[index], item.Code)
			}
		}
		allSKUs = append(allSKUs, item.VariantSKUs...)
		for _, label := range item.VariantLabels {
			key := strings.ToLower(strings.TrimSpace(label))
			if labels[key] {
				return fmt.Errorf("%w: размер %q повторяется в нескольких SKU", ErrInvalidInput, label)
			}
			labels[key] = true
		}
	}
	if !sameUniqueStrings(allSKUs, request.ExpectedSKUs) {
		return fmt.Errorf("%w: состав SKU изменился; повторите проверку карточек", ErrInvalidInput)
	}
	// A source's Saby PRODUCT identity must already be represented by one of
	// its variants. Otherwise deleting the source would break stock import.
	for _, sourceID := range request.SourceProductIDs {
		source := products[sourceID]
		if source.SabyID != "" && !containsString(source.VariantSabyIDs, source.SabyID) {
			return fmt.Errorf("%w: СБИС %s карточки %s не привязан ни к одному SKU", ErrInvalidInput, source.SabyID, source.Code)
		}
		if len(source.VariantIDs) > 1 {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM product_media WHERE product_id=$1 AND variant_id IS NULL`, sourceID).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("%w: фото карточки %s с несколькими SKU не привязаны к размеру", ErrInvalidInput, source.Code)
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM product_external_ids WHERE product_id=$1 AND variant_id IS NULL`, sourceID).Scan(&count); err != nil {
				return err
			}
			if unscopedIDsAmbiguous(len(source.VariantIDs), count) {
				return fmt.Errorf("%w: внешние ID карточки %s с несколькими SKU не привязаны к размеру", ErrInvalidInput, source.Code)
			}
		}
	}
	ids := make([]int64, 0, len(products))
	for id := range products {
		ids = append(ids, id)
	}
	// This launch rollback table is for non-plants only. A stale row on one
	// of these plants would be deleted by cascade, so reject it explicitly.
	var hiddenLaunchPlant bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM launch_hidden_nonplant_products WHERE product_id=ANY($1))`, ids).Scan(&hiddenLaunchPlant); err != nil {
		return err
	}
	if hiddenLaunchPlant {
		return fmt.Errorf("%w: у растения есть запись в таблице отката скрытия непродуктовых товаров", ErrInvalidInput)
	}
	var missingSabyCode bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM product_variants variant
		LEFT JOIN saby_nomenclature nomenclature ON nomenclature.saby_id=variant.saby_id
		WHERE variant.product_id=ANY($1)
		  AND (nomenclature.code IS NULL OR nomenclature.code !~ '^X[0-9]+$')
	)`, ids).Scan(&missingSabyCode); err != nil {
		return err
	}
	if missingSabyCode {
		return fmt.Errorf("%w: у одного или нескольких SKU не подтверждён буквенный X-код СБИС", ErrInvalidInput)
	}
	var conflictingAttributes bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM product_attribute_values WHERE product_id=ANY($1)
		GROUP BY attribute_id HAVING count(DISTINCT value)>1
	)`, ids).Scan(&conflictingAttributes); err != nil {
		return err
	}
	if conflictingAttributes {
		return fmt.Errorf("%w: характеристики карточек различаются; сначала проверьте значения", ErrInvalidInput)
	}
	var conflictingContent bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM products a JOIN products b ON a.id<b.id
		WHERE a.id=ANY($1) AND b.id=ANY($1) AND (
			(a.short_description<>'' AND b.short_description<>'' AND a.short_description<>b.short_description) OR
			(a.description<>'' AND b.description<>'' AND a.description<>b.description) OR
			(a.care_instructions<>'' AND b.care_instructions<>'' AND a.care_instructions<>b.care_instructions) OR
			(NULLIF(a.plant_kind,'') IS NOT NULL AND NULLIF(b.plant_kind,'') IS NOT NULL AND a.plant_kind<>b.plant_kind) OR
			(NULLIF(a.light_level,'') IS NOT NULL AND NULLIF(b.light_level,'') IS NOT NULL AND a.light_level<>b.light_level) OR
			(NULLIF(a.watering,'') IS NOT NULL AND NULLIF(b.watering,'') IS NOT NULL AND a.watering<>b.watering) OR
			(NULLIF(a.height_class,'') IS NOT NULL AND NULLIF(b.height_class,'') IS NOT NULL AND a.height_class<>b.height_class) OR
			(NULLIF(a.care_level,'') IS NOT NULL AND NULLIF(b.care_level,'') IS NOT NULL AND a.care_level<>b.care_level) OR
			(NULLIF(a.placement,'') IS NOT NULL AND NULLIF(b.placement,'') IS NOT NULL AND a.placement<>b.placement) OR
			(NULLIF(a.pet_safety,'') IS NOT NULL AND NULLIF(b.pet_safety,'') IS NOT NULL AND a.pet_safety<>b.pet_safety) OR
			(NULLIF(a.growth_habit,'') IS NOT NULL AND NULLIF(b.growth_habit,'') IS NOT NULL AND a.growth_habit<>b.growth_habit) OR
			(a.plant_passport<>'{}'::jsonb AND b.plant_passport<>'{}'::jsonb AND a.plant_passport<>b.plant_passport) OR
			(cardinality(a.important_warnings)>0 AND cardinality(b.important_warnings)>0 AND a.important_warnings<>b.important_warnings)
		)
	)`, ids).Scan(&conflictingContent); err != nil {
		return err
	}
	if conflictingContent {
		return fmt.Errorf("%w: описания или уход карточек различаются; объедините контент вручную", ErrInvalidInput)
	}
	var conflictingReviews bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM product_reviews WHERE product_id=ANY($1)
		GROUP BY customer_id,order_id HAVING count(*)>1
	)`, ids).Scan(&conflictingReviews); err != nil {
		return err
	}
	if conflictingReviews {
		return fmt.Errorf("%w: один покупатель оставил отзывы на несколько объединяемых карточек", ErrInvalidInput)
	}
	return nil
}

func normalizeMergeName(value string) string {
	value = strings.ToLower(stripVariantSize(value))
	value = strings.NewReplacer("«", "", "»", "", "\"", "", "„", "", "“", "", "”", "", "(", "", ")", "").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

// A genus-only Latin label is not enough to prove two published plants are
// the same taxon. A binomial or an explicitly named cultivar is required;
// final identity still has to be reviewed by the owner before the merge.
func completeMergeLatinIdentity(value string) bool {
	value = strings.TrimSpace(value)
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return false
	}
	remainder := strings.TrimSpace(strings.TrimPrefix(value, parts[0]))
	if strings.HasPrefix(remainder, "'") && strings.HasSuffix(remainder, "'") && len(remainder) > 2 {
		return true
	}
	species := strings.ToLower(strings.Trim(parts[1], "."))
	return species != "sp" && species != "spp" && len(parts[1]) > 1 && parts[1][0] >= 'a' && parts[1][0] <= 'z'
}

var mergePotSize = regexp.MustCompile(`(?i)(?:^|[[:space:]])[dд][[:space:]]*[0-9]+(?:[.,][0-9]+)?`)
var mergeHeight = regexp.MustCompile(`(?i)(?:^|[[:space:]])[0-9]+(?:[.,][0-9]+)?[[:space:]]*см`)

func stripVariantSize(value string) string {
	value = mergePotSize.ReplaceAllString(value, " ")
	value = mergeHeight.ReplaceAllString(value, " ")
	return strings.Trim(strings.Join(strings.Fields(value), " "), " ,.-·")
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func sameUniqueStrings(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(actual))
	for _, value := range actual {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, value := range expected {
		if !seen[value] {
			return false
		}
		delete(seen, value)
	}
	return len(seen) == 0
}

func unscopedIDsAmbiguous(variantCount, unscopedIDCount int) bool {
	return variantCount > 1 && unscopedIDCount > 0
}

// A new direct FK to PRODUCT must be handled before enabling this operation;
// otherwise ON DELETE CASCADE could silently erase its data.
func checkKnownProductReferences(ctx context.Context, tx pgx.Tx) error {
	known := map[string]bool{
		"products": true, "product_variants": true, "product_external_ids": true,
		"product_attribute_values": true, "collection_products": true, "product_media": true,
		"product_reviews": true, "order_items": true, "catalog_ai_enrichment_jobs": true,
		"product_url_aliases": true, "avito_listing_products": true,
		"launch_hidden_nonplant_products": true,
	}
	rows, err := tx.Query(ctx, `SELECT conrelid::regclass::TEXT FROM pg_constraint
		WHERE contype='f' AND confrelid='products'::regclass`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return err
		}
		name = strings.TrimPrefix(name, "public.")
		if !known[name] {
			return fmt.Errorf("%w: связь таблицы %s с товарами не учтена в объединении", ErrInvalidInput, name)
		}
	}
	return rows.Err()
}
