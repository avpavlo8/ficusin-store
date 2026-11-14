package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func orderRealizationPostFixture() map[string]any {
	record := orderRealizationReadFixture()
	sabyRecordField(record, "ДокументРасширение.Состояние").([]any)[10] = false
	sabyRecordField(record, "ДокументРасширение.Состояние").([]any)[11] = false
	for name, value := range map[string]any{"Дата": "2026-11-13", "Лицо1": int64(18655), "Лицо3": int64(700), "Лицо3.Название": "must not be forwarded"} {
		record["s"] = append(record["s"].([]any), map[string]any{"n": name, "t": "Строка"})
		record["d"] = append(record["d"].([]any), value)
	}
	return record
}

func orderRealizationPostExpected() SabyOrderRealizationPostRequest {
	id := orderRealizationTestIdentity
	id.MovementWarehouseID = 284
	return SabyOrderRealizationPostRequest{Identity: id, OrganizationID: 700, RegulationID: 122650, BuyerID: 18655,
		Lines: []SabyOrderRealizationPostLine{{NomenclatureID: 1101, Quantity: "2.125", UnitPrice: "0"}}}
}

func orderRealizationPostClient(t *testing.T, record, lines map[string]any, post func(http.ResponseWriter, *http.Request, map[string]json.RawMessage)) (*SabyClient, *atomic.Int64) {
	t.Helper()
	for _, name := range []string{"Цена", "СуммаЦен", "СуммаЦенБезНДС"} {
		lines["s"] = append(lines["s"].([]any), map[string]any{"n": name, "t": map[string]any{"n": "Деньги", "p": 2}})
		rows := lines["d"].([]any)
		for i, row := range rows {
			rows[i] = append(row.([]any), json.Number("0"))
		}
	}
	writes := new(atomic.Int64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Protocol int                        `json:"protocol"`
			Method   string                     `json:"method"`
			Params   map[string]json.RawMessage `json:"params"`
		}
		if r.URL.Path != "/service/" || json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid RPC transport")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "ДокОтгрИсх.Прочитать", "РеалИсх.DocNomList":
			if request.Protocol != 6 {
				t.Error("read protocol changed")
			}
			data := any(record)
			if request.Method == "РеалИсх.DocNomList" {
				data = lines
			}
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": data})
		case "Документ.PostAsync":
			writes.Add(1)
			if request.Protocol != 7 || r.Header.Get("Content-Type") != "application/json-rpc;charset=utf-8" || r.Header.Get("X-SBISAccessToken") != "fixture-token" {
				t.Error("wrong protocol-7 write headers")
			}
			post(w, r, request.Params)
		default:
			t.Errorf("unexpected RPC %s", request.Method)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	client := NewSabyClient("fixture-client", "fixture-secret", "fixture-key", 278, 6)
	client.client, client.serviceURL = server.Client(), server.URL+"/service/"
	client.authURL = server.URL + "/auth/"
	client.token, client.tokenUntil = "fixture-token", time.Now().Add(time.Hour)
	return client, writes
}

