package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fillHarness struct {
	header      map[string]any
	rows        []any
	template    map[string]any
	writes      []string
	writeStatus int
	editReply   func(string, map[string]any)
	inspect     func(string, map[string]any)
}

func fillRequest() SabyOrderRealizationFillRequest {
	id := orderRealizationTestIdentity
	id.MovementWarehouseID = 284
	next := SabyOrderRealizationFillLine{NomenclatureID: 1101, Quantity: json.Number("2"), UnitPrice: json.Number("10.20")}
	return SabyOrderRealizationFillRequest{Identity: id, OrganizationID: 700, RegulationID: 122650, BuyerID: 41, Lines: []SabyOrderRealizationFillLine{next}, Next: next}
}
func fillLine(saved bool) map[string]any {
	id, closed := any(nil), any(nil)
	if saved {
		id = int64(700001)
		closed = false
	}
	return map[string]any{"_type": "record", "f": 0, "s": []any{
		map[string]any{"n": "@НоменклатураДокумента", "t": "Число целое"},
		map[string]any{"n": "Номенклатура", "t": "Связь"},
		map[string]any{"n": "СкладскойДокумент", "t": "Связь"},
		map[string]any{"n": "Склад", "t": "Связь"},
		map[string]any{"n": "Количество", "t": "Число вещественное"},
		map[string]any{"n": "Тип", "t": map[string]any{"n": "Перечисляемое", "s": map[string]any{"1": "Расход"}}},
		map[string]any{"n": "Закрыто", "t": "Логическое"},
		map[string]any{"n": "Флаги", "t": map[string]any{"n": "Флаги", "s": map[string]any{"0": "Удален", "24": "Оплачено"}}},
		map[string]any{"n": "Цена", "t": map[string]any{"n": "Деньги", "p": 2}},
		map[string]any{"n": "СуммаЦен", "t": map[string]any{"n": "Деньги", "p": 2}},
		map[string]any{"n": "PreservedNativeNumber", "t": "Число целое"},
	}, "d": []any{id, int64(1101), int64(42737), int64(284), json.Number("2.0"), int64(1), closed, []any{false, nil}, json.Number("10.20"), json.Number("20.40"), json.Number("9007199254740993")}}
}
func fillSet(rows []any) map[string]any {
	return map[string]any{"_type": "recordset", "s": fillLine(true)["s"], "d": rows}
}
func newFillHarness(t *testing.T) (*SabyClient, *fillHarness) {
	t.Helper()
	h := &fillHarness{header: orderRealizationReadFixture(), rows: []any{}, template: fillLine(false)}
	flags := sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)
	flags[10] = nil
	flags[11] = nil
	flags[18] = nil
	flags[27] = nil
	for _, field := range []struct {
		name  string
		value any
	}{{"Лицо1", int64(41)}, {"Лицо3", int64(700)}, {"Дата", "2026-11-13"}} {
		h.header["s"] = append(h.header["s"].([]any), map[string]any{"n": field.name, "t": "Строка"})
		h.header["d"] = append(h.header["d"].([]any), field.value)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Protocol int             `json:"protocol"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if r.URL.Path != "/service/" || json.NewDecoder(r.Body).Decode(&q) != nil {
			t.Error("unexpected auth/transport request")
			w.WriteHeader(400)
			return
		}
		p, e := sabyFillDecode(q.Params)
		if e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		var reply any
		switch q.Method {
		case "ДокОтгрИсх.Прочитать":
			if q.Protocol != 6 {
				t.Error("read protocol")
			}
			reply = h.header
		case "РеалИсх.DocNomList":
			if q.Protocol != 6 {
				t.Error("list protocol")
			}
			reply = fillSet(h.rows)
		case "РеалИсх.DocNomCreate", "РеалИсх.CustomDocNomUpdate":
			if q.Protocol != 7 {
				t.Error("write protocol")
			}
			h.writes = append(h.writes, q.Method)
			if h.inspect != nil {
				h.inspect(q.Method, p)
			}
			if h.writeStatus != 0 {
				w.WriteHeader(h.writeStatus)
				return
			}
			if q.Method == "РеалИсх.DocNomCreate" {
				reply = h.template
			} else {
				saved := fillLine(true)
				reply = sabyRecord(map[string]any{"result": int64(700001), "details": map[string]any{}, "nav_res": "", "new_rs": fillSet([]any{saved["d"]}), "upd_rs": map[string]any{"_type": "recordset", "s": []any{}, "d": []any{}}, "del_rs": nil, "missed_rs": nil, "data": nil, "total_rec": sabyRecord(map[string]any{})})
			}
			if q.Method == "РеалИсх.CustomDocNomUpdate" {
				for _, raw := range reply.(map[string]any)["s"].([]any) {
					field := raw.(map[string]any)
					field["t"] = map[string]string{"result": "Число целое", "details": "JSON-объект", "nav_res": "Строка", "new_rs": "Выборка", "upd_rs": "Выборка", "del_rs": "Выборка", "missed_rs": "Выборка", "data": "Запись", "total_rec": "Запись"}[field["n"].(string)]
				}
			}
			if h.editReply != nil {
				h.editReply(q.Method, reply.(map[string]any))
			}
		default:
			t.Errorf("unexpected method %s", q.Method)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": reply})
	}))
	t.Cleanup(srv.Close)
	c := NewSabyClient("fake", "fake", "fake", 278, 6)
	c.client = srv.Client()
	c.serviceURL = srv.URL + "/service/"
	c.token = "fake-token"
	c.tokenUntil = time.Now().Add(time.Hour)
	return c, h
}
func TestSabyOrderRealizationFillPreparedDurableExactNativeRow(t *testing.T) {
	c, h := newFillHarness(t)
	req := fillRequest()
	h.inspect = func(method string, p map[string]any) {
		if method == "РеалИсх.DocNomCreate" {
			f := p["Фильтр"].(map[string]any)
			if sabyRecordField(f, "Цена") != "10.20" || sabyValue(sabyRecordField(f, "Количество")) != "2" {
				t.Fatal("captured price string/qty lost")
			}
		}
		if method == "РеалИсх.CustomDocNomUpdate" {
			row := p["Запись"].(map[string]any)
			if sabyValue(sabyRecordField(row, "PreservedNativeNumber")) != "9007199254740993" {
				t.Fatal("native integer rounded")
			}
			if sabyRecordField(row, "Флаги").([]any)[1] != nil {
				t.Fatal("unrelated paid flag was guessed")
			}
			if sabyValue(p["ДопПоля"].(map[string]any)["СкладскойДокумент"]) != "42737" {
				t.Fatal("save not document bound")
			}
		}
	}
	prepared, err := c.PrepareOrderRealizationRow(context.Background(), req)
	if err != nil || prepared.Outcome != "prepared" {
		t.Fatalf("prepare %+v %v", prepared, err)
	}
	// JSONB may normalize numeric spelling; semantically equal durable receipts survive.
	raw, _ := json.Marshal(prepared.Prepared)
	raw = []byte(strings.ReplaceAll(string(raw), "2.0,", "2.00,"))
	var durable SabyOrderRealizationPreparedRow
	if json.Unmarshal(raw, &durable) != nil {
		t.Fatal("durable decode")
	}
	saved, err := c.SavePreparedOrderRealizationRow(context.Background(), req, durable)
	if err != nil || saved.Outcome != "saved" || saved.RowID != 700001 {
		t.Fatalf("save %+v %v", saved, err)
	}
	if len(h.writes) != 2 {
		t.Fatal(h.writes)
	}
	// An unknown prior save must never be replayed after the line becomes visible.
	h.rows = []any{fillLine(true)["d"]}
	again, err := c.SavePreparedOrderRealizationRow(context.Background(), req, durable)
	if err == nil || again.Outcome != "not_sent" || len(h.writes) != 2 {
		t.Fatal("duplicate native line was sent")
	}
}
func TestSabyOrderRealizationFillGuardsBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*fillHarness, *SabyOrderRealizationFillRequest)
	}{
		{"buyer", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			orderRealizationSet(h.header, "Лицо1", int64(42))
		}},
		{"organization", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			orderRealizationSet(h.header, "Лицо3", int64(701))
		}},
		{"posted", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[10] = true
		}},
		{"queued", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[18] = true
		}},
		{"wrong UUID", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			r.Identity.UUID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{"wrong movement warehouse", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			h.rows = []any{fillLine(true)["d"]}
			r.Identity.MovementWarehouseID = 999
		}},
		{"unexpected existing group", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			line := fillLine(true)
			orderRealizationSet(line, "Номенклатура", int64(999))
			h.rows = []any{line["d"]}
		}},
		{"already complete", func(h *fillHarness, r *SabyOrderRealizationFillRequest) { h.rows = []any{fillLine(true)["d"]} }},
		{"fractional create unsupported", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			r.Next.Quantity = json.Number("0.5")
			r.Lines[0] = r.Next
		}},
		{"empty full composition", func(h *fillHarness, r *SabyOrderRealizationFillRequest) { r.Lines = nil }},
		{"missing date", func(h *fillHarness, r *SabyOrderRealizationFillRequest) {
			orderRealizationSet(h.header, "Дата", nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h := newFillHarness(t)
			req := fillRequest()
			tc.edit(h, &req)
			got, err := c.PrepareOrderRealizationRow(context.Background(), req)
			if err == nil || got.Outcome != "not_sent" || len(h.writes) != 0 {
				t.Fatalf("unsafe write %+v %v %v", got, err, h.writes)
			}
		})
	}
}
func TestSabyOrderRealizationFillRejectsWrongTemplateWithoutPriceFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"native ignored requested price", func(r map[string]any) {
			orderRealizationSet(r, "Цена", json.Number("0"))
			orderRealizationSet(r, "СуммаЦен", json.Number("0"))
		}},
		{"wrong canonical item", func(r map[string]any) { orderRealizationSet(r, "Номенклатура", int64(999)) }},
		{"already saved", func(r map[string]any) {
			orderRealizationSet(r, "@НоменклатураДокумента", int64(50))
		}},
		{"wrong warehouse", func(r map[string]any) { orderRealizationSet(r, "Склад", int64(283)) }},
		{"sum mismatch", func(r map[string]any) { orderRealizationSet(r, "СуммаЦен", json.Number("10.20")) }},
		{"unknown money schema", func(r map[string]any) {
			for _, raw := range r["s"].([]any) {
				f := raw.(map[string]any)
				if f["n"] == "Цена" {
					f["t"] = "Число вещественное"
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h := newFillHarness(t)
			tc.edit(h.template)
			got, err := c.PrepareOrderRealizationRow(context.Background(), fillRequest())
			if err == nil || got.Outcome != "unknown" || got.Prepared != nil || len(h.writes) != 1 {
				t.Fatalf("wrong template accepted %+v %v", got, err)
			}
		})
	}
}
func TestSabyOrderRealizationFillSaveTamperAndChangedBaseline(t *testing.T) {
	for _, mode := range []string{"row", "request", "baseline"} {
		t.Run(mode, func(t *testing.T) {
			c, h := newFillHarness(t)
			req := fillRequest()
			p, err := c.PrepareOrderRealizationRow(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "row":
				p.Prepared.Row = []byte(strings.Replace(string(p.Prepared.Row), "9007199254740993", "9007199254740994", 1))
			case "request":
				req.BuyerID++
			case "baseline":
				orderRealizationSet(h.header, "Дата", "2026-11-14")
			}
			got, err := c.SavePreparedOrderRealizationRow(context.Background(), req, *p.Prepared)
			if err == nil || got.Outcome != "not_sent" || len(h.writes) != 1 {
				t.Fatal("tampered request wrote", got, err)
			}
		})
	}
}
func TestSabyOrderRealizationFillUnknownSaveNeverReplaysTransport(t *testing.T) {
	for _, mode := range []string{"401", "malformed receipt", "extra changed row", "wrong saved price", "extra new row"} {
		t.Run(mode, func(t *testing.T) {
			c, h := newFillHarness(t)
			req := fillRequest()
			p, err := c.PrepareOrderRealizationRow(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "401" {
				h.writeStatus = 401
			} else {
				h.editReply = func(_ string, r map[string]any) {
					switch mode {
					case "malformed receipt":
						orderRealizationSet(r, "result", nil)
					case "extra changed row":
						orderRealizationSet(r, "upd_rs", fillSet([]any{fillLine(true)["d"]}))
					case "extra new row":
						orderRealizationSet(r, "new_rs", fillSet([]any{fillLine(true)["d"], fillLine(true)["d"]}))
					case "wrong saved price":
						line := fillLine(true)
						orderRealizationSet(line, "Цена", json.Number("0"))
						orderRealizationSet(line, "СуммаЦен", json.Number("0"))
						orderRealizationSet(r, "new_rs", fillSet([]any{line["d"]}))
					}
				}
			}
			got, err := c.SavePreparedOrderRealizationRow(context.Background(), req, *p.Prepared)
			if err == nil || got.Outcome != "unknown" || len(h.writes) != 2 {
				t.Fatalf("ambiguous save replayed/accepted %+v %v %v", got, err, h.writes)
			}
		})
	}
}
func TestSabyOrderRealizationPricedLinesRejectMissingPriceAndDuplicateIDs(t *testing.T) {
	for _, mode := range []string{"valid", "missing price", "wrong precision", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			c, h := newFillHarness(t)
			line := fillLine(true)
			switch mode {
			case "missing price":
				orderRealizationSet(line, "Цена", nil)
			case "wrong precision":
				orderRealizationSet(line, "Цена", json.Number("10.201"))
			}
			h.rows = []any{line["d"]}
			if mode == "duplicate" {
				h.rows = append(h.rows, line["d"])
			}
			got, err := c.ReadOrderRealizationPricedLines(context.Background(), fillRequest().Identity)
			if (err == nil) != (mode == "valid") || len(h.writes) != 0 {
				t.Fatalf("got %v %v writes%v", got, err, h.writes)
			}
		})
	}
}

func TestSabyOrderRealizationFillAllowsOnlyCompleteExpectedPrefix(t *testing.T) {
	c, h := newFillHarness(t)
	req := fillRequest()
	prefix := fillLine(true)
	orderRealizationSet(prefix, "Номенклатура", int64(2222))
	orderRealizationSet(prefix, "@НоменклатураДокумента", int64(700002))
	h.rows = []any{prefix["d"]}
	req.Lines = append(req.Lines, SabyOrderRealizationFillLine{NomenclatureID: 2222, Quantity: json.Number("2"), UnitPrice: json.Number("10.20")})
	got, err := c.PrepareOrderRealizationRow(context.Background(), req)
	if err != nil || got.Outcome != "prepared" || len(h.writes) != 1 {
		t.Fatalf("valid prefix rejected %+v %v", got, err)
	}
	// Changing even an existing group's price before Save blocks the write.
	orderRealizationSet(prefix, "Цена", json.Number("10.00"))
	orderRealizationSet(prefix, "СуммаЦен", json.Number("20.00"))
	saved, err := c.SavePreparedOrderRealizationRow(context.Background(), req, *got.Prepared)
	if err == nil || saved.Outcome != "not_sent" || len(h.writes) != 1 {
		t.Fatal("changed prefix accepted", saved, err)
	}
}
