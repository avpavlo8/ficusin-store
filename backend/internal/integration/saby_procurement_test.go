package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

func TestSabyProbeCachesServiceSession(t *testing.T) {
	var authCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			authCalls.Add(1)
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		if request.Header.Get("X-SBISAccessToken") != "safe-token" {
			t.Fatal("Saby access token is missing")
		}
		if request.URL.Query().Get("pointId") != "278" || request.URL.Query().Get("priceListId") != "6" || request.URL.Query().Get("pageSize") != "1" {
			t.Fatalf("unexpected safe probe query: %s", request.URL.RawQuery)
		}
		_, _ = response.Write([]byte(`{"nomenclatures":[]}`))
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	if err := client.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if authCalls.Load() != 1 {
		t.Fatalf("auth calls = %d, want 1", authCalls.Load())
	}
}

func TestSabyDraftsAreWrittenButNeverPosted(t *testing.T) {
	var mutex sync.Mutex
	methods := make([]string, 0)
	documents := make([]map[string]any, 0)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		var rpc struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil { t.Fatal(err) }
		mutex.Lock()
		methods = append(methods, rpc.Method)
		mutex.Unlock()
		if rpc.Method == "sabyWarehouse.List" {
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"code":"3","name":"ул. Новоселов, д. 40а","address":"ул. Новоселов, д. 40а","organisation":{"inn":"620000000001","name":"Павловский Александр Владимирович"}}]}`))
			return
		}
		if rpc.Method != "СБИС.ЗаписатьДокумент" { t.Fatalf("unsafe Saby method: %s", rpc.Method) }
		document, ok := rpc.Params["Документ"].(map[string]any)
		if !ok { t.Fatalf("document is missing: %+v", rpc.Params) }
		mutex.Lock()
		documents = append(documents, document)
		index := len(documents)
		mutex.Unlock()
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"Идентификатор":"draft-` + string(rune('0'+index)) + `","СсылкаДляНашаОрганизация":"https://online.sbis.ru/draft"}}`))
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.serviceURL, client.client = server.URL+"/oauth/service/", server.URL, server.URL+"/service/?srv=1", server.Client()
	receiptPayload := json.RawMessage(`{"orderId":323,"orderNumber":"323","supplier":{"name":"ТК Ярославский, ООО","taxId":"7627031650","kpp":"762701001"},"lines":[{"sabyId":"42","code":"X42","name":"Орхидея D12","quantity":2,"unitCost":125.5}]}`)
	result, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", Payload: receiptPayload})
	if err != nil { t.Fatal(err) }
	if !result.Completed || result.ExternalOperationID == "" || result.ExternalURL == "" { t.Fatalf("unexpected result: %+v", result) }
	pricePayload := json.RawMessage(`{"orderId":323,"lines":[{"sabyId":"42","code":"X42","name":"Орхидея D12","newPrice":2650}]}`)
	if _, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_price", Payload: pricePayload}); err == nil || !strings.Contains(err.Error(), "импорт XLSX") { t.Fatalf("price error = %v", err) }

	for _, method := range methods {
		if strings.Contains(method, "ПодготовитьДействие") || strings.Contains(method, "ВыполнитьДействие") {
			t.Fatalf("draft flow attempted to post a document: %s", method)
		}
	}
	if len(documents) != 1 { t.Fatalf("documents = %d, want 1", len(documents)) }
	if documents[0]["Тип"] != "ДокОтгрВх" { t.Fatalf("receipt type: %+v", documents[0]) }
	counterparty := documents[0]["Контрагент"].(map[string]any)["СвЮЛ"].(map[string]any)
	if counterparty["ИНН"] != "7627031650" || counterparty["КПП"] != "762701001" { t.Fatalf("counterparty: %+v", counterparty) }
	receiptLines := documents[0]["Наименования"].([]any)
	receiptLine := receiptLines[0].(map[string]any)
	if receiptLine["Количество"] != "2" || receiptLine["СуммаСебест"] != "251.00" || receiptLine["ТипНоменклатуры"].(map[string]any)["ВидУчета"] != "Товар" { t.Fatalf("receipt lines: %+v", receiptLines) }
}

func TestSabyErrorMessageUsesJSONRPCDetails(t *testing.T) {
	t.Parallel()
	content := []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"warning","details":"КПП должен быть заполнен"},"id":1}`)
	if got := sabyErrorMessage(content); got != "КПП должен быть заполнен" {
		t.Fatalf("message = %q", got)
	}
}

func TestSabyReceiptRequiresKPPForRussianCompany(t *testing.T) {
	t.Parallel()
	client := NewSabyClient("client", "secret", "service", 278, 6)
	payload := json.RawMessage(`{"orderId":323,"supplier":{"name":"ТК Ярославский","taxId":"7627031650"},"lines":[{"sabyId":"42","code":"X42","name":"Орхидея D12","quantity":2}]}`)
	_, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", Payload: payload})
	if err == nil || !strings.Contains(err.Error(), "КПП") {
		t.Fatalf("error = %v", err)
	}
}

func TestSabyFetchCatalogMergesCompleteCatalogueAndPriceList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		if request.URL.Query().Get("page") != "0" {
			_, _ = response.Write([]byte(`{"nomenclatures":[]}`))
			return
		}
		if request.URL.Query().Get("priceListId") == "6" {
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":42,"name":"Имя из прайса","balance":"5","cost":2990},{"id":99,"name":"Новый товар","balance":2,"cost":100}]}`))
			return
		}
		_, _ = response.Write([]byte(`{"nomenclatures":[{"id":42,"name":"Каноническое имя","balance":"0","code":"X42"}]}`))
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	items, err := client.FetchCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %+v", len(items), items)
	}
	if items[0].Name != "Каноническое имя" || items[0].Balance != "5" || items[0].Cost != float64(2990) {
		t.Fatalf("unexpected merged item: %+v", items[0])
	}
}