func TestSabyOrderRealizationPostExactProtocol7Binding(t *testing.T) {
	client, writes := orderRealizationPostClient(t, orderRealizationPostFixture(), orderRealizationLineFixture(), func(w http.ResponseWriter, r *http.Request, params map[string]json.RawMessage) {
		if len(params) != 1 {
			t.Fatal("unexpected PostAsync parameters")
		}
		var outer map[string]any
		json.Unmarshal(params["params"], &outer)
		checks, ok := sabyRecordField(outer, "CheckSettings").(map[string]any)
		if !ok {
			t.Fatal("missing CheckSettings record")
		}
		doc, ok := sabyRecordField(checks, "DocRec").(map[string]any)
		if !ok {
			t.Fatal("missing DocRec record")
		}
		wantNames := []string{"@Документ", "ТипДокумента", "Дата", "ДокументНашаОрганизация", "Лицо3", "Лицо3.Название", "Лицо1", "СкладскойДокумент.Склад", "isAcceptance", "errJournalEnabled"}
		wantTypes := []string{"Число целое", "Связь", "Дата", "Связь", "Связь", "Связь", "Связь", "Связь", "Логическое", "Логическое"}
		fields := doc["s"].([]any)
		if len(fields) != len(wantNames) {
			t.Fatal("DocRec includes unrelated fields")
		}
		for i, raw := range fields {
			f := raw.(map[string]any)
			if f["n"] != wantNames[i] || f["t"] != wantTypes[i] {
				t.Fatalf("native schema field %d changed", i)
			}
		}
		wantData := []any{float64(42737), float64(226), "2026-11-13", float64(700), float64(700), nil, float64(18655), float64(283), nil, true}
		if !reflect.DeepEqual(doc["d"], wantData) {
			t.Fatal("PostAsync sends wrong IDs or unexpected header fields")
		}
		if outer["f"] != float64(0) || checks["f"] != float64(1) || doc["f"] != float64(2) || sabyRecordField(outer, "skipCheck") != nil {
			t.Fatal("PostAsync native record binding changed")
		}
		if !reflect.DeepEqual(sabyRecordField(checks, "Operation"), []any{"posting"}) {
			t.Fatal("unexpected posting operation")
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": `{"result":"ok","job":"fixture"}`, "id": 1})
	})
	result, err := client.PostOrderRealization(context.Background(), orderRealizationPostExpected())
	if err != nil || result.Outcome != "queued" || result.HTTPStatus != 200 || len(result.Receipt) == 0 || writes.Load() != 1 {
		t.Fatalf("expected one pending-verification write: %+v %v writes%d", result, err, writes.Load())
	}
}

func TestSabyOrderRealizationPostPreflightBlocksForeignOrChangedDocument(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(map[string]any, map[string]any, *SabyOrderRealizationPostRequest)
	}{
		{"wrong organization", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) {
			orderRealizationSet(r, "ДокументНашаОрганизация", int64(701))
		}},
		{"wrong operation", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) {
			orderRealizationSet(r, "Регламент", int64(122498))
		}},
		{"wrong buyer", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) {
			orderRealizationSet(r, "Лицо1", int64(99))
		}},
		{"wrong shipper", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) {
			orderRealizationSet(r, "Лицо3", int64(99))
		}},
		{"wrong UUID", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) {
			e.Identity.UUID = "8d6e3870-40b3-4c82-8d2b-ccefd310637d"
		}},
		{"wrong header warehouse", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.Identity.WarehouseID = 99 }},
		{"wrong movement warehouse", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.Identity.MovementWarehouseID = 283 }},
		{"missing date", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { orderRealizationSet(r, "Дата", nil) }},
		{"changed quantity", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.Lines[0].Quantity = "2.126" }},
		{"changed price", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.Lines[0].UnitPrice = "10.20" }},
		{"changed item", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.Lines[0].NomenclatureID = 999 }},
		{"empty goods", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { l["d"] = []any{} }},
		{"incoming line", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { l["d"].([]any)[0].([]any)[5] = int64(0) }},
		{"zero quantity", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.Lines[0].Quantity = "0" }},
		{"no buyer mapping", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) { e.BuyerID = 0 }},
		{"prior posting error", func(r, l map[string]any, e *SabyOrderRealizationPostRequest) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[14] = true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, l, e := orderRealizationPostFixture(), orderRealizationLineFixture(), orderRealizationPostExpected()
			test.edit(r, l, &e)
			client, writes := orderRealizationPostClient(t, r, l, func(http.ResponseWriter, *http.Request, map[string]json.RawMessage) { t.Error("unsafe write reached") })
			result, err := client.PostOrderRealization(context.Background(), e)
			if err == nil || result.Outcome != "not_sent" || writes.Load() != 0 {
				t.Fatalf("preflight did not block: %+v %v writes%d", result, err, writes.Load())
			}
		})
	}
}

func TestSabyOrderRealizationPostNoReplayOnLostResponseAuthOrRedirect(t *testing.T) {
	for _, mode := range []string{"lost", "unauthorized", "redirect", "rpc-error", "malformed", "null", "oversized", "empty-nom"} {
		t.Run(mode, func(t *testing.T) {
			var redirects atomic.Int64
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); t.Error("write followed redirect") }))
			defer destination.Close()
			client, writes := orderRealizationPostClient(t, orderRealizationPostFixture(), orderRealizationLineFixture(), func(w http.ResponseWriter, r *http.Request, p map[string]json.RawMessage) {
				switch mode {
				case "lost":
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					connection.Close()
				case "unauthorized":
					w.WriteHeader(401)
				case "redirect":
					http.Redirect(w, r, destination.URL, 307)
				case "rpc-error":
					fmt.Fprint(w, `{"jsonrpc":"2.0","error":{"code":-32000,"message":"vendor details"},"id":1}`)
				case "malformed":
					fmt.Fprint(w, `{"result":"not-json"}`)
				case "null":
					fmt.Fprint(w, `{"result":null}`)
				case "oversized":
					json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"result": "ok", "padding": strings.Repeat("x", 65<<10)}})
				case "empty-nom":
					json.NewEncoder(w).Encode(map[string]any{"result": `{"result":"error","detail":[{"check_type":"EMPTY_NOM","message":"sensitive remote text"}]}`})
				}
			})
			result, err := client.PostOrderRealization(context.Background(), orderRealizationPostExpected())
			want := "unknown"
			if mode == "empty-nom" {
				want = "rejected"
				if result.Code != "EMPTY_NOM" {
					t.Fatal("lost vendor validation code")
				}
			}
			if err == nil || result.Outcome != want || writes.Load() != 1 || redirects.Load() != 0 || bytes.Contains([]byte(err.Error()), []byte("sensitive")) {
				t.Fatalf("unsafe replay/classification %+v/%v writes%d", result, err, writes.Load())
			}
		})
	}
}

