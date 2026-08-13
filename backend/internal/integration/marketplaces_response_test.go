package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Страница продаж или карточек весит сотни килобайт. Прежний потолок чтения
// в 64 КБ обрывал её на полуслове, разбор падал с «unexpected end of JSON
// input», и со стороны это выглядело как молчание площадки — хотя площадка
// отвечала исправно. Тест держит потолок выше настоящей страницы.
func TestMarketplaceResponseLongerThanSixtyFourKilobytesIsParsedWhole(t *testing.T) {
	const items = 4000
	var body strings.Builder
	body.WriteString(`{"result":{"items":[`)
	for index := 0; index < items; index++ {
		if index > 0 {
			body.WriteString(",")
		}
		body.WriteString(`{"offer_id":"FIC-000000000000000000000000`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`"}`)
	}
	body.WriteString(`],"last_id":""}}`)
	if body.Len() <= 64<<10 {
		t.Fatalf("тело ответа = %d байт, тест должен быть длиннее прежнего потолка", body.Len())
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(body.String()))
	}))
	defer server.Close()

	executor := NewMarketplaceExecutor("", "client", "secret")
	executor.client = server.Client()
	var response struct {
		Result struct {
			Items []struct {
				OfferID string `json:"offer_id"`
			} `json:"items"`
		} `json:"result"`
	}
	if err := executor.request(context.Background(), http.MethodGet, server.URL, nil, nil, &response); err != nil {
		t.Fatalf("разобрать длинный ответ: %v", err)
	}
	if len(response.Result.Items) != items {
		t.Fatalf("позиций = %d, ожидалось %d", len(response.Result.Items), items)
	}
}
