package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NewMockCatalog exercises the real HTTP adapters against deterministic local
// responses. This transport cannot make a network connection, even if a seller
// endpoint or real credential is accidentally supplied.
func NewMockCatalog() *CatalogPublisher {
	e := NewMarketplaceExecutor("local-test", "local-test", "local-test")
	e.ozonBase = "http://catalog-mock.invalid"
	e.wbContentBase = e.ozonBase
	e.wbBase = e.ozonBase
	e.client = &http.Client{Transport: &catalogMockTransport{cards: map[string]mockCard{}}}
	return &CatalogPublisher{Executor: e, Mock: true}
}

type mockCard struct {
	Document       map[string]any `json:"document,omitempty"`
	ID             int64
	Offer, Barcode string
	Price          float64
	Images         []string
}
type catalogMockTransport struct {
	pool   *pgxpool.Pool
	mu     sync.Mutex
	cards  map[string]mockCard
	serial int64
}

func (m *catalogMockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "catalog-mock.invalid" {
		return nil, fmt.Errorf("локальный API запретил внешний запрос")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var in map[string]json.RawMessage
	var raw []byte
	if r.Body != nil {
		raw, _ = io.ReadAll(r.Body)
	}
	_ = json.Unmarshal(raw, &in)
	status := 200
	var out any
	path := r.URL.Path
	switch path {
	case "/v1/description-category/tree":
		out = map[string]any{"result": []any{map[string]any{"description_category_id": 900001, "category_name": "Локальный тест: растения", "children": []any{map[string]any{"type_id": 900002, "type_name": "Саженец (тест API)", "children": []any{}}}}}}
	case "/content/v2/object/all":
		out = map[string]any{"data": []any{map[string]any{"subjectID": 900003, "subjectName": "Растения (тест API)", "parentName": "Локальный тест"}}}
	case "/v1/description-category/attribute":
		out = map[string]any{"result": []any{
			map[string]any{"id": 85, "name": "Бренд", "is_required": true, "dictionary_id": 85, "type": "String", "max_value_count": 1},
			map[string]any{"id": 22232, "name": "Код ТН ВЭД ЕАЭС", "is_required": true, "dictionary_id": 22232, "type": "String", "max_value_count": 1},
			map[string]any{"id": 8229, "name": "Тип", "is_required": true, "dictionary_id": 8229, "type": "String", "max_value_count": 1},
			map[string]any{"id": 9048, "name": "Название модели", "is_required": true, "type": "String", "max_value_count": 1},
			map[string]any{"id": 4191, "name": "Аннотация", "is_required": true, "type": "String", "max_value_count": 1},
			map[string]any{"id": 90001, "name": "Единиц в одном товаре", "is_required": true, "type": "Integer", "max_value_count": 1},
			map[string]any{"id": 90002, "name": "Страна производства", "is_required": true, "dictionary_id": 90002, "type": "String", "max_value_count": 1},
		}}
	case "/content/v2/object/charcs/900003":
		out = map[string]any{"data": []any{map[string]any{"charcID": 15000001, "name": "Страна производства", "required": true, "charcType": 1, "maxCount": 1}, map[string]any{"charcID": 15000002, "name": "Диаметр горшка", "required": true, "charcType": 4, "maxCount": 1}, map[string]any{"charcID": 15000003, "name": "Код ТН ВЭД", "required": true, "charcType": 1, "maxCount": 1}}}
	case "/v1/description-category/attribute/values", "/v1/description-category/attribute/values/search":
		var attr int64
		json.Unmarshal(in["attribute_id"], &attr)
		values := []map[string]any{{"id": 900101, "value": "Фикусин"}}
		if attr == 22232 {
			values = []map[string]any{{"id": 900102, "value": "0602909900"}}
		}
		if attr == 90002 {
			values = []map[string]any{{"id": 900103, "value": "Россия"}, {"id": 900104, "value": "Нидерланды"}}
		}
		out = map[string]any{"result": values, "has_next": false}
	case "/api/content/v1/brands":
		out = map[string]any{"brands": []any{map[string]any{"id": 1, "name": "Фикусин"}}, "next": 0}
	case "/content/v2/directory/tnved":
		out = map[string]any{"data": []any{map[string]any{"tnved": "0602909900", "description": "Локальный тестовый код"}}}
	case "/content/v2/directory/countries":
		out = map[string]any{"data": []any{map[string]any{"name": "Россия", "fullName": "Российская Федерация"}, map[string]any{"name": "Нидерланды", "fullName": "Нидерланды"}}}
	case "/v3/product/import":
		var body struct {
			Items []struct {
				Article string   `json:"offer_id"`
				Barcode string   `json:"barcode"`
				Price   string   `json:"price"`
				Images  []string `json:"images"`
				Primary string   `json:"primary_image"`
			} `json:"items"`
		}
		json.Unmarshal(raw, &body)
		if len(body.Items) != 1 {
			return nil, fmt.Errorf("mock: ожидалась одна карточка")
		}
		v := body.Items[0]
		m.serial++
		id := 990000000 + m.serial
		if prior, ok := m.cards["ozon:"+v.Article]; ok {
			id = prior.ID
		}
		card := mockCard{ID: id, Offer: v.Article, Barcode: v.Barcode, Images: append([]string{v.Primary}, v.Images...)}
		fmt.Sscan(v.Price, &card.Price)
		var request map[string]any
		json.Unmarshal(raw, &request)
		if items, ok := request["items"].([]any); ok && len(items) > 0 {
			card.Document, _ = items[0].(map[string]any)
		}
		m.cards["ozon:"+v.Article] = card
		out = map[string]any{"result": map[string]any{"task_id": card.ID}}
	case "/v4/product/info/attributes":
		var body struct {
			Filter struct {
				Offers []string `json:"offer_id"`
			} `json:"filter"`
		}
		json.Unmarshal(raw, &body)
		items := []any{}
		for _, offer := range body.Filter.Offers {
			if c, ok := m.cards["ozon:"+offer]; ok {
				doc := map[string]any{}
				for key, value := range c.Document {
					doc[key] = value
				}
				doc["id"] = c.ID
				doc["offer_id"] = c.Offer
				doc["barcode"] = c.Barcode
				doc["images"] = c.Images
				items = append(items, doc)
			}
		}
		out = map[string]any{"result": items}
	case "/v5/product/info/prices":
		var body struct {
			Filter struct {
				Offers []string `json:"offer_id"`
			} `json:"filter"`
		}
		json.Unmarshal(raw, &body)
		items := []any{}
		for _, offer := range body.Filter.Offers {
			if c, ok := m.cards["ozon:"+offer]; ok {
				items = append(items, map[string]any{"offer_id": offer, "price": map[string]any{"price": fmt.Sprintf("%.2f", c.Price)}})
			}
		}
		out = map[string]any{"items": items}
	case "/v1/product/import/info":
		var task int64
		json.Unmarshal(in["task_id"], &task)
		items := []any{}
		for key, c := range m.cards {
			if strings.HasPrefix(key, "ozon:") && c.ID == task {
				items = append(items, map[string]any{"offer_id": c.Offer, "product_id": c.ID, "status": "imported", "errors": []any{}})
			}
		}
		out = map[string]any{"result": map[string]any{"items": items}}
	case "/v3/product/info/list":
		var offers []string
		json.Unmarshal(in["offer_id"], &offers)
		items := []any{}
		for _, offer := range offers {
			if c, ok := m.cards["ozon:"+offer]; ok {
				items = append(items, map[string]any{"id": c.ID, "offer_id": c.Offer, "barcodes": []string{c.Barcode}, "images": c.Images, "sources": []any{map[string]any{"sku": c.ID + 100000000}}, "statuses": map[string]any{"status_name": "Локальный тест", "is_created": true}})
			}
		}
		out = map[string]any{"items": items}
	case "/content/v2/cards/upload":
		var body []struct {
			Variants []struct {
				Article string `json:"vendorCode"`
				Sizes   []struct {
					Price float64  `json:"price"`
					SKUs  []string `json:"skus"`
				} `json:"sizes"`
			} `json:"variants"`
		}
		json.Unmarshal(raw, &body)
		if len(body) != 1 || len(body[0].Variants) != 1 || len(body[0].Variants[0].Sizes) == 0 {
			return nil, fmt.Errorf("mock: некорректный запрос WB")
		}
		v := body[0].Variants[0]
		m.serial++
		var request []map[string]any
		json.Unmarshal(raw, &request)
		variants, _ := request[0]["variants"].([]any)
		doc, _ := variants[0].(map[string]any)
		doc["subjectID"] = request[0]["subjectID"]
		m.cards["wb:"+v.Article] = mockCard{ID: 880000000 + m.serial, Offer: v.Article, Barcode: v.Sizes[0].SKUs[0], Price: v.Sizes[0].Price, Document: doc}
		out = map[string]any{"error": false}
	case "/content/v2/cards/update":
		var body []struct {
			ID      int64  `json:"nmID"`
			Article string `json:"vendorCode"`
		}
		json.Unmarshal(raw, &body)
		for _, v := range body {
			if prior, ok := m.cards["wb:"+v.Article]; !ok || prior.ID != v.ID {
				return nil, fmt.Errorf("mock: обновление неизвестной карточки")
			}
		}
		var updates []map[string]any
		json.Unmarshal(raw, &updates)
		for _, doc := range updates {
			article, _ := doc["vendorCode"].(string)
			card := m.cards["wb:"+article]
			if card.Document == nil {
				card.Document = map[string]any{}
			}
			for key, value := range doc {
				card.Document[key] = value
			}
			m.cards["wb:"+article] = card
		}
		out = map[string]any{"error": false}
	case "/content/v2/get/cards/list":
		var body struct {
			Settings struct {
				Filter struct {
					Text string `json:"textSearch"`
				} `json:"filter"`
			} `json:"settings"`
		}
		json.Unmarshal(raw, &body)
		cards := []any{}
		for key, c := range m.cards {
			if strings.HasPrefix(key, "wb:") && (c.Offer == body.Settings.Filter.Text || fmt.Sprint(c.ID) == body.Settings.Filter.Text) {
				doc := map[string]any{}
				for key, value := range c.Document {
					doc[key] = value
				}
				doc["nmID"] = c.ID
				doc["vendorCode"] = c.Offer
				doc["photos"] = c.Images
				if doc["sizes"] == nil {
					doc["sizes"] = []any{map[string]any{"skus": []string{c.Barcode}}}
				}
				cards = append(cards, doc)
			}
		}
		out = map[string]any{"cards": cards, "cursor": map[string]any{"total": len(cards)}}
	case "/content/v2/cards/error/list":
		out = map[string]any{"data": []any{}}
	case "/content/v3/media/save":
		var body struct {
			ID   int64    `json:"nmId"`
			Data []string `json:"data"`
		}
		json.Unmarshal(raw, &body)
		for key, c := range m.cards {
			if c.ID == body.ID {
				c.Images = body.Data
				m.cards[key] = c
			}
		}
		out = map[string]any{"error": false}
	case "/api/v2/upload/task":
		var body struct {
			Data []struct {
				ID    int64   `json:"nmID"`
				Price float64 `json:"price"`
			} `json:"data"`
		}
		json.Unmarshal(raw, &body)
		for _, v := range body.Data {
			for key, c := range m.cards {
				if c.ID == v.ID {
					c.Price = v.Price
					m.cards[key] = c
				}
			}
		}
		out = map[string]any{"data": map[string]any{"id": 1}, "error": false}
	case "/api/v2/list/goods/filter":
		cards := []any{}
		for key, c := range m.cards {
			if strings.HasPrefix(key, "wb:") && fmt.Sprint(c.ID) == r.URL.Query().Get("filterNmID") {
				cards = append(cards, map[string]any{"nmID": c.ID, "sizes": []any{map[string]any{"discountedPrice": c.Price}}})
			}
		}
		out = map[string]any{"data": map[string]any{"listGoods": cards}}
	default:
		status = 404
		out = map[string]any{"error": "Локальный API не поддерживает этот метод"}
	}
	if m.pool != nil && (path == "/v3/product/import" || path == "/content/v2/cards/upload" || path == "/content/v2/cards/update" || path == "/content/v3/media/save" || path == "/api/v2/upload/task") {
		snapshot, _ := json.Marshal(struct {
			Cards  map[string]mockCard `json:"cards"`
			Serial int64               `json:"serial"`
		}{m.cards, m.serial})
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, err := m.pool.Exec(ctx, `INSERT INTO catalog_editor_mock_state(key,payload) VALUES('vendor-v1',$1) ON CONFLICT(key) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, snapshot); err != nil {
			return nil, err
		}
	}
	data, _ := json.Marshal(out)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
}

func NewPersistentMockCatalog(ctx context.Context, pool *pgxpool.Pool) (*CatalogPublisher, error) {
	p := NewMockCatalog()
	transport := p.Executor.client.Transport.(*catalogMockTransport)
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT payload FROM catalog_editor_mock_state WHERE key='vendor-v1'),'{}'::jsonb)`).Scan(&raw); err != nil {
		return nil, err
	}
	var saved struct {
		Cards  map[string]mockCard `json:"cards"`
		Serial int64               `json:"serial"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	if saved.Cards != nil {
		transport.cards = saved.Cards
	}
	transport.serial = saved.Serial
	transport.pool = pool
	if err := transport.seedExistingCatalogue(ctx, pool); err != nil {
		return nil, err
	}
	return p, nil
}

// Seed the emulator's existing cards from the local staging catalogue. This is
// a labelled mock view of the mirror, never a read or write in a seller account.
func (m *catalogMockTransport) seedExistingCatalogue(ctx context.Context, pool *pgxpool.Pool) error {
	rows, e := pool.Query(ctx, `SELECT d.variant_id,d.internal_sku,d.wb_nm_ids,d.wb_articles,d.ozon_articles,p.name,p.description,v.base_price_minor::float8/100,COALESCE(shipping_package_dimension(v.id,'package_length_cm'),0),COALESCE(shipping_package_dimension(v.id,'package_width_cm'),0),COALESCE(shipping_package_dimension(v.id,'package_height_cm'),0),COALESCE(variant_numeric_attribute(v.id,'package_weight_grams'),0),ARRAY(SELECT COALESCE(mm.large_url,pm.object_key) FROM product_media pm LEFT JOIN media_mirror mm ON mm.source_url=pm.object_key WHERE pm.product_id=p.id AND (pm.variant_id IS NULL OR pm.variant_id=v.id) ORDER BY pm.is_primary DESC,pm.sort_order) FROM canonical_product_directory d JOIN products p ON p.id=d.product_id JOIN product_variants v ON v.id=d.variant_id WHERE cardinality(d.wb_nm_ids)>0 OR cardinality(d.wb_articles)>0 OR cardinality(d.ozon_articles)>0`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var variant int64
		var sku, name, description string
		var wbIDs, wbArticles, ozon, images []string
		var price, length, width, height, weight float64
		if e = rows.Scan(&variant, &sku, &wbIDs, &wbArticles, &ozon, &name, &description, &price, &length, &width, &height, &weight, &images); e != nil {
			return e
		}
		barcode := "LOCAL-MIRROR-" + sku
		for _, offer := range ozon {
			key := "ozon:" + offer
			if _, exists := m.cards[key]; exists {
				continue
			}
			m.cards[key] = mockCard{ID: 970000000 + variant, Offer: offer, Barcode: barcode, Price: price, Images: images, Document: map[string]any{"offer_id": offer, "description_category_id": 900001, "type_id": 900002, "name": name, "depth": length * 10, "width": width * 10, "height": height * 10, "dimension_unit": "mm", "weight": weight, "weight_unit": "g", "attributes": []any{map[string]any{"id": 4191, "values": []any{map[string]any{"value": description}}}}}}
		}
		if len(wbIDs) == 0 {
			for i := range wbArticles {
				wbIDs = append(wbIDs, strconv.FormatInt(9600000000000+variant*100+int64(i), 10))
			}
		}
		for i, id := range wbIDs {
			remote, _ := strconv.ParseInt(id, 10, 64)
			if remote <= 0 {
				continue
			}
			article := sku
			if i < len(wbArticles) {
				article = wbArticles[i]
			}
			key := "wb:" + article
			if _, exists := m.cards[key]; exists {
				continue
			}
			m.cards[key] = mockCard{ID: remote, Offer: article, Barcode: barcode, Price: price, Images: images, Document: map[string]any{"vendorCode": article, "subjectID": 900003, "title": name, "description": description, "dimensions": map[string]any{"length": length, "width": width, "height": height, "weightBrutto": weight / 1000}, "characteristics": []any{}, "sizes": []any{map[string]any{"techSize": "0", "skus": []string{barcode}}}}}
		}
	}
	return rows.Err()
}
