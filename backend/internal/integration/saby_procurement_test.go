package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

func TestSabyResponseLongerThanSixtyFourKilobytesIsParsedWhole(t *testing.T) {
	const rows = 5000
	var body strings.Builder
	body.WriteString(`{"jsonrpc":"2.0","id":1,"result":[`)
	for index := 0; index < rows; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"id":`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`,"name":"Орхидея Фаленопсис D12"}`)
	}
	body.WriteString(`]}`)
	if body.Len() <= 64<<10 {
		t.Fatalf("тестовый ответ = %d байт, ожидалось больше старого лимита", body.Len())
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(response, body.String())
	}))
	defer server.Close()
	client := NewSabyClient("client", "secret", "key", 278, 6)
	client.client = server.Client()
	var decoded struct {
		Result []struct {
			ID int `json:"id"`
		} `json:"result"`
	}
	if _, err := client.requestJSON(context.Background(), http.MethodPost, server.URL, map[string]any{"probe": true}, "token", &decoded); err != nil {
		t.Fatalf("разобрать большой ответ Saby: %v", err)
	}
	if len(decoded.Result) != rows {
		t.Fatalf("строк = %d, ожидалось %d", len(decoded.Result), rows)
	}
}

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

func TestSabyCatalogPageAcceptsObjectWrappedRows(t *testing.T) {
	t.Parallel()
	var page sabyCatalogPage
	if err := json.Unmarshal([]byte(`{"nomenclatures":{"items":[{"id":42,"code":"X42","name":"Олива европейская D12"}]}}`), &page); err != nil {
		t.Fatal(err)
	}
	rows := page.rows()
	if len(rows) != 1 || sabyValue(rows[0]["code"]) != "X42" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSabyCatalogPageAcceptsSingleObjectRow(t *testing.T) {
	t.Parallel()
	var page sabyCatalogPage
	if err := json.Unmarshal([]byte(`{"nomenclatures":{"id":42,"code":"X42","name":"Олива европейская D12"}}`), &page); err != nil {
		t.Fatal(err)
	}
	if rows := page.rows(); len(rows) != 1 || sabyValue(rows[0]["id"]) != "42" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSabyDraftsAreWrittenButNeverPosted(t *testing.T) {
	const receiptGUID = "11111111-1111-4111-8111-111111111111"
	var mutex sync.Mutex
	methods := make([]string, 0)
	added := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		var rpc struct {
			Method   string         `json:"method"`
			Protocol int            `json:"protocol"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil { t.Fatal(err) }
		if rpc.Protocol != 6 { t.Fatalf("protocol = %d", rpc.Protocol) }
		mutex.Lock()
		methods = append(methods, rpc.Method)
		mutex.Unlock()
		switch rpc.Method {
		case "РеалВх.Список":
			filter := rpc.Params["Фильтр"].(map[string]any)
			if sabyRecordField(filter, "ТипДокумента") != float64(224) || sabyRecordField(filter, "ИдентификаторДокумента") != receiptGUID { t.Fatalf("filter: %+v", filter) }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"recordset","d":[[7,%q]],"s":[{"n":"@Документ","t":"Число целое"},{"n":"ИдентификаторДокумента","t":"UUID"}]}}`, receiptGUID)
		case "РеалВх.NomCreateWithSaveBatch":
			recordSet := rpc.Params["rs"].(map[string]any)
			rows := recordSet["d"].([]any)
			if len(rows) != 1 || rows[0].([]any)[0] != float64(42) || rows[0].([]any)[2] != float64(2) { t.Fatalf("rows: %+v", rows) }
			actions := rpc.Params["actions"].(map[string]any)
			if sabyRecordField(actions, "changed_document") != true { t.Fatalf("actions: %+v", actions) }
			added = true
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"created":true}]}`))
		case "ДокОтгрВх.Прочитать":
			if rpc.Params["ИдО"] != float64(7) { t.Fatalf("ИдО must be Int64, got %#v", rpc.Params["ИдО"]) }
			rows := "[]"
			if added { rows = "[[42,2]]" }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"record","d":[7,%q,{"_type":"recordset","d":%s,"s":[{"n":"Номенклатура","t":"Число целое"},{"n":"Количество","t":"Число вещественное"}]}],"s":[{"n":"@Документ","t":"Число целое"},{"n":"ИдентификаторДокумента","t":"UUID"},{"n":"Строки","t":"Выборка"}]}}`, receiptGUID, rows)
		case "sabyWarehouse.List":
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"code":"3","name":"ул. Новоселов, д. 40а","address":"ул. Новоселов, д. 40а","organisation":{"inn":"620000000001","name":"Павловский Александр Владимирович"}}]}`))
		case "СБИС.ЗаписатьДокумент":
			document := rpc.Params["Документ"].(map[string]any)
			if document["Идентификатор"] != nil { t.Fatalf("public API must create one supplier header: %+v", document) }
			if document["Номер"] != "323" || document["Направление"] != "Входящий" { t.Fatalf("formal receipt fields: %+v", document) }
			counterparty := document["Контрагент"].(map[string]any)["СвЮЛ"].(map[string]any)
			if counterparty["ИНН"] != "7627031650" || counterparty["КПП"] != "762701001" { t.Fatalf("counterparty: %+v", counterparty) }
			if nestedString(document["Склад"], "Название") != "ул. Новоселов, д. 40а" { t.Fatalf("warehouse: %+v", document["Склад"]) }
			regulation := document["Регламент"].(map[string]any)
			if regulation["Название"] != "Поступление" { t.Fatalf("regulation: %+v", regulation) }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"Идентификатор":%q,"СсылкаДляНашаОрганизация":"https://ret.saby.ru/opendoc.html?guid=%s"}}`, receiptGUID, receiptGUID)
		default:
			t.Fatalf("unsafe Saby method: %s", rpc.Method)
		}
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
	want := []string{"sabyWarehouse.List", "СБИС.ЗаписатьДокумент", "РеалВх.Список", "ДокОтгрВх.Прочитать", "РеалВх.NomCreateWithSaveBatch", "ДокОтгрВх.Прочитать"}
	if fmt.Sprint(methods) != fmt.Sprint(want) { t.Fatalf("methods: %+v, want %+v", methods, want) }
}

func TestSabyReceiptRetryUsesStableExternalID(t *testing.T) {
	const receiptGUID = "22222222-2222-4222-8222-222222222222"
	var writes, adds, saved int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		var rpc struct { Method string `json:"method"`; Params map[string]any `json:"params"` }
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil { t.Fatal(err) }
		switch rpc.Method {
		case "РеалВх.Список":
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"recordset","d":[[8,%q]],"s":[{"n":"@Документ"},{"n":"ИдентификаторДокумента"}]}}`, receiptGUID)
		case "РеалВх.NomCreateWithSaveBatch":
			adds++
			recordSet := rpc.Params["rs"].(map[string]any)
			row := recordSet["d"].([]any)[0].([]any)
			if adds == 1 {
				_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"temporary add error"}}`))
				return
			}
			if row[2] != float64(2) { t.Fatalf("retry must repeat the whole failed atomic batch, row=%+v", row) }
			saved = 2
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"created":true}]}`))
		case "sabyWarehouse.List":
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"code":"3","name":"ул. Новоселов, д. 40а","organisation":{"inn":"620000000001","name":"ИП Павловский"}}]}`))
		case "СБИС.ЗаписатьДокумент":
			writes++
			document := rpc.Params["Документ"].(map[string]any)
			if document["Идентификатор"] != nil { t.Fatalf("retry header must be created once: %+v", document) }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"Идентификатор":%q,"СсылкаДляНашаОрганизация":"https://ret.saby.ru/doc/%s"}}`, receiptGUID, receiptGUID)
		case "ДокОтгрВх.Прочитать":
			if rpc.Params["ИдО"] != float64(8) { t.Fatalf("ИдО must remain numeric: %#v", rpc.Params["ИдО"]) }
			rows := "[]"
			if saved > 0 { rows = fmt.Sprintf("[[42,%d]]", saved) }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"record","d":[8,%q,{"_type":"recordset","d":%s,"s":[{"n":"Номенклатура"},{"n":"Количество"}]}],"s":[{"n":"@Документ"},{"n":"ИдентификаторДокумента"},{"n":"Строки"}]}}`, receiptGUID, rows)
		default:
			t.Fatalf("unexpected method: %s", rpc.Method)
		}
	}))
	defer server.Close()
	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.serviceURL, client.client = server.URL+"/oauth/service/", server.URL+"/service/?srv=1", server.Client()
	payload := json.RawMessage(`{"orderId":323,"supplier":{"name":"ТК Ярославский","taxId":"7627031650","kpp":"762701001"},"lines":[{"sabyId":"42","name":"Орхидея D12","quantity":2}]}`)
	first, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", Payload: payload})
	if err == nil || first.ExternalOperationID != "8" { t.Fatalf("first: %+v, err=%v", first, err) }
	second, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", Payload: payload, ExternalOperationID: first.ExternalOperationID, ExternalURL: first.ExternalURL})
	if err != nil || !second.Completed { t.Fatalf("second: %+v, err=%v", second, err) }
	if writes != 1 || adds != 2 { t.Fatalf("writes=%d adds=%d", writes, adds) }
}

func TestSabyReceiptRetryUsesHeaderDocumentFromURL(t *testing.T) {
	const targetGUID = "33333333-3333-4333-8333-333333333333"
	var adds, writes int
	saved := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		var rpc struct { Method string `json:"method"`; Params map[string]any `json:"params"` }
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil { t.Fatal(err) }
		switch rpc.Method {
		case "РеалВх.Список":
			filter := rpc.Params["Фильтр"].(map[string]any)
			if sabyRecordField(filter, "ИдентификаторДокумента") != targetGUID { t.Fatalf("wrong target filter: %+v", filter) }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"recordset","d":[[777,%q]],"s":[{"n":"@Документ"},{"n":"ИдентификаторДокумента"}]}}`, targetGUID)
		case "ДокОтгрВх.Прочитать":
			rows := "[]"
			if saved { rows = `[[{"object":"opaque"},{"value":"20"}]]` }
			_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"record","d":[777,%q,{"_type":"recordset","d":%s,"s":[{"n":"Номенклатура"},{"n":"Количество"}]}],"s":[{"n":"@Документ"},{"n":"ИдентификаторДокумента"},{"n":"Строки"}]}}`, targetGUID, rows)
		case "РеалВх.NomCreateWithSaveBatch":
			adds++
			saved = true
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"created":true}]}`))
		case "СБИС.ЗаписатьДокумент":
			writes++
			t.Fatal("retry must reuse the supplier header from the URL")
		default:
			t.Fatalf("unexpected method: %s", rpc.Method)
		}
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.serviceURL, client.client = server.URL+"/oauth/service/", server.URL+"/service/?srv=1", server.Client()
	payload := json.RawMessage(`{"orderId":323,"supplier":{"name":"ТК Ярославский","taxId":"7627031650","kpp":"762701001"},"lines":[{"sabyId":"631","name":"Орхидея D12","quantity":20}]}`)
	result, err := client.CreateDraft(context.Background(), procurement.ActionItem{
		Channel: "saby_receipt", Payload: payload, ExternalOperationID: "631",
		ExternalURL: "https://ret.saby.ru/doc/" + targetGUID,
	})
	if err != nil || !result.Completed { t.Fatalf("result=%+v, err=%v", result, err) }
	if result.ExternalOperationID != "777" || adds != 1 || writes != 0 { t.Fatalf("result=%+v adds=%d writes=%d", result, adds, writes) }
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

