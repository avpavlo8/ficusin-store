package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var orderRealizationTestIdentity = SabyOrderRealizationIdentity{ID: 42737, UUID: "01a1cb92-38be-7fbd-865d-d89aa0b571eb", WarehouseID: 283}

func TestSabyOrderRealizationReadObservedNullableDraftFlagsAndBuyer(t *testing.T) {
	record := orderRealizationReadFixture()
	flags := sabyRecordField(record, "ДокументРасширение.Состояние").([]any)
	// Verified fresh ordinary draft42807: these statuses are unset, while error
	// and stale flags are explicitly false. Null is not evidence of posting.
	for _, index := range []int{10, 11, 18, 27} {
		flags[index] = nil
	}
	record["s"] = append(record["s"].([]any), map[string]any{"n": "Лицо1", "t": "Связь"})
	record["d"] = append(record["d"].([]any), int64(18655))
	client := orderRealizationReadClient(t, func(method string, params map[string]json.RawMessage) any { return record })
	snapshot, err := client.ReadOrderRealization(context.Background(), orderRealizationTestIdentity)
	if err != nil || snapshot.HeaderPosted || snapshot.PostingStateKnown || snapshot.Posted || snapshot.HasEntries || snapshot.PostingQueued || snapshot.EntriesQueued || snapshot.BuyerID != 18655 {
		t.Fatalf("nullable draft was rejected or mistaken for posting proof: %+v %v", snapshot, err)
	}
	// Schema loss remains a hard failure, distinct from a present null value.
	for _, raw := range record["s"].([]any) {
		field := raw.(map[string]any)
		if field["n"] == "ДокументРасширение.Состояние" {
			delete(field["t"].(map[string]any)["s"].(map[string]any), "18")
		}
	}
	if _, err = client.ReadOrderRealization(context.Background(), orderRealizationTestIdentity); err == nil {
		t.Fatal("missing posting schema was treated as an unposted draft")
	}
}

func orderRealizationReadFixture() map[string]any {
	labels := map[string]any{}
	flags := make([]any, 31)
	for i := range flags {
		labels[fmt.Sprint(i)] = fmt.Sprintf("Other%d", i)
	}
	for key, name := range map[string]string{"10": "Проведен", "11": "ЕстьПроводки", "14": "ОшибкиПроведения", "16": "НеактуальныеПроводки", "18": "ВОчередиНаПроведение", "27": "ВОчередиНаРасчетПроводок"} {
		labels[key] = name
	}
	flags[10], flags[11], flags[14], flags[16], flags[18], flags[27] = true, true, false, false, false, false
	record := sabyRecord(map[string]any{
		"@Документ": int64(42737), "ИдентификаторДокумента": orderRealizationTestIdentity.UUID,
		"ТипДокумента": int64(226), "ТипДокумента.ТипДокумента": "ДокОтгрИсх", "Удален": false,
		"СкладскойДокумент.Склад": int64(283), "ДокументНашаОрганизация": int64(700), "Регламент": int64(122650),
		"Документ_": `"СкладскойДокумент"`, "ДокументРасширение.ИдентификаторВИ": "6bcfd75e-4d34-44a7-af38-77b69d61cd94",
		"ДокументРасширение.Состояние": flags,
	})
	for _, raw := range record["s"].([]any) {
		field := raw.(map[string]any)
		if field["n"] == "ДокументРасширение.Состояние" {
			field["t"] = map[string]any{"n": "Флаги", "s": labels}
		}
	}
	return record
}

func orderRealizationSet(record map[string]any, name string, value any) {
	fields, data := record["s"].([]any), record["d"].([]any)
	for i, raw := range fields {
		if raw.(map[string]any)["n"] == name {
			data[i] = value
			return
		}
	}
}

