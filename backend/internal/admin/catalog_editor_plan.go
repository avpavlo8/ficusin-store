package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5"
	urlpkg "net/url"
	"regexp"
	"strings"
)

var editorPotSuffix = regexp.MustCompile(`(?i)\s+[dд]\s*\d+(?:[.,]\d+)?(?:\s*см)?$`)

func editorSabyName(name string, attributes map[string]any) string {
	name = strings.TrimSpace(name)
	if pot := attributes["pot_diameter_cm"]; pot != nil {
		return strings.TrimSpace(editorPotSuffix.ReplaceAllString(name, "")) + " D" + fmt.Sprint(pot)
	}
	return name
}

type EditorCategory = catalogeditor.Category
type EditorChannel = catalogeditor.Channel
type EditorMarketplace = catalogeditor.Marketplace
type EditorSaby = catalogeditor.Saby
type EditorVariantPlan struct {
	VariantID   int64             `json:"variantId"`
	Saby        EditorSaby        `json:"saby"`
	Marketplace EditorMarketplace `json:"marketplace"`
	OzonPrice   string            `json:"ozonPrice"`
	WBPrice     string            `json:"wbPrice"`
}
type EditorPlan struct {
	Variants []EditorVariantPlan `json:"variants"`
}
type EditorPublication struct {
	ExternalIDs   catalogeditor.RemoteIDs `json:"externalIds"`
	ExecutionMode string                  `json:"executionMode"`
	VariantID     int64                   `json:"variantId"`
	Channel       string                  `json:"channel"`
	Status        string                  `json:"status"`
	Message       string                  `json:"message"`
}
type EditorPlanView struct {
	SabyLinks       map[int64]EditorSabyLink    `json:"sabyLinks"`
	Links           map[int64]map[string]string `json:"links"`
	Config          EditorPlan                  `json:"config"`
	Publications    []EditorPublication         `json:"publications"`
	Barcodes        map[int64]string            `json:"barcodes"`
	PublicationMode string                      `json:"publicationMode"`
}

