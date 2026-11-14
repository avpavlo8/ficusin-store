package integration

// Flowwow's official Seller API: https://seller-docs.flowwow.com/5.-instrumenty-prodavca/5.1-dokumentaciya-i-podderzhka-po-api/otkrytoe-api-dlya-prodavcov-0.0.1
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

type FlowwowClient struct {
	token, shopID string
	client        *http.Client
	mu            sync.Mutex
	retryAt       time.Time
}

func NewFlowwowClient(token, shopID string) *FlowwowClient {
	return &FlowwowClient{token: strings.TrimSpace(token), shopID: strings.TrimSpace(shopID), client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *FlowwowClient) Configured() bool {
	id, e := strconv.ParseUint(c.shopID, 10, 32)
	return c.token != "" && e == nil && id > 0
}
func (c *FlowwowClient) ShopID() string { return c.shopID }
func (c *FlowwowClient) call(ctx context.Context, method, path string, body any, out any) error {
	if !c.Configured() {
		return errors.New("Flowwow: задайте FLOWWOW_API_TOKEN и FLOWWOW_SHOP_ID на сервере")
	}
	c.mu.Lock()
	cooling := time.Now().Before(c.retryAt)
	c.mu.Unlock()
	if cooling {
		return &FlowwowError{Status: 429}
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, method, "https://apis.flowwow.com/apiseller/"+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	q := req.URL.Query()
	q.Set("shopId", c.shopID)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.client.Do(req)
	if e != nil {
		return errors.New("Flowwow: не получен ответ API; проверьте результат операции")
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if e != nil {
		return errors.New("Flowwow: ответ не прочитан")
	}
	if resp.StatusCode == 429 {
		delay := time.Minute
		if seconds, e := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64); e == nil && seconds >= 0 && seconds < 86400 {
			delay = time.Duration(seconds) * time.Second
		} else if until, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			delay = time.Until(until)
		}
		c.mu.Lock()
		until := time.Now().Add(delay)
		if until.After(c.retryAt) {
			c.retryAt = until
		}
		c.mu.Unlock()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &FlowwowError{Status: resp.StatusCode}
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("Flowwow: некорректный ответ API")
	}
	return nil
}

type FlowwowError struct{ Status int }

func (e *FlowwowError) Error() string {
	return fmt.Sprintf("Flowwow: API вернул HTTP %d", e.Status)
}
func (e *FlowwowError) HTTPStatus() int { return e.Status }

type FlowwowProduct struct {
	MinOrder       int            `json:"minOrder"`
	VAT            string         `json:"vat"`
	IsPostDelivery bool           `json:"isDeliveryPost"`
	ProductID      int64          `json:"productId"`
	OfferID        string         `json:"offerId"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Price          string         `json:"price"`
	Discount       string         `json:"discount"`
	Type           int            `json:"type"`
	SubCategoryID  int64          `json:"subCategoryId"`
	Stock          int            `json:"stock"`
	Images         []string       `json:"images"`
	Properties     map[string]any `json:"properties"`
	Currency       string         `json:"currencyCode"`
}

func (c *FlowwowClient) Products(ctx context.Context, filter map[string]any) ([]FlowwowProduct, int, error) {
	var out struct {
		Items []FlowwowProduct `json:"items"`
		Total int              `json:"total"`
	}
	e := c.call(ctx, "POST", "products", filter, &out)
	return out.Items, out.Total, e
}
func (c *FlowwowClient) find(ctx context.Context, offer, product string) (*FlowwowProduct, error) {
	filter := map[string]any{"extended": true, "withArchive": true, "limit": 2}
	if product != "" {
		id, e := strconv.ParseInt(product, 10, 64)
		if e != nil || id <= 0 {
			return nil, errors.New("Flowwow: укажите числовой ID карточки")
		}
		filter["productIds"] = []int64{id}
	} else {
		filter["offerIds"] = []string{offer}
	}
	items, _, e := c.Products(ctx, filter)
	if e != nil {
		return nil, e
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) != 1 {
		return nil, errors.New("Flowwow: найдено несколько карточек, проверьте связь")
	}
	p := items[0]
	if product != "" && fmt.Sprint(p.ProductID) != product || product == "" && p.OfferID != offer {
		return nil, errors.New("Flowwow: ответ не соответствует запрошенному товару")
	}
	return &p, nil
}
func (c *FlowwowClient) Reference(ctx context.Context, kind, category string) (json.RawMessage, error) {
	path := "categories/tree"
	if kind == "attributes" {
		id, e := strconv.ParseInt(category, 10, 64)
		if e != nil || id <= 0 {
			return nil, errors.New("Flowwow: выберите подкатегорию")
		}
		path = "categories/subCategoryProperties?subCategoryId=" + url.QueryEscape(category) + "&productType=1"
	} else if kind != "categories" {
		return nil, errors.New("Flowwow: неизвестный справочник")
	}
	var raw json.RawMessage
	e := c.call(ctx, "POST", path, map[string]any{}, &raw)
	return raw, e
}

type flowwowBatch struct {
	Data []struct {
		ProductID int64  `json:"productId"`
		OfferID   string `json:"offerId"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *FlowwowClient) batch(ctx context.Context, method, path, key string, item map[string]any) (flowwowBatch, error) {
	var b flowwowBatch
	e := c.call(ctx, method, path, map[string]any{key: []any{item}}, &b)
	if e != nil {
		return b, e
	}
	if len(b.Errors) > 0 {
		return b, &FlowwowValidationError{Message: "Flowwow: товар не принят — " + strings.ReplaceAll(b.Errors[0].Message, c.token, "[скрыто]")}
	}
	if len(b.Data) != 1 || b.Data[0].OfferID != item["offerId"] || b.Data[0].ProductID <= 0 {
		return b, errors.New("Flowwow: API не подтвердил обработку товара")
	}
	return b, nil
}

// Advance uses the existing durable publication lease. The create boundary is
// persisted by the worker BEFORE POST; an unknown response is only reconciled.
func (c *FlowwowClient) Advance(ctx context.Context, channel string, r ce.Request, p ce.Progress) (ce.Result, error) {
	result := func(stage, status, message string) ce.Result {
		p.Stage = stage
		return ce.Result{Progress: p, Status: status, Message: message}
	}
	if channel != "flowwow" {
		return ce.Result{}, errors.New("Flowwow: неверный канал")
	}
	if !c.Configured() {
		return result(p.Stage, "failed", "Flowwow API не подключён"), nil
	}
	offer := strings.TrimSpace(r.SKU)
	if r.Channel.Article != nil {
		offer = strings.TrimSpace(*r.Channel.Article)
	}
	if p.IDs.OfferID != "" {
		offer = p.IDs.OfferID
	}
	if offer == "" || len(offer) > 50 {
		return ce.Result{}, errors.New("Flowwow: артикул должен содержать от 1 до 50 символов")
	}
	switch p.Stage {
	case "":
		productID := ""
		if r.Action == "link" {
			productID = r.Channel.ExistingID
		}
		remote, e := c.find(ctx, offer, productID)
		if e != nil {
			return ce.Result{}, e
		}
		if remote != nil {
			if remote.Type != 1 {
				return result("", "failed", "Через API можно связать только обычный товар Flowwow"), nil
			}
			if remote.OfferID == "" { // mapping is a separate durable, repeatable step.
				p.IDs = ce.RemoteIDs{ProductID: fmt.Sprint(remote.ProductID), OfferID: offer}
				return result("mapping", "queued", "Назначаем артикул существующей карточке"), nil
			}
			if r.Action == "link" && remote.OfferID != offer {
				return result("", "failed", "У карточки Flowwow другой артикул. Укажите его, чтобы сохранить существующую связь"), nil
			}
			p.IDs = ce.RemoteIDs{ProductID: fmt.Sprint(remote.ProductID), OfferID: remote.OfferID}
			return result("update", "queued", "Карточка найдена, готовим обновление с сайта"), nil
		}
		if r.Action == "update" || r.Action == "link" {
			return result("", "failed", "Связанная карточка Flowwow не найдена. Новая автоматически не создавалась"), nil
		}
		item, e := c.productPayload(ctx, r, offer, nil)
		if e != nil {
			return ce.Result{}, e
		}
		p.Native, _ = json.Marshal(item)
		return result("create", "queued", "Готовим создание карточки Flowwow"), nil
	case "mapping":
		_, e := c.batch(ctx, "POST", "products/offersMappings", "offers", map[string]any{"offerId": offer, "productId": mustFlowwowID(p.IDs.ProductID)})
		if e != nil {
			return ce.Result{}, e
		}
		return result("update", "queued", "Артикул назначен, готовим обновление"), nil
	case "reconcile":
		remote, e := c.find(ctx, offer, "")
		if e != nil {
			return ce.Result{}, e
		}
		if remote == nil {
			return result("reconcile", "needs_attention", "Ответ на создание не получен, карточка пока не найдена. Повторное создание остановлено; проверьте кабинет Flowwow"), nil
		}
		p.IDs = ce.RemoteIDs{ProductID: fmt.Sprint(remote.ProductID), OfferID: remote.OfferID}
		return result("update", "queued", "Карточка после создания найдена"), nil
	case "create", "update":
		var remote *FlowwowProduct
		if p.Stage == "update" {
			var e error
			remote, e = c.find(ctx, offer, "")
			if e != nil {
				return ce.Result{}, e
			}
			if remote == nil || fmt.Sprint(remote.ProductID) != p.IDs.ProductID {
				return result("update", "needs_attention", "Связь карточки Flowwow изменилась; проверьте её перед обновлением"), nil
			}
		}
		var item map[string]any
		var e error
		if p.Stage == "create" {
			if json.Unmarshal(p.Native, &item) != nil || item["offerId"] != offer {
				return result("create", "needs_attention", "Не сохранён запрос создания Flowwow; повторная отправка остановлена"), nil
			}
		} else {
			item, e = c.productPayload(ctx, r, offer, remote)
			if e != nil {
				return ce.Result{}, e
			}
		}
		path := "products/" + p.Stage
		b, e := c.batch(ctx, "POST", path, "products", item)
		if e != nil {
			var rejected *FlowwowValidationError
			var httpErr *FlowwowError
			if p.Stage == "create" && errors.As(e, &httpErr) && httpErr.Status == 429 {
				p.CreateAttempted = false
				return result("create", "queued", e.Error()), nil
			}
			if p.Stage == "create" && (errors.As(e, &rejected) || errors.As(e, &httpErr) && (httpErr.Status == 400 || httpErr.Status == 401 || httpErr.Status == 403 || httpErr.Status == 422)) {
				p.CreateAttempted = false
				p.Native = nil
				return result("", "failed", e.Error()), nil
			}
			return ce.Result{Progress: p}, e
		}
		p.IDs = ce.RemoteIDs{ProductID: fmt.Sprint(b.Data[0].ProductID), OfferID: offer}
		p.Checks = 0
		return result("verify", "queued", "Flowwow принял данные; проверяем карточку"), nil
	case "verify":
		remote, e := c.find(ctx, offer, "")
		if e != nil {
			return ce.Result{}, e
		}
		p.Checks++
		if remote == nil || fmt.Sprint(remote.ProductID) != p.IDs.ProductID || remote.Name != r.Name || remote.Description != r.Description || !flowwowPriceEqual(remote.Price, r.PriceMinor) || remote.Discount != "0" || len(remote.Images) != len(r.Images) {
			if p.Checks >= 100 {
				return result("verify", "needs_attention", "Flowwow пока не подтвердил все поля и фотографии; проверьте карточку"), nil
			}
			return result("verify", "queued", "Ожидаем обновления данных и обработки фото Flowwow (до 30 минут)"), nil
		}
		return result("done", "synced", "Название, описание, цена и фотографии отправлены в Flowwow"), nil
	case "done":
		return result("done", "synced", "Данные Flowwow обновлены"), nil
	}
	return result(p.Stage, "needs_attention", "Неизвестный этап обмена Flowwow"), nil
}
func mustFlowwowID(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
func flowwowPriceEqual(s string, minor int64) bool {
	v, e := strconv.ParseFloat(s, 64)
	return e == nil && v == float64(minor)/100
}
func (c *FlowwowClient) productPayload(ctx context.Context, r ce.Request, offer string, remote *FlowwowProduct) (map[string]any, error) {
	category := r.Channel.Category.ID
	if remote != nil {
		if remote.Type != 1 {
			return nil, errors.New("Flowwow: готовые и дополнительные товары не поддерживаются этим каналом")
		}
		category = fmt.Sprint(remote.SubCategoryID)
		if r.Channel.Category.ID != "" && r.Channel.Category.ID != category {
			return nil, errors.New("Flowwow: API не позволяет менять подкатегорию существующей карточки")
		}
	}
	raw, e := c.Reference(ctx, "attributes", category)
	if e != nil {
		return nil, e
	}
	var schema struct {
		Data []FlowwowField `json:"data"`
	}
	if json.Unmarshal(raw, &schema) != nil || len(schema.Data) == 0 {
		return nil, errors.New("Flowwow: не получены правила подкатегории")
	}
	item := map[string]any{}
	// Only fields allowed by the current category are retained from the remote card.
	for _, f := range schema.Data {
		if remote != nil && remote.Properties[f.Field] != nil {
			item[f.Field] = remote.Properties[f.Field]
		}
		if v, ok := r.Channel.Attributes[f.Field]; ok {
			item[f.Field] = v
		}
	}
	if remote != nil && remote.Stock > 0 && item["stock"] == nil {
		item["stock"] = remote.Stock
	}
	if remote != nil {
		if remote.MinOrder > 0 {
			item["minOrderCount"] = remote.MinOrder
		}
		if remote.VAT != "" {
			item["vat"] = remote.VAT
		}
		item["isPostDelivery"] = remote.IsPostDelivery
	}
	item["offerId"], item["productType"], item["subCategoryId"] = offer, 1, mustFlowwowID(category)
	item["name"], item["description"], item["price"], item["discount"], item["images"] = r.Name, r.Description, fmt.Sprintf("%d.%02d", r.PriceMinor/100, r.PriceMinor%100), "0", r.Images
	if r.PriceMinor <= 0 || strings.TrimSpace(r.Name) == "" || len([]rune(r.Name)) > 255 || len([]rune(r.Description)) > 5000 || len(r.Images) == 0 {
		return nil, errors.New("Flowwow: нужны название до 255 символов, описание до 5000, положительная цена и фото")
	}
	for _, image := range r.Images {
		if !publicFlowwowImage(image) {
			return nil, errors.New("Flowwow: фотографии должны иметь публичные HTTPS-ссылки")
		}
	}
	if e = ValidateFlowwowFields(schema.Data, item); e != nil {
		return nil, e
	}
	return item, nil
}

type FlowwowField struct {
	Name      string   `json:"name"`
	Field     string   `json:"field"`
	Type      string   `json:"type"`
	Required  bool     `json:"required"`
	DependsOn []string `json:"dependsOn"`
	Min       *float64 `json:"minValue"`
	Max       *float64 `json:"maxValue"`
	Decimals  *int     `json:"maxDecimalPlaces"`
	Values    []struct {
		ID   any    `json:"id"`
		Name string `json:"name"`
	} `json:"values"`
	Measure []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"measure"`
}

func (c *FlowwowClient) SyncInventory(ctx context.Context, offer string, price int64, stock int) error {
	if price <= 0 || stock < 0 {
		return errors.New("Flowwow: неверные цена или остаток")
	}
	if _, e := c.batch(ctx, "PUT", "prices/put", "offers", map[string]any{"offerId": offer, "price": fmt.Sprintf("%d.%02d", price/100, price%100), "discount": "0"}); e != nil {
		return e
	}
	if _, e := c.batch(ctx, "PUT", "stocks/put", "offers", map[string]any{"offerId": offer, "stock": stock}); e != nil {
		return e
	}
	remote, e := c.find(ctx, offer, "")
	if e != nil {
		return e
	}
	if remote == nil || !flowwowPriceEqual(remote.Price, price) || remote.Discount != "0" || remote.Stock != stock {
		return errors.New("Flowwow: цена или остаток пока не подтверждены чтением карточки")
	}
	return nil
}

type FlowwowValidationError struct{ Message string }

func (e *FlowwowValidationError) Error() string { return e.Message }