func orderRealizationReadClient(t *testing.T, handle func(string, map[string]json.RawMessage) any) *SabyClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Protocol int                        `json:"protocol"`
			Method   string                     `json:"method"`
			Params   map[string]json.RawMessage `json:"params"`
		}
		if r.URL.Path != "/service/" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Protocol != 6 {
			t.Error("unexpected transport instead of protocol-6 native Read")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": handle(request.Method, request.Params)})
	}))
	t.Cleanup(server.Close)
	client := NewSabyClient("fixture-client", "fixture-secret", "fixture-key", 278, 6)
	client.client, client.serviceURL = server.Client(), server.URL+"/service/"
	client.token, client.tokenUntil = "fixture-token", time.Now().Add(time.Hour)
	return client
}

func TestSabyOrderRealizationReadRequiresNativeIdentityAndPostingProof(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		edit   func(map[string]any)
		bad    bool
		posted bool
	}{
		{name: "posted native header", posted: true},
		{name: "not yet posted", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[10] = false
		}},
		{name: "no accounting entries", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[11] = false
		}},
		{name: "posting errors", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[14] = true
		}},
		{name: "stale entries", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[16] = true
		}},
		{name: "posting queued", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[18] = true
		}},
		{name: "entry calculation queued", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[27] = true
		}},
		{name: "unknown error flag is not confirmation", edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[14] = nil
		}},
		{name: "wrong ID", bad: true, edit: func(r map[string]any) { orderRealizationSet(r, "@Документ", int64(42738)) }},
		{name: "wrong UUID", bad: true, edit: func(r map[string]any) {
			orderRealizationSet(r, "ИдентификаторДокумента", "01a1c670-f345-7c11-a595-abd50188f430")
		}},
		{name: "public UUID cannot replace native UUID", bad: true, edit: func(r map[string]any) {
			orderRealizationSet(r, "ИдентификаторДокумента", "6bcfd75e-4d34-44a7-af38-77b69d61cd94")
		}},
		{name: "wrong warehouse", bad: true, edit: func(r map[string]any) {
			orderRealizationSet(r, "СкладскойДокумент.Склад", int64(999))
		}},
		{name: "missing warehouse", bad: true, edit: func(r map[string]any) { orderRealizationSet(r, "СкладскойДокумент.Склад", nil) }},
		{name: "incoming type", bad: true, edit: func(r map[string]any) { orderRealizationSet(r, "ТипДокумента", int64(224)) }},
		{name: "wrong document family", bad: true, edit: func(r map[string]any) {
			orderRealizationSet(r, "ТипДокумента.ТипДокумента", "ДокОтгрВх")
		}},
		{name: "deleted", bad: true, edit: func(r map[string]any) { orderRealizationSet(r, "Удален", true) }},
		{name: "unknown deletion", bad: true, edit: func(r map[string]any) { orderRealizationSet(r, "Удален", nil) }},
		{name: "missing native flags despite successful EDO state", bad: true, edit: func(r map[string]any) {
			for _, raw := range r["s"].([]any) {
				field := raw.(map[string]any)
				if field["n"] == "ДокументРасширение.Состояние" {
					field["n"] = "СостояниеЭДО"
				}
			}
		}},
		{name: "malformed flag", bad: true, edit: func(r map[string]any) {
			sabyRecordField(r, "ДокументРасширение.Состояние").([]any)[10] = "true"
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			record := orderRealizationReadFixture()
			if scenario.edit != nil {
				scenario.edit(record)
			}
			reads := 0
			client := orderRealizationReadClient(t, func(method string, params map[string]json.RawMessage) any {
				reads++
				if method != "ДокОтгрИсх.Прочитать" || string(params["ИдО"]) != "42737" || string(params["ИмяМетода"]) != `"ДокОтгрИсх.Список"` {
					t.Fatalf("read-only recovery attempted another method or identity: %s", method)
				}
				return record
			})
			got, err := client.ReadOrderRealization(context.Background(), orderRealizationTestIdentity)
			if (err != nil) != scenario.bad || got.HeaderPosted != scenario.posted || reads != 1 {
				t.Fatalf("snapshot=%+v err=%v reads=%d", got, err, reads)
			}
			if err == nil && (got.ID != 42737 || got.WarehouseID != 283 || got.OrganizationID != 700 || got.RegulationID != 122650 || got.PublicUUID == got.UUID || got.document == nil) {
				t.Fatal("native metadata or distinct public UUID was lost")
			}
		})
	}
}