func (r *PostgresRepository) ReadEditorPlan(ctx context.Context, productID int64) (EditorPlanView, error) {
	view := EditorPlanView{Config: EditorPlan{Variants: []EditorVariantPlan{}}, Publications: []EditorPublication{}, Barcodes: map[int64]string{}, PublicationMode: r.editorPublicationMode, Links: map[int64]map[string]string{}}
	var err error
	view.SabyLinks, err = r.readEditorSabyLinks(ctx, productID)
	if err != nil {
		return view, err
	}
	err = r.pool.QueryRow(ctx, `SELECT config FROM catalog_editor_plans WHERE product_id=$1`, productID).Scan(&view.Config)
	if err != nil && err != pgx.ErrNoRows {
		return view, err
	}
	rows, err := r.pool.Query(ctx, `SELECT p.variant_id,p.channel,p.status,p.message,COALESCE(p.progress->'ids','{}'::jsonb),p.execution_mode FROM catalog_editor_publications p JOIN product_variants v ON v.id=p.variant_id WHERE v.product_id=$1 ORDER BY p.variant_id,p.channel`, productID)
	if err != nil {
		return view, err
	}
	for rows.Next() {
		var p EditorPublication
		if err = rows.Scan(&p.VariantID, &p.Channel, &p.Status, &p.Message, &p.ExternalIDs, &p.ExecutionMode); err != nil {
			rows.Close()
			return view, err
		}
		view.Publications = append(view.Publications, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return view, err
	}
	rows, err = r.pool.Query(ctx, `SELECT id,generated_barcode FROM product_variants WHERE product_id=$1 AND generated_barcode IS NOT NULL`, productID)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var code string
		if err = rows.Scan(&id, &code); err != nil {
			return view, err
		}
		view.Barcodes[id] = code
	}
	if err = rows.Err(); err != nil {
		return view, err
	}
	rows.Close()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return view, err
	}
	defer tx.Rollback(ctx)
	ids, err := tx.Query(ctx, `SELECT id FROM product_variants WHERE product_id=$1`, productID)
	if err != nil {
		return view, err
	}
	variants := []int64{}
	for ids.Next() {
		var id int64
		if err = ids.Scan(&id); err != nil {
			ids.Close()
			return view, err
		}
		variants = append(variants, id)
	}
	err = ids.Err()
	ids.Close()
	if err != nil {
		return view, err
	}
	for _, id := range variants {
		links, e := editorLinkedChannels(ctx, tx, id, r.editorPublicationMode)
		if e != nil {
			return view, e
		}
		view.Links[id] = links
	}
	return view, nil
}
func ValidateEditorPlan(plan EditorPlan, submit bool) error {
	if len(plan.Variants) > 100 {
		return ErrInvalidInput
	}
	seen := map[int64]bool{}
	for _, v := range plan.Variants {
		if v.VariantID <= 0 || seen[v.VariantID] {
			return fmt.Errorf("%w: проверьте варианты товара", ErrInvalidInput)
		}
		seen[v.VariantID] = true
		for _, mode := range []string{v.Saby.Mode, v.Marketplace.Ozon.Mode, v.Marketplace.WB.Mode} {
			if mode != "" && mode != "create" && mode != "link" {
				return ErrInvalidInput
			}
		}
		if v.Marketplace.Units < 0 {
			return ErrInvalidInput
		}
		if !submit {
			continue
		}
		for name, ch := range map[string]EditorChannel{"Ozon": v.Marketplace.Ozon, "Wildberries": v.Marketplace.WB} {
			if !ch.Enabled {
				continue
			}

			if ch.Mode == "link" {
				if strings.TrimSpace(ch.ExistingID) == "" {
					return fmt.Errorf("%w: укажите артикул существующего товара %s", ErrInvalidInput, name)
				}
				continue
			}
			if name == "Ozon" && ch.Category.TypeID <= 0 {
				return fmt.Errorf("%w: выберите тип товара Ozon", ErrInvalidInput)
			}
			if ch.Category.ID == "" {
				return fmt.Errorf("%w: выберите категорию %s", ErrInvalidInput, name)
			}
			m := v.Marketplace
			if name == "Ozon" && (strings.TrimSpace(m.Model) == "" || m.Units < 1) {
				return fmt.Errorf("%w: укажите название модели и количество единиц для Ozon", ErrInvalidInput)
			}

			if m.SellerArticle(map[string]string{"Ozon": "ozon", "Wildberries": "wb"}[name]) == "" || strings.TrimSpace(m.Brand) == "" || strings.TrimSpace(m.TNVED) == "" {
				return fmt.Errorf("%w: заполните артикул, бренд и ТН ВЭД для %s", ErrInvalidInput, name)
			}
		}
	}
	return nil
}
func (r *PostgresRepository) SaveEditorPlan(ctx context.Context, actor Actor, productID int64, plan EditorPlan, submit bool) (EditorPlanView, error) {
	if !Can(actor.Role, PermissionProductsManage) {
		return EditorPlanView{}, ErrForbidden
	}
	if err := ValidateEditorPlan(plan, false); err != nil {
		return EditorPlanView{}, err
	}
	mode := r.editorPublicationMode
	if mode != "live" && mode != "mock" {
		mode = "disabled"
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return EditorPlanView{}, err
	}
	defer tx.Rollback(ctx)
	var productName, description string
	if err = tx.QueryRow(ctx, `SELECT name,description FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&productName, &description); err != nil {
		return EditorPlanView{}, err
	}
	for _, v := range plan.Variants {
		var sabyID, sku string
		var price int64
		var attrs map[string]any
		if err = tx.QueryRow(ctx, `SELECT COALESCE(saby_id,''),sku,base_price_minor,COALESCE((SELECT jsonb_object_agg(d.code,a.value) FROM variant_attribute_values a JOIN attribute_definitions d ON d.id=a.attribute_id WHERE a.variant_id=v.id),'{}'::jsonb) FROM product_variants v WHERE id=$1 AND product_id=$2 FOR UPDATE`, v.VariantID, productID).Scan(&sabyID, &sku, &price, &attrs); err != nil {
			return EditorPlanView{}, fmt.Errorf("%w: вариант не принадлежит карточке", ErrInvalidInput)
		}
		if !submit {
			continue
		}
		if mode == "live" {
			var legacyBusy bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_saby_exports WHERE variant_id=$1 AND state IN ('queued','checking','verifying','unknown'))`, v.VariantID).Scan(&legacyBusy); err != nil {
				return EditorPlanView{}, err
			}
			if legacyBusy {
				return EditorPlanView{}, fmt.Errorf("%w: дождитесь проверки предыдущей отправки в СБИС", ErrInvalidInput)
			}
		}
		linked, err := editorLinkedChannels(ctx, tx, v.VariantID, r.editorPublicationMode)
		if err != nil {
			return EditorPlanView{}, err
		}
		for channel, id := range linked {
			if id == "!ambiguous" {
				return EditorPlanView{}, fmt.Errorf("%w: у варианта несколько связей %s; проверьте соответствие карточек", ErrInvalidInput, channel)
			}
		}
		replacing := map[string]bool{}
		for channel, ch := range map[string]EditorChannel{"wb": v.Marketplace.WB, "ozon": v.Marketplace.Ozon} {
			if !ch.ReplaceLink || ch.Article == nil {
				continue
			}
			if strings.TrimSpace(*ch.Article) == "" {
				return EditorPlanView{}, fmt.Errorf("%w: укажите артикул продавца %s", ErrInvalidInput, channel)
			}
			article, err := editorLinkedArticle(ctx, tx, v.VariantID, channel, r.editorPublicationMode)
			if err != nil {
				return EditorPlanView{}, err
			}
			replacing[channel] = article != strings.TrimSpace(*ch.Article)
		}
		validation := v
		if linked["wb"] == "" && validation.Marketplace.WB.Mode == "link" {
			validation.Marketplace.WB.Enabled = true
		}
		if linked["ozon"] == "" && validation.Marketplace.Ozon.Mode == "link" {
			validation.Marketplace.Ozon.Enabled = true
		}
		if linked["wb"] != "" {
			validation.Marketplace.WB.Enabled = false
		}
		if linked["ozon"] != "" {
			validation.Marketplace.Ozon.Enabled = false
		}
		if err = ValidateEditorPlan(EditorPlan{Variants: []EditorVariantPlan{validation}}, true); err != nil {
			return EditorPlanView{}, err
		}
		if err = validateEditorDictionaries(ctx, tx, validation, r.editorPublicationMode, attrs); err != nil {
			return EditorPlanView{}, err
		}

		if v.Saby.Mode == "link" {
			code := strings.TrimSpace(v.Saby.ExistingCode)
			if code == "" {
				return EditorPlanView{}, fmt.Errorf("%w: укажите код существующего товара СБИС", ErrInvalidInput)
			}
			var id string
			var existingCodeMatches bool
			if sabyID != "" {
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saby_nomenclature WHERE saby_id=$1 AND lower(code)=lower($2) AND missing_since IS NULL) OR EXISTS(SELECT 1 FROM product_external_ids WHERE product_id=$3 AND variant_id=$4 AND provider='saby' AND id_type='code' AND status='active' AND lower(external_id)=lower($2))`, sabyID, code, productID, v.VariantID).Scan(&existingCodeMatches); err != nil {
					return EditorPlanView{}, err
				}
			}
			if existingCodeMatches {
				id = sabyID
			} else {
				var matches int
				if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(saby_id),'') FROM saby_nomenclature WHERE lower(code)=lower($1) AND missing_since IS NULL`, code).Scan(&matches, &id); err != nil {
					return EditorPlanView{}, err
				}
				if matches != 1 {
					return EditorPlanView{}, fmt.Errorf("%w: код СБИС не найден в загруженном справочнике", ErrInvalidInput)
				}
			}
			var occupied bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants WHERE saby_id=$1 AND id<>$2) OR EXISTS(SELECT 1 FROM product_external_ids WHERE provider='saby' AND id_type='id' AND external_id=$1 AND (variant_id IS DISTINCT FROM $2) AND product_id<>$3)`, id, v.VariantID, productID).Scan(&occupied); err != nil {
				return EditorPlanView{}, err
			}
			if occupied || sabyID != "" && sabyID != id {
				return EditorPlanView{}, fmt.Errorf("%w: связь СБИС уже занята; требуется проверка перед заменой", ErrInvalidInput)
			}
			if _, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2 WHERE id=$1`, v.VariantID, id); err != nil {
				return EditorPlanView{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source) VALUES($1,$2,'saby','id',$3,'manual') ON CONFLICT(provider,id_type,external_id) DO UPDATE SET variant_id=EXCLUDED.variant_id WHERE product_external_ids.product_id=EXCLUDED.product_id`, productID, v.VariantID, id); err != nil {
				return EditorPlanView{}, err
			}
			sabyID = id
		}
		if sabyID == "" && (v.Saby.Category.ID == "" || v.Saby.Folder.ID == "") {
			return EditorPlanView{}, fmt.Errorf("%w: выберите категорию и раздел СБИС", ErrInvalidInput)
		}
		var sabyUUID string
		if sabyID != "" {
			var uuids []string
			if err = tx.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT alias FROM (
			 SELECT external_id alias FROM product_external_ids WHERE product_id=$1 AND variant_id=$2 AND provider='saby' AND id_type='alias' AND status='active'
			 UNION SELECT unnest(external_ids) FROM saby_nomenclature WHERE saby_id=$3 AND missing_since IS NULL
			 ) candidates WHERE alias~*'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')`, productID, v.VariantID, sabyID).Scan(&uuids); err != nil {
				return EditorPlanView{}, err
			}
			if len(uuids) > 1 {
				return EditorPlanView{}, fmt.Errorf("%w: найдено несколько UUID связи СБИС; проверьте соответствие карточки", ErrInvalidInput)
			}
			if len(uuids) == 1 {
				sabyUUID = uuids[0]
			}
		}
		var barcode string
		if err = tx.QueryRow(ctx, `UPDATE product_variants SET generated_barcode=COALESCE(generated_barcode,catalog_editor_barcode()) WHERE id=$1 RETURNING generated_barcode`, v.VariantID).Scan(&barcode); err != nil {
			return EditorPlanView{}, err
		}
		images := []string{}
		seenImages := map[string]bool{}
		rows, e := tx.Query(ctx, `SELECT COALESCE(m.large_url,p.object_key) FROM product_media p LEFT JOIN media_mirror m ON m.source_url=p.object_key WHERE p.product_id=$1 AND (p.variant_id IS NULL OR p.variant_id=$2) ORDER BY (p.variant_id IS NULL AND p.is_primary<>0) DESC,p.is_primary DESC,p.sort_order,p.id`, productID, v.VariantID)
		if e != nil {
			return EditorPlanView{}, e
		}
		for rows.Next() {
			var url string
			if e = rows.Scan(&url); e != nil {
				rows.Close()
				return EditorPlanView{}, e
			}
			parsed, parseErr := urlpkg.Parse(url)
			if parseErr == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && !editorVideoURL(parsed.Path) && !seenImages[url] {
				seenImages[url] = true
				images = append(images, url)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return EditorPlanView{}, e
		}
		var packaging catalogeditor.Packaging
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT value FROM settings WHERE key='cdek.packaging_season'),'summer'),COALESCE(shipping_package_dimension($1,'package_length_cm'),0),COALESCE(shipping_package_dimension($1,'package_width_cm'),0),COALESCE(shipping_package_dimension($1,'package_height_cm'),0),COALESCE(variant_numeric_attribute($1,'package_weight_grams'),0)`, v.VariantID).Scan(&packaging.Season, &packaging.Length, &packaging.Width, &packaging.Height, &packaging.Weight); err != nil {
			return EditorPlanView{}, err
		}
		name := editorSabyName(productName, attrs)
		for channel, ch := range map[string]EditorChannel{"saby": {Enabled: true}, "ozon": v.Marketplace.Ozon, "wb": v.Marketplace.WB} {
			action := "create"
			existing := linked[channel]
			if channel == "saby" {
				existing = sabyID
			}
			if replacing[channel] {
				action = "resolve"
				existing = ""
				ch.Enabled = true
				ch.Mode = "create"
			} else if existing != "" {
				action = "update"
				ch.Enabled = true
			} else if ch.Mode == "link" {
				ch.Enabled = true
				action = "link"
			}
			if !ch.Enabled {
				if _, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET status='cancelled',message='Площадка отключена до отправки',updated_at=NOW() WHERE variant_id=$1 AND channel=$2 AND status IN ('prepared','queued')`, v.VariantID, channel); err != nil {
					return EditorPlanView{}, err
				}
				continue
			}
			if channel != "saby" && action == "create" {
				if price <= 0 || len(images) == 0 {
					return EditorPlanView{}, fmt.Errorf("%w: для площадки нужны цена и фото", ErrInvalidInput)
				}
			}
			channelBarcode := barcode
			if action == "resolve" {
				// A replacement card cannot reuse a barcode still owned by the old card.
				e := tx.QueryRow(ctx, `SELECT payload->>'barcode' FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2 AND payload->>'action'='resolve' AND payload->'channel'->>'article'=$3`, v.VariantID, channel, v.Marketplace.SellerArticle(channel)).Scan(&channelBarcode)
				if e == pgx.ErrNoRows {
					e = tx.QueryRow(ctx, `SELECT catalog_editor_barcode()`).Scan(&channelBarcode)
				}
				if e != nil {
					return EditorPlanView{}, e
				}
			}
			payload := map[string]any{"variantId": v.VariantID, "productId": productID, "name": name, "description": description, "priceMinor": price, "barcode": channelBarcode, "sku": sku, "sabyId": sabyID, "sabyUUID": sabyUUID, "saby": v.Saby, "marketplace": v.Marketplace, "channel": ch, "ozonPrice": v.OzonPrice, "wbPrice": v.WBPrice, "attributes": attrs, "images": images, "vat": "0", "season": "shared", "actorId": actor.CustomerID}
			if channel == "saby" {
				payload["action"], payload["existingId"] = action, existing
			}
			if channel != "saby" {
				schema := []catalogeditor.Attribute{}
				if action == "create" {
					schema, err = editorSchema(ctx, tx, channel, ch, r.editorPublicationMode)
					if err != nil {
						return EditorPlanView{}, err
					}
				}
				payload["action"] = action
				payload["existingId"] = existing
				payload["schema"] = schema
				payload["packaging"] = packaging
				payload["name"] = productName
			}
			raw, _ := json.Marshal(payload)
			fingerprintPayload := map[string]any{}
			_ = json.Unmarshal(raw, &fingerprintPayload)
			if channel == "saby" {
				for _, key := range []string{"action", "existingId", "sabyId", "sabyUUID", "actorId", "marketplace", "channel", "ozonPrice", "wbPrice", "vat", "season"} {
					delete(fingerprintPayload, key)
				}
				if config, ok := fingerprintPayload["saby"].(map[string]any); ok {
					delete(config, "mode")
					delete(config, "existingCode")
					for _, field := range []string{"category", "folder"} {
						if category, ok := config[field].(map[string]any); ok {
							delete(category, "name")
						}
					}
				}
			}
			if channel != "saby" {
				for _, key := range []string{"action", "existingId", "schema", "saby", "sabyId", "sabyUUID", "actorId", "barcode"} {
					delete(fingerprintPayload, key)
				}
				if c, ok := fingerprintPayload["channel"].(map[string]any); ok {
					delete(c, "enabled")
					delete(c, "replaceLink")
					delete(c, "existingId")
				}
				if m, ok := fingerprintPayload["marketplace"].(map[string]any); ok {
					for _, key := range []string{"wb", "ozon"} {
						if c, ok := m[key].(map[string]any); ok {
							delete(c, "enabled")
							delete(c, "replaceLink")
							delete(c, "existingId")
						}
					}
				}
			}
			fingerprintRaw, _ := json.Marshal(fingerprintPayload)
			sum := sha256.Sum256(fingerprintRaw)
			hash := hex.EncodeToString(sum[:])
			// Persist the complete immutable request. Sending is separate from saving;
			// configured providers consume this durable plan without re-reading a draft.
			var priorStatus, priorHash, priorMode string
			var priorProgress catalogeditor.Progress
			priorErr := tx.QueryRow(ctx, `SELECT status,fingerprint,execution_mode,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2 FOR UPDATE`, v.VariantID, channel).Scan(&priorStatus, &priorHash, &priorMode, &priorProgress)
			if priorErr != nil && priorErr != pgx.ErrNoRows {
				return EditorPlanView{}, priorErr
			}
			if priorErr == nil && priorMode != mode {
				finished := priorStatus == "created" || priorStatus == "synced" || priorStatus == "linked" || priorStatus == "simulated"
				if !finished && (priorProgress.CreateAttempted || priorProgress.Ambiguous || priorStatus == "processing" || priorStatus == "queued" || priorStatus == "needs_attention") {
					return EditorPlanView{}, fmt.Errorf("%w: предыдущая отправка площадки не завершена; сначала проверьте её результат", ErrInvalidInput)
				}
			}
			if priorHash != "" && priorHash != hash && priorMode == mode && (priorStatus == "processing" || priorStatus == "queued" || priorStatus == "needs_attention") {
				if _, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET pending_payload=$3,pending_fingerprint=$4,updated_at=NOW() WHERE variant_id=$1 AND channel=$2`, v.VariantID, channel, raw, hash); err != nil {
					return EditorPlanView{}, err
				}
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,message,execution_mode) VALUES($1,$2,$3,$4,'prepared','Подготовлено к созданию',$5) ON CONFLICT(variant_id,channel) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,payload=EXCLUDED.payload,execution_mode=EXCLUDED.execution_mode,pending_payload=NULL,pending_fingerprint=NULL,progress=CASE WHEN catalog_editor_publications.fingerprint=EXCLUDED.fingerprint AND catalog_editor_publications.execution_mode=EXCLUDED.execution_mode THEN catalog_editor_publications.progress ELSE '{}'::jsonb END,next_attempt_at=NOW(),status=CASE WHEN catalog_editor_publications.fingerprint=EXCLUDED.fingerprint AND catalog_editor_publications.execution_mode=EXCLUDED.execution_mode AND catalog_editor_publications.status<>'cancelled' THEN catalog_editor_publications.status ELSE 'prepared' END,message=CASE WHEN catalog_editor_publications.fingerprint=EXCLUDED.fingerprint AND catalog_editor_publications.execution_mode=EXCLUDED.execution_mode THEN catalog_editor_publications.message ELSE EXCLUDED.message END,updated_at=NOW()`, v.VariantID, channel, hash, raw, mode); err != nil {
				return EditorPlanView{}, err
			}
		}
	}
	raw, _ := json.Marshal(plan)
	if _, err = tx.Exec(ctx, `INSERT INTO catalog_editor_plans(product_id,config) VALUES($1,$2) ON CONFLICT(product_id) DO UPDATE SET config=EXCLUDED.config,updated_at=NOW()`, productID, raw); err != nil {
		return EditorPlanView{}, err
	}
	if err = insertAudit(ctx, tx, actor, "catalog.editor.plan.save", "product", fmt.Sprint(productID), nil, plan); err != nil {
		return EditorPlanView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EditorPlanView{}, err
	}
	return r.ReadEditorPlan(ctx, productID)
}

func editorVideoURL(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".mp4") || strings.HasSuffix(p, ".mov") || strings.HasSuffix(p, ".webm") || strings.HasSuffix(p, ".m3u8")
}
