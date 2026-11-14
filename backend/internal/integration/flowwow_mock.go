package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Isolated HTTP emulator: the transport never delegates to a network transport.
func NewMockFlowwowClient() *FlowwowClient {
	c := NewFlowwowClient("mock-flowwow", "1")
	c.client.Transport = &flowwowMockTransport{cards: map[string]FlowwowProduct{}}
	return c
}

type flowwowMockTransport struct {
	mu    sync.Mutex
	cards map[string]FlowwowProduct
	next  int64
}

func (m *flowwowMockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "apis.flowwow.com" {
		return nil, fmt.Errorf("mock Flowwow refuses host")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(raw, &body)
	var response any
	status := 200
	switch r.URL.Path {
	case "/apiseller/categories/tree":
		response = map[string]any{"shopId": 1, "data": []any{map[string]any{"categoryId": 1, "name": "Тест Flowwow: Растения", "subCategories": []any{map[string]any{"subCategoryId": 101, "name": "Горшечные"}}}}}
	case "/apiseller/categories/subCategoryProperties":
		response = map[string]any{"shopId": 1, "data": []any{map[string]any{"name": "Высота", "field": "height", "type": "measure", "required": true, "measure": []any{map[string]any{"id": 3}}}, map[string]any{"name": "Остаток", "field": "stock", "type": "decimal", "required": true, "minValue": 1}}}
	case "/apiseller/products":
		var offers []string
		var ids []int64
		_ = json.Unmarshal(body["offerIds"], &offers)
		_ = json.Unmarshal(body["productIds"], &ids)
		items := []FlowwowProduct{}
		for _, p := range m.cards {
			match := len(offers) == 0 && len(ids) == 0
			for _, o := range offers {
				match = match || o == p.OfferID
			}
			for _, id := range ids {
				match = match || id == p.ProductID
			}
			if match {
				items = append(items, p)
			}
		}
		response = map[string]any{"items": items, "total": len(items)}
	case "/apiseller/products/create", "/apiseller/products/update":
		var items []map[string]any
		_ = json.Unmarshal(body["products"], &items)
		data := []any{}
		errs := []any{}
		for _, item := range items {
			o, _ := item["offerId"].(string)
			p, exists := m.cards[o]
			if r.URL.Path == "/apiseller/products/create" && exists || r.URL.Path == "/apiseller/products/update" && !exists {
				errs = append(errs, map[string]any{"offerId": o, "message": "Тест: артикул уже существует либо не найден"})
				continue
			}
			if !exists {
				m.next++
				p.ProductID = 900000 + m.next
			}
			p.OfferID = o
			p.Name, _ = item["name"].(string)
			p.Description, _ = item["description"].(string)
			p.Price, _ = item["price"].(string)
			p.Discount = "0"
			p.Type = 1
			p.SubCategoryID = int64(item["subCategoryId"].(float64))
			p.Properties = item
			p.Images = []string{}
			for _, img := range item["images"].([]any) {
				p.Images = append(p.Images, img.(string))
			}
			if n, ok := item["stock"].(float64); ok {
				p.Stock = int(n)
			}
			p.Currency = "RUB"
			m.cards[o] = p
			data = append(data, map[string]any{"offerId": o, "productId": p.ProductID})
		}
		response = map[string]any{"shopId": 1, "data": data, "errors": errs}
	case "/apiseller/prices/put", "/apiseller/stocks/put":
		var offers []map[string]any
		_ = json.Unmarshal(body["offers"], &offers)
		data := []any{}
		errs := []any{}
		for _, item := range offers {
			o, _ := item["offerId"].(string)
			p, exists := m.cards[o]
			if !exists {
				errs = append(errs, map[string]any{"offerId": o, "message": "Карточка не найдена"})
				continue
			}
			if v, ok := item["price"].(string); ok {
				p.Price = v
				p.Discount = "0"
			}
			if v, ok := item["stock"].(float64); ok {
				p.Stock = int(v)
			}
			m.cards[o] = p
			data = append(data, map[string]any{"offerId": o, "productId": p.ProductID, "price": p.Price, "stock": p.Stock, "discount": "0", "updated": true})
		}
		response = map[string]any{"shopId": 1, "data": data, "errors": errs}
	default:
		status = 404
		response = map[string]any{"message": "Unsupported local Flowwow endpoint"}
	}
	b, _ := json.Marshal(response)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
}