func TestSabyOrderRealizationReadCannotDiscoverOrCreateMissingIdentity(t *testing.T) {
	calls := 0
	client := orderRealizationReadClient(t, func(string, map[string]json.RawMessage) any { calls++; return orderRealizationReadFixture() })
	for _, expected := range []SabyOrderRealizationIdentity{{}, {ID: 42737, WarehouseID: 283}, {ID: 42737, UUID: "not-a-uuid", WarehouseID: 283}, {ID: 42737, UUID: orderRealizationTestIdentity.UUID}, {ID: -1, UUID: orderRealizationTestIdentity.UUID, WarehouseID: 283}} {
		if _, err := client.ReadOrderRealization(context.Background(), expected); err == nil {
			t.Fatal("incomplete identity accepted")
		}
	}
	if calls != 0 {
		t.Fatal("missing identity reached a provider method")
	}
}

func TestSabyOrderRealizationFlagsFollowDictionaryOrdinals(t *testing.T) {
	record := orderRealizationReadFixture()
	for _, raw := range record["s"].([]any) {
		field := raw.(map[string]any)
		if field["n"] == "ДокументРасширение.Состояние" {
			field["t"] = map[string]any{"n": "Флаги", "s": map[string]any{"27": "ВОчередиНаРасчетПроводок", "10": "Проведен", "11": "ЕстьПроводки", "14": "ОшибкиПроведения", "16": "НеактуальныеПроводки", "18": "ВОчередиНаПроведение"}}
		}
	}
	orderRealizationSet(record, "ДокументРасширение.Состояние", []any{true, true, false, false, false, false})
	client := orderRealizationReadClient(t, func(string, map[string]json.RawMessage) any { return record })
	got, err := client.ReadOrderRealization(context.Background(), orderRealizationTestIdentity)
	if err != nil || !got.HeaderPosted {
		t.Fatalf("sparse dictionary lost labelled posting state: %+v/%v", got, err)
	}
}

func orderRealizationLineFixture() map[string]any {
	return map[string]any{"_type": "recordset", "s": []any{
		map[string]any{"n": "@НоменклатураДокумента", "t": "Число целое"},
		map[string]any{"n": "Номенклатура", "t": "Число целое"},
		map[string]any{"n": "СкладскойДокумент", "t": "Число целое"},
		map[string]any{"n": "Склад", "t": "Число целое"},
		map[string]any{"n": "Количество", "t": "Число вещественное"},
		map[string]any{"n": "Тип", "t": map[string]any{"n": "Перечисляемое", "s": map[string]any{"0": "Приход", "1": "Расход"}}},
		map[string]any{"n": "Закрыто", "t": "Логическое"},
		map[string]any{"n": "Флаги", "t": map[string]any{"n": "Флаги", "s": map[string]any{"0": "Удален", "2": "Непросчитано"}}},
	}, "d": []any{[]any{int64(700001), int64(1101), int64(42737), int64(284), json.Number("2.125"), int64(1), true, []any{false, false}}}}
}