func TestSabyOrderRealizationPostAllowsObservedNullableDraftWithoutPostingProof(t *testing.T) {
	record := orderRealizationPostFixture()
	flags := sabyRecordField(record, "ДокументРасширение.Состояние").([]any)
	for _, index := range []int{10, 11, 18, 27} {
		flags[index] = nil
	}
	client, writes := orderRealizationPostClient(t, record, orderRealizationLineFixture(), func(w http.ResponseWriter, r *http.Request, p map[string]json.RawMessage) {
		json.NewEncoder(w).Encode(map[string]any{"result": `{"result":"ok"}`})
	})
	result, err := client.PostOrderRealization(context.Background(), orderRealizationPostExpected())
	if err != nil || result.Outcome != "queued" || writes.Load() != 1 {
		t.Fatalf("nullable unposted draft did not reach one bounded attempt: %+v %v", result, err)
	}
}

func TestSabyOrderRealizationPostExactDuplicateQuantitiesAndAlreadyPosted(t *testing.T) {
	record, lines, expected := orderRealizationPostFixture(), orderRealizationLineFixture(), orderRealizationPostExpected()
	expected.Lines[0].Quantity = "0.3"
	rows := lines["d"].([]any)
	row := append([]any(nil), rows[0].([]any)...)
	row[0], row[4] = int64(700002), json.Number("0.2")
	rows[0].([]any)[4] = json.Number("0.1")
	lines["d"] = append(rows, row)
	flags := sabyRecordField(record, "ДокументРасширение.Состояние").([]any)
	flags[10], flags[11] = true, true
	client, writes := orderRealizationPostClient(t, record, lines, func(http.ResponseWriter, *http.Request, map[string]json.RawMessage) {
		t.Error("reposted existing document")
	})
	result, err := client.PostOrderRealization(context.Background(), expected)
	if err != nil || result.Outcome != "already_posted" || writes.Load() != 0 {
		t.Fatalf("exact quantity reconciliation failed: %+v %v", result, err)
	}
}

func TestSabyOrderRealizationPostDoesNotRepostQueuedDocument(t *testing.T) {
	record := orderRealizationPostFixture()
	sabyRecordField(record, "ДокументРасширение.Состояние").([]any)[18] = true
	client, writes := orderRealizationPostClient(t, record, orderRealizationLineFixture(), func(http.ResponseWriter, *http.Request, map[string]json.RawMessage) {
		t.Error("queued document was posted again")
	})
	result, err := client.PostOrderRealization(context.Background(), orderRealizationPostExpected())
	if err != nil || result.Outcome != "queued" || writes.Load() != 0 {
		t.Fatalf("queued state was not preserved: %+v %v", result, err)
	}
}

func TestSabyOrderRealizationPostSameSKUTotalCannotHideDifferentPrice(t *testing.T) {
	record, lines, expected := orderRealizationPostFixture(), orderRealizationLineFixture(), orderRealizationPostExpected()
	expected.Lines[0].Quantity = "0.3"
	rows := lines["d"].([]any)
	second := append([]any(nil), rows[0].([]any)...)
	rows[0].([]any)[4] = json.Number("0.1")
	second[0], second[4] = int64(700002), json.Number("0.2")
	lines["d"] = append(rows, second)
	client, writes := orderRealizationPostClient(t, record, lines, func(http.ResponseWriter, *http.Request, map[string]json.RawMessage) {
		t.Error("changed price reached posting")
	})
	// Quantity/SKU totals remain identical, but the second native row carries
	// a different sale price. It must not be absorbed into the zero-price group.
	second = lines["d"].([]any)[1].([]any)
	second[8], second[9], second[10] = json.Number("1"), json.Number("0.20"), json.Number("0.20")
	result, err := client.PostOrderRealization(context.Background(), expected)
	if err == nil || result.Outcome != "not_sent" || writes.Load() != 0 {
		t.Fatalf("different price was lost in SKU aggregation: %+v %v", result, err)
	}
}

func TestSabyOrderRealizationWriteOnceRejectsUnprovenMethod(t *testing.T) {
	client, writes := orderRealizationPostClient(t, orderRealizationPostFixture(), orderRealizationLineFixture(), func(http.ResponseWriter, *http.Request, map[string]json.RawMessage) { t.Error("unexpected write") })
	if _, _, err := client.sabyOrderRealizationWriteRPCOnce(context.Background(), "Документ.ПроизвольноеИзменение", nil); err == nil || writes.Load() != 0 {
		t.Fatal("unproven method was accepted")
	}
}
