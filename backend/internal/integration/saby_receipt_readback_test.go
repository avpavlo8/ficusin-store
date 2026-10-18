package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

func TestSabyExistingReceiptReadOnlyVerification(t *testing.T) {
	const guid = "11111111-1111-4111-8111-111111111111"
	for _, test := range []struct {
		name                                     string
		posted, postingError, deleted, wrongGUID bool
		quantity                                 int
		wantDone, wantError                      bool
	}{
		{name: "posted header and separately read rows", posted: true, quantity: 2, wantDone: true},
		{name: "operator has not posted yet", quantity: 2},
		{name: "posting errors must not close", posted: true, postingError: true, quantity: 2},
		{name: "quantity differs", posted: true, quantity: 1, wantError: true},
		{name: "deleted document", posted: true, deleted: true, quantity: 2, wantError: true},
		{name: "another document GUID", posted: true, wrongGUID: true, quantity: 2, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads, writes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/oauth/service/" {
					fmt.Fprint(w, `{"token":"test-token"}`)
					return
				}
				var rpc struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
					t.Error(err)
					return
				}
				var result any
				switch rpc.Method {
				case "ДокОтгрВх.Прочитать":
					reads++
					flags := make([]any, 19)
					flags[10], flags[14] = test.posted, test.postingError
					result = map[string]any{"_type": "record", "d": []any{int64(77), guid, test.deleted, flags}, "s": []any{
						map[string]any{"n": "@Документ"}, map[string]any{"n": "ИдентификаторДокумента"}, map[string]any{"n": "Удален"},
						map[string]any{"n": "ДокументРасширение.Состояние", "t": map[string]any{"n": "Флаги", "s": map[string]any{"10": "Проведен", "14": "ОшибкиПроведения", "18": "ВОчередиНаПроведение"}}},
					}}
				case "РеалВх.DocNomList":
					reads++
					filter, _ := rpc.Params["Фильтр"].(map[string]any)
					if id, err := sabyInt64Field(filter, "СкладскойДокумент"); err != nil || id != 77 {
						t.Errorf("unbound receipt rows filter: %v", filter)
					}
					result = map[string]any{"_type": "recordset", "d": []any{[]any{700, test.quantity, 42, 77}}, "s": []any{
						map[string]any{"n": "@НоменклатураДокумента"}, map[string]any{"n": "Количество"}, map[string]any{"n": "Номенклатура"}, map[string]any{"n": "СкладскойДокумент"},
					}}
				default:
					writes++
					t.Errorf("forbidden RPC in read-only recovery: %s", rpc.Method)
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
			}))
			defer server.Close()
			client := NewSabyClient("client", "secret", "key", 278, 6)
			client.authURL, client.serviceURL, client.client = server.URL+"/oauth/service/", server.URL+"/service/", server.Client()
			link := "https://ret.saby.ru/doc/" + guid
			if test.wrongGUID {
				link = "https://ret.saby.ru/doc/22222222-2222-4222-8222-222222222222"
			}
			item := procurement.ActionItem{Channel: "saby_receipt", ExternalOperationID: "77", ExternalURL: link, Payload: json.RawMessage(`{"orderId":19,"verifyOnly":true,"lines":[{"sabyId":"42","quantity":2}]}`)}
			result, err := client.CreateDraft(context.Background(), item)
			if (err != nil) != test.wantError || result.Completed != test.wantDone || writes != 0 {
				t.Fatalf("result=%+v err=%v reads=%d writes=%d", result, err, reads, writes)
			}
			if !test.wantDone && !test.wantError && result.RetryAfter != 30*time.Second {
				t.Fatalf("missing durable polling: %+v", result)
			}
		})
	}
}

func TestSabyReadOnlyReceiptWithoutDocumentCannotCreate(t *testing.T) {
	client := NewSabyClient("client", "secret", "key", 278, 6)
	for _, id := range []string{"", "not-an-id", "-1"} {
		_, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", ExternalOperationID: id, ExternalURL: "https://ret.saby.ru/doc/test", Payload: json.RawMessage(`{"orderId":19,"verifyOnly":true,"lines":[{"sabyId":"42","quantity":2}]}`)})
		if err == nil {
			t.Fatal("missing document accepted")
		}
	}
}