func TestSabyOrderRealizationLinesStayBoundToDocumentAndMovementWarehouse(t *testing.T) {
	for _, test := range []struct {
		name   string
		edit   func(map[string]any)
		bad    bool
		closed bool
	}{
		{name: "verified separate warehouse identity", closed: true},
		{name: "wrong owner document", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[2] = int64(42738) }},
		{name: "header warehouse is not the movement warehouse", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[3] = int64(283) }},
		{name: "foreign movement warehouse", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[3] = int64(999) }},
		{name: "unmapped nomenclature", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[1] = nil }},
		{name: "incoming movement", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[5] = int64(0) }},
		{name: "deleted line", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[7].([]any)[0] = true }},
		{name: "negative quantity", bad: true, edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[4] = json.Number("-2") }},
		{name: "unclosed movement is not posting proof", edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[6] = false }},
		{name: "uncalculated line is not posting proof", edit: func(s map[string]any) { s["d"].([]any)[0].([]any)[7].([]any)[1] = true }},
		{name: "no lines is not posting proof", edit: func(s map[string]any) { s["d"] = []any{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			set := orderRealizationLineFixture()
			if test.edit != nil {
				test.edit(set)
			}
			reads := 0
			client := orderRealizationReadClient(t, func(method string, params map[string]json.RawMessage) any {
				reads++
				switch method {
				case "ДокОтгрИсх.Прочитать":
					return orderRealizationReadFixture()
				case "РеалИсх.DocNomList":
					var filter, page map[string]any
					if json.Unmarshal(params["Фильтр"], &filter) != nil || json.Unmarshal(params["Навигация"], &page) != nil {
						t.Error("invalid native filter")
					}
					if id, err := sabyInt64Field(filter, "СкладскойДокумент"); err != nil || id != 42737 {
						t.Error("line request was not scoped to the saved document")
					}
					if sabyValue(sabyRecordField(filter, "Тип")) != "1" || sabyValue(sabyRecordField(page, "Страница")) != "0" || sabyValue(sabyRecordField(page, "РазмерСтраницы")) != "100" {
						t.Error("outgoing read filter differs from verified vendor contract")
					}
					return set
				default:
					t.Errorf("read-only verification attempted another method: %s", method)
					return nil
				}
			})
			expected := orderRealizationTestIdentity
			expected.MovementWarehouseID = 284
			got, err := client.ReadOrderRealizationWithLines(context.Background(), expected)
			if (err != nil) != test.bad || got.ClosedOutgoingLines != test.closed || reads != 2 {
				t.Fatalf("line snapshot=%+v err=%v reads=%d", got, err, reads)
			}
			if test.closed && (len(got.Lines) != 1 || got.Lines[0].Quantity != "2.125" || got.Lines[0].NomenclatureID != 1101 || got.WarehouseID != 283 || got.MovementWarehouseID != 284) {
				t.Fatal("exact quantities or distinct warehouse identities were lost")
			}
		})
	}
}

func TestSabyOrderRealizationLinesRequireVerifiedWarehouseMappingBeforeReading(t *testing.T) {
	calls := 0
	client := orderRealizationReadClient(t, func(string, map[string]json.RawMessage) any { calls++; return nil })
	if _, err := client.ReadOrderRealizationWithLines(context.Background(), orderRealizationTestIdentity); err == nil || calls != 0 {
		t.Fatal("unverified movement warehouse reached provider")
	}
}

func TestSabyOrderRealizationLinePaginationCannotRepeatRows(t *testing.T) {
	set := orderRealizationLineFixture()
	prototype := set["d"].([]any)[0].([]any)
	rows := make([]any, 100)
	for i := range rows {
		row := append([]any(nil), prototype...)
		row[0] = int64(700000 + i)
		rows[i] = row
	}
	set["d"] = rows
	pages := 0
	client := orderRealizationReadClient(t, func(method string, params map[string]json.RawMessage) any {
		if method == "ДокОтгрИсх.Прочитать" {
			return orderRealizationReadFixture()
		}
		if method != "РеалИсх.DocNomList" {
			t.Errorf("unexpected write-capable method %s", method)
			return nil
		}
		pages++
		return set // Vendor repeats page zero instead of respecting navigation.
	})
	expected := orderRealizationTestIdentity
	expected.MovementWarehouseID = 284
	if _, err := client.ReadOrderRealizationWithLines(context.Background(), expected); err == nil || pages != 2 {
		t.Fatal("duplicate/repeated vendor page was accepted or paged indefinitely")
	}
}
