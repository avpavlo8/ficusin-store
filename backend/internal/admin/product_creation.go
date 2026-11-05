package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// The first release is intentionally restricted to an isolated staging DB.
// These schemas are labelled test schemas, never advertised as live platform
// requirements. Production must supply real category/attribute dictionaries
// and durable provider commands before enabling this flow there.
type CreationField struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	Shared   bool     `json:"shared,omitempty"`
}
type CreationCategory struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Fields []CreationField `json:"fields"`
}
type CreationFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type CreationOptions struct {
	Enabled     bool               `json:"enabled"`
	Simulation  bool               `json:"simulation"`
	SabyFolders []CreationFolder   `json:"sabyFolders"`
	WB          []CreationCategory `json:"wb"`
	Ozon        []CreationCategory `json:"ozon"`
}
type CreationChannel struct {
	Enabled    bool              `json:"enabled"`
	Category   string            `json:"category"`
	PriceMinor int64             `json:"priceMinor"`
	Values     map[string]string `json:"values"`
}
type CreationInput struct {
	Step               int             `json:"step"`
	Name               string          `json:"name"`
	LatinName          string          `json:"latinName"`
	Description        string          `json:"description"`
	CategoryID         int64           `json:"categoryId"`
	PriceMinor         int64           `json:"priceMinor"`
	WholesaleMinQty    int             `json:"wholesaleMinQty"`
	PotCM              float64         `json:"potCm"`
	HeightCM           float64         `json:"heightCm"`
	PackageLengthCM    int             `json:"packageLengthCm"`
	PackageWidthCM     int             `json:"packageWidthCm"`
	PackageHeightCM    int             `json:"packageHeightCm"`
	PackageWeightGrams int             `json:"packageWeightGrams"`
	Attributes         map[string]any  `json:"attributes"`
	VariantAttributes  map[string]any  `json:"variantAttributes"`
	Photos             []string        `json:"photos"`
	SabyFolder         string          `json:"sabyFolder"`
	WB                 CreationChannel `json:"wb"`
	Ozon               CreationChannel `json:"ozon"`
}
type CreationPhoto struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
}
type CreationResult struct {
	Channel    string `json:"channel"`
	ExternalID string `json:"externalId"`
	State      string `json:"state"`
}
type CreationDraft struct {
	ID         string           `json:"id"`
	Revision   int              `json:"revision"`
	Simulation bool             `json:"simulation"`
	Data       CreationInput    `json:"data"`
	Photos     []CreationPhoto  `json:"photos"`
	Product    *Product         `json:"product,omitempty"`
	Results    []CreationResult `json:"results"`
}