func TestSabyLineQuantitiesReadsNestedRelationCells(t *testing.T) {
	t.Parallel()
	result := map[string]any{
		"_type": "recordset",
		"s": []any{
			map[string]any{"n": "Номенклатура"},
			map[string]any{"n": "Количество"},
		},
		"d": []any{
			[]any{
				map[string]any{
					"_type": "record",
					"s": []any{map[string]any{"n": "@Номенклатура"}},
					"d": []any{float64(3604)},
				},
				map[string]any{"Количество": "3,0"},
			},
		},
	}
	quantities := sabyLineQuantities(result)
	if quantities[3604] != 3 {
		t.Fatalf("quantities = %+v, want item 3604 quantity 3", quantities)
	}
}

func TestSabyLineCountIgnoresRelationCellShape(t *testing.T) {
	t.Parallel()
	result := map[string]any{
		"_type": "recordset",
		"s": []any{map[string]any{"n": "Номенклатура"}, map[string]any{"n": "Количество"}},
		"d": []any{
			[]any{map[string]any{"object": "opaque"}, map[string]any{"value": "20"}},
			[]any{map[string]any{"different": []any{3604}}, float64(3)},
			[]any{"X2542", nil},
		},
	}
	if count := sabyLineCount(result); count != 3 {
		t.Fatalf("line count = %d, want 3", count)
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
		_, _ = response.Write([]byte(`{"nomenclatures":[{"id":42,"name":"Каноническое имя","balance":"0","cost":1590,"code":"X42"}]}`))
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
	if items[0].Name != "Каноническое имя" || items[0].Balance != "5" || items[0].Cost != float64(1590) {
		t.Fatalf("unexpected merged item: %+v", items[0])
	}
}

func TestSabyBoolAcceptsStringFolderFlag(t *testing.T) {
	t.Parallel()
	if !sabyBool("true") || !sabyBool(true) || sabyBool("false") || sabyBool(nil) {
		t.Fatal("Saby folder flag must accept both JSON booleans and their string form")
	}
}