func creationID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}
func (r *PostgresRepository) creationAccess(ctx context.Context, actor Actor) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	var db string
	if err := r.pool.QueryRow(ctx, "SELECT current_database()").Scan(&db); err != nil {
		return err
	}
	if db != "ficusin_staging" && !strings.HasPrefix(db, "ficusin_staging_") {
		return fmt.Errorf("%w: мастер пока доступен только в изолированном стейджинге", ErrForbidden)
	}
	return nil
}
func creationSchemas() (wb, ozon []CreationCategory) {
	shared := []CreationField{
		{Code: "brand", Name: "Бренд", Type: "text", Required: true, Shared: true},
		{Code: "plantKind", Name: "Вид растения", Type: "text", Required: true, Shared: true},
		{Code: "country", Name: "Страна производства", Type: "select", Required: true, Options: []string{"Россия", "Нидерланды", "Китай", "Польша"}, Shared: true},
		{Code: "contents", Name: "Комплектация", Type: "text", Required: true, Shared: true},
	}
	wb = []CreationCategory{{ID: "STG-WB-PLANTS", Name: "Комнатные растения — тестовая категория", Fields: shared}}
	fields := append([]CreationField{}, shared...)
	fields = append(fields, CreationField{Code: "tnved", Name: "Код ТН ВЭД", Type: "text", Required: true}, CreationField{Code: "vat", Name: "НДС", Type: "text", Required: true})
	ozon = []CreationCategory{{ID: "STG-OZON-PLANTS", Name: "Комнатные растения — тестовая категория", Fields: fields}}
	return
}
func (r *PostgresRepository) ProductCreationOptions(ctx context.Context, actor Actor) (CreationOptions, error) {
	if err := r.creationAccess(ctx, actor); err != nil {
		return CreationOptions{}, err
	}
	wb, ozon := creationSchemas()
	options := CreationOptions{Enabled: true, Simulation: true, WB: wb, Ozon: ozon, SabyFolders: []CreationFolder{{ID: "STG-CATALOG", Name: "Тестовый каталог СБИС"}}}
	rows, err := r.pool.Query(ctx, `SELECT saby_id,CASE WHEN cardinality(path)>0 THEN array_to_string(path,' / ') ELSE name END FROM saby_catalog_folders WHERE missing_since IS NULL ORDER BY name LIMIT 2000`)
	if err != nil {
		return options, err
	}
	defer rows.Close()
	for rows.Next() {
		var f CreationFolder
		if err = rows.Scan(&f.ID, &f.Name); err != nil {
			return options, err
		}
		options.SabyFolders = append(options.SabyFolders, f)
	}
	return options, rows.Err()
}
func (r *PostgresRepository) NewProductCreation(ctx context.Context, actor Actor) (CreationDraft, error) {
	if err := r.creationAccess(ctx, actor); err != nil {
		return CreationDraft{}, err
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO product_creation_drafts(id,customer_id) VALUES($1,$2) ON CONFLICT(customer_id) WHERE product_id IS NULL DO NOTHING`, creationID(), actor.CustomerID)
	if err != nil {
		return CreationDraft{}, err
	}
	var id string
	if err = r.pool.QueryRow(ctx, `SELECT id FROM product_creation_drafts WHERE customer_id=$1 AND product_id IS NULL`, actor.CustomerID).Scan(&id); err != nil {
		return CreationDraft{}, err
	}
	return r.ProductCreation(ctx, actor, id)
}
func (r *PostgresRepository) ProductCreation(ctx context.Context, actor Actor, id string) (CreationDraft, error) {
	if err := r.creationAccess(ctx, actor); err != nil {
		return CreationDraft{}, err
	}
	d := CreationDraft{ID: id, Simulation: true, Photos: []CreationPhoto{}, Results: []CreationResult{}}
	var raw []byte
	var productID *int64
	if err := r.pool.QueryRow(ctx, `SELECT revision,data,product_id FROM product_creation_drafts WHERE id=$1 AND customer_id=$2`, id, actor.CustomerID).Scan(&d.Revision, &raw, &productID); err != nil {
		return d, err
	}
	if err := json.Unmarshal(raw, &d.Data); err != nil {
		return d, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id,filename FROM product_creation_photos WHERE draft_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var p CreationPhoto
		if err = rows.Scan(&p.ID, &p.Filename); err != nil {
			rows.Close()
			return d, err
		}
		p.URL = "/api/v1/product-creation-photos/" + p.ID
		d.Photos = append(d.Photos, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return d, err
	}
	if productID != nil {
		p, err := r.productByID(ctx, *productID)
		if err != nil {
			return d, err
		}
		d.Product = &p
	}
	results, err := r.pool.Query(ctx, `SELECT channel,external_id FROM product_creation_staging_cards WHERE draft_id=$1 ORDER BY channel`, id)
	if err != nil {
		return d, err
	}
	defer results.Close()
	for results.Next() {
		var result CreationResult
		if err = results.Scan(&result.Channel, &result.ExternalID); err != nil {
			return d, err
		}
		result.State = "simulated"
		d.Results = append(d.Results, result)
	}
	return d, results.Err()
}
func (r *PostgresRepository) SaveProductCreation(ctx context.Context, actor Actor, id string, revision int, input CreationInput) (CreationDraft, error) {
	if err := r.creationAccess(ctx, actor); err != nil {
		return CreationDraft{}, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return CreationDraft{}, err
	}
	if len(raw) > 100000 {
		return CreationDraft{}, fmt.Errorf("%w: слишком большой черновик", ErrInvalidInput)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE product_creation_drafts SET data=$4,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND customer_id=$2 AND revision=$3 AND product_id IS NULL`, id, actor.CustomerID, revision, raw)
	if err != nil {
		return CreationDraft{}, err
	}
	if tag.RowsAffected() != 1 {
		return CreationDraft{}, fmt.Errorf("%w: черновик изменён в другом окне; откройте мастер заново", ErrInvalidInput)
	}
	return r.ProductCreation(ctx, actor, id)
}
func (r *PostgresRepository) AddProductCreationPhoto(ctx context.Context, actor Actor, id, filename string, image []byte) (CreationDraft, error) {
	if err := r.creationAccess(ctx, actor); err != nil {
		return CreationDraft{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CreationDraft{}, err
	}
	defer tx.Rollback(ctx)
	var draft string
	if err = tx.QueryRow(ctx, `SELECT id FROM product_creation_drafts WHERE id=$1 AND customer_id=$2 AND product_id IS NULL FOR UPDATE`, id, actor.CustomerID).Scan(&draft); err != nil {
		return CreationDraft{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM product_creation_photos WHERE draft_id=$1`, id).Scan(&count); err != nil {
		return CreationDraft{}, err
	}
	if count >= 12 {
		return CreationDraft{}, fmt.Errorf("%w: можно добавить до 12 фотографий", ErrInvalidInput)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO product_creation_photos(id,draft_id,filename,image) VALUES($1,$2,$3,$4)`, creationID(), id, filename, image); err != nil {
		return CreationDraft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CreationDraft{}, err
	}
	return r.ProductCreation(ctx, actor, id)
}
func (r *PostgresRepository) ProductCreationPhoto(ctx context.Context, id string) ([]byte, error) {
	// Public only inside the staging access gate, so the newly-created site's
	// product can use the same image URL without an admin session.
	if err := r.creationAccess(ctx, Actor{Role: RoleOwner}); err != nil {
		return nil, err
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT image FROM product_creation_photos WHERE id=$1`, id).Scan(&raw)
	return raw, err
}
func validateCreation(input CreationInput, options CreationOptions) error {
	if strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > 500 || strings.TrimSpace(input.LatinName) == "" || strings.TrimSpace(input.Description) == "" || len([]rune(input.Description)) > 10000 || input.CategoryID <= 0 {
		return fmt.Errorf("%w: заполните название, латинское название, описание и категорию сайта", ErrInvalidInput)
	}
	finite := func(v float64, max float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 && v <= max }
	if input.PriceMinor <= 0 || input.PriceMinor > 1_000_000_000 || input.WholesaleMinQty < 1 || input.WholesaleMinQty > 100000 || !finite(input.PotCM, 500) || !finite(input.HeightCM, 3000) {
		return fmt.Errorf("%w: проверьте цену, минимальное количество, горшок и высоту", ErrInvalidInput)
	}
	if len(input.Photos) == 0 || len(input.Photos) > 12 {
		return fmt.Errorf("%w: добавьте от 1 до 12 фотографий", ErrInvalidInput)
	}
	seen := map[string]bool{}
	for _, id := range input.Photos {
		if id == "" || seen[id] {
			return fmt.Errorf("%w: проверьте фотографии", ErrInvalidInput)
		}
		seen[id] = true
	}
	folderOK := false
	for _, f := range options.SabyFolders {
		if f.ID == input.SabyFolder {
			folderOK = true
		}
	}
	if !folderOK {
		return fmt.Errorf("%w: выберите каталог СБИС", ErrInvalidInput)
	}
	for _, item := range []struct {
		channel    CreationChannel
		categories []CreationCategory
		name       string
	}{{input.WB, options.WB, "Wildberries"}, {input.Ozon, options.Ozon, "Ozon"}} {
		if !item.channel.Enabled {
			continue
		}
		if item.channel.PriceMinor <= 0 || item.channel.PriceMinor > 1_000_000_000 {
			return fmt.Errorf("%w: укажите цену %s", ErrInvalidInput, item.name)
		}
		if input.PackageLengthCM <= 0 || input.PackageWidthCM <= 0 || input.PackageHeightCM <= 0 || input.PackageWeightGrams <= 0 || input.PackageLengthCM > 1000 || input.PackageWidthCM > 1000 || input.PackageHeightCM > 3000 || input.PackageWeightGrams > 1000000 {
			return fmt.Errorf("%w: заполните размеры и вес упаковки для площадок", ErrInvalidInput)
		}
		found := false
		for _, cat := range item.categories {
			if cat.ID != item.channel.Category {
				continue
			}
			found = true
			for _, field := range cat.Fields {
				value := strings.TrimSpace(item.channel.Values[field.Code])
				if field.Required && value == "" {
					return fmt.Errorf("%w: %s — %s обязательно", ErrInvalidInput, item.name, field.Name)
				}
				if len(value) > 1000 {
					return fmt.Errorf("%w: %s слишком длинное значение", ErrInvalidInput, field.Name)
				}
				if len(field.Options) > 0 && value != "" {
					ok := false
					for _, v := range field.Options {
						if value == v {
							ok = true
						}
					}
					if !ok {
						return fmt.Errorf("%w: %s — выберите значение %s из списка", ErrInvalidInput, item.name, field.Name)
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("%w: выберите категорию %s", ErrInvalidInput, item.name)
		}
	}
	return nil
}
func (r *PostgresRepository) FinishProductCreation(ctx context.Context, actor Actor, id string, revision int) (CreationDraft, error) {
	options, err := r.ProductCreationOptions(ctx, actor)
	if err != nil {
		return CreationDraft{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CreationDraft{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var current int
	var productID *int64
	if err = tx.QueryRow(ctx, `SELECT data,revision,product_id FROM product_creation_drafts WHERE id=$1 AND customer_id=$2 FOR UPDATE`, id, actor.CustomerID).Scan(&raw, &current, &productID); err != nil {
		return CreationDraft{}, err
	}
	if productID != nil {
		_ = tx.Rollback(ctx)
		return r.ProductCreation(ctx, actor, id)
	}
	if current != revision {
		return CreationDraft{}, fmt.Errorf("%w: черновик изменился; откройте мастер заново", ErrInvalidInput)
	}
	var input CreationInput
	if err = json.Unmarshal(raw, &input); err != nil {
		return CreationDraft{}, err
	}
	if err = validateCreation(input, options); err != nil {
		return CreationDraft{}, err
	}
	// Required category attributes are checked again on the server.
	schema, err := r.EffectiveCategoryAttributes(ctx, input.CategoryID)
	if err != nil {
		return CreationDraft{}, err
	}
	values := input.Attributes
	if values == nil {
		values = map[string]any{}
	}
	variantValues := input.VariantAttributes
	if variantValues == nil {
		variantValues = map[string]any{}
	}
	variantValues["pot_diameter_cm"] = input.PotCM
	variantValues["height_cm"] = input.HeightCM
	for key, value := range map[string]int{"package_length_cm": input.PackageLengthCM, "package_width_cm": input.PackageWidthCM, "package_height_cm": input.PackageHeightCM, "package_weight_grams": input.PackageWeightGrams} {
		if value > 0 {
			variantValues[key] = float64(value)
		}
	}
	for _, field := range schema {
		if !field.Required || field.Excluded {
			continue
		}
		v := values[field.Code]
		if field.Scope == "variant" {
			v = variantValues[field.Code]
		}
		if v == nil || fmt.Sprint(v) == "" || fmt.Sprint(v) == "[]" {
			return CreationDraft{}, fmt.Errorf("%w: заполните обязательное поле %s", ErrInvalidInput, field.Name)
		}
	}
	images := []string{}
	for _, photoID := range input.Photos {
		var photo string
		if err = tx.QueryRow(ctx, `SELECT id FROM product_creation_photos WHERE id=$1 AND draft_id=$2`, photoID, id).Scan(&photo); err != nil {
			return CreationDraft{}, fmt.Errorf("%w: фотография не принадлежит черновику", ErrInvalidInput)
		}
		images = append(images, "/api/v1/product-creation-photos/"+photoID)
	}
	var section string
	if err = tx.QueryRow(ctx, `WITH RECURSIVE tree AS (SELECT id,parent_id,slug FROM categories WHERE id=$1 UNION ALL SELECT c.id,c.parent_id,c.slug FROM categories c JOIN tree t ON t.parent_id=c.id) SELECT slug FROM tree WHERE parent_id IS NULL LIMIT 1`, input.CategoryID).Scan(&section); err != nil {
		return CreationDraft{}, fmt.Errorf("%w: категория сайта не найдена", ErrInvalidInput)
	}
	product, err := createProduct(ctx, tx, seed{name: strings.TrimSpace(input.Name), latinName: strings.TrimSpace(input.LatinName), description: strings.TrimSpace(input.Description), categoryID: &input.CategoryID, catalogSection: section, priceMinor: input.PriceMinor, status: "draft", images: images, stock: 0})
	if err != nil {
		return CreationDraft{}, err
	}
	var variant int64
	var sku, code string
	if err = tx.QueryRow(ctx, `SELECT v.id,v.sku,p.product_code::text FROM product_variants v JOIN products p ON p.id=v.product_id WHERE p.id=$1 ORDER BY v.id LIMIT 1`, product).Scan(&variant, &sku, &code); err != nil {
		return CreationDraft{}, err
	}
	if err = saveProductAttributes(ctx, tx, product, values); err != nil {
		return CreationDraft{}, err
	}

	if err = saveVariantPIMValues(ctx, tx, product, variant, variantValues); err != nil {
		return CreationDraft{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET wholesale_min_qty=$2,label=$3 WHERE id=$1`, variant, input.WholesaleMinQty, fmt.Sprintf("Горшок %g см", input.PotCM)); err != nil {
		return CreationDraft{}, err
	}
	// Stage's Saby card uses the same reserved identity as the existing staging
	// export provider. WB/Ozon copies are local test cards, never real listings.
	sabyID := "STG-" + code
	remote, _ := json.Marshal(map[string]any{"ID": sabyID, "Code": code, "Article": sku, "Name": input.Name, "PotCM": input.PotCM, "HeightCM": input.HeightCM, "PriceMinor": input.PriceMinor})
	if _, err = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,article,name,description,price_minor,balance,folder_saby_id) VALUES($1,$2,$3,$4,$5,$6,0,$7)`, sabyID, code, sku, input.Name, string(remote), input.PriceMinor, input.SabyFolder); err != nil {
		return CreationDraft{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE products SET saby_id=$2 WHERE id=$1`, product, sabyID); err != nil {
		return CreationDraft{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2 WHERE id=$1`, variant, sabyID); err != nil {
		return CreationDraft{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id) VALUES($1,$2,'saby','id',$3)`, product, variant, sabyID); err != nil {
		return CreationDraft{}, err
	}
	for _, channel := range []struct {
		name    string
		enabled bool
	}{{"saby", true}, {"wb", input.WB.Enabled}, {"ozon", input.Ozon.Enabled}} {
		if !channel.enabled {
			continue
		}
		external := "STG-" + strings.ToUpper(channel.name) + "-" + code
		if _, err = tx.Exec(ctx, `INSERT INTO product_creation_staging_cards(draft_id,channel,external_id,data) VALUES($1,$2,$3,$4)`, id, channel.name, external, raw); err != nil {
			return CreationDraft{}, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE product_creation_drafts SET product_id=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, product); err != nil {
		return CreationDraft{}, err
	}
	if err = insertAudit(ctx, tx, actor, "product.creation.staging", "product", fmt.Sprint(product), nil, map[string]any{"draftId": id, "simulation": true}); err != nil {
		return CreationDraft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CreationDraft{}, err
	}
	return r.ProductCreation(ctx, actor, id)
}

func (r *PostgresRepository) DeleteProductCreationPhoto(ctx context.Context, actor Actor, id, photoID string) (CreationDraft, error) {
	if err := r.creationAccess(ctx, actor); err != nil {
		return CreationDraft{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CreationDraft{}, err
	}
	defer tx.Rollback(ctx)
	var draft string
	if err = tx.QueryRow(ctx, `SELECT id FROM product_creation_drafts WHERE id=$1 AND customer_id=$2 AND product_id IS NULL FOR UPDATE`, id, actor.CustomerID).Scan(&draft); err != nil {
		return CreationDraft{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM product_creation_photos WHERE id=$1 AND draft_id=$2`, photoID, id); err != nil {
		return CreationDraft{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE product_creation_drafts SET data=jsonb_set(data,'{photos}',COALESCE((SELECT jsonb_agg(value) FROM jsonb_array_elements(COALESCE(data->'photos','[]'::jsonb)) WHERE value<>to_jsonb($2::text)),'[]'::jsonb)),revision=revision+1 WHERE id=$1`, id, photoID); err != nil {
		return CreationDraft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CreationDraft{}, err
	}
	return r.ProductCreation(ctx, actor, id)
}
