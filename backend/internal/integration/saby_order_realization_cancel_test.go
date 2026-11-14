package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type cancelHarness struct {
	header               map[string]any
	line                 map[string]any
	calls, reads, status int
	noClear              bool
	editReply            func(map[string]any)
	afterWrite           func(*cancelHarness)
}

func cancelRequest() SabyOrderRealizationCancelRequest {
	r := fillRequest()
	return SabyOrderRealizationCancelRequest{Identity: r.Identity, ExpectedRowIDs: []int64{700001}, PublicUUID: "6bcfd75e-4d34-44a7-af38-77b69d61cd94", OrganizationID: r.OrganizationID, RegulationID: r.RegulationID, BuyerID: r.BuyerID, Lines: []SabyOrderRealizationPostLine{{NomenclatureID: r.Next.NomenclatureID, Quantity: r.Next.Quantity, UnitPrice: r.Next.UnitPrice}}}
}
func cancelReply() map[string]any {
	return map[string]any{"_type": "record", "f": 0, "s": []any{
		map[string]any{"n": "НеактуальныеПроводки", "t": "Логическое"}, map[string]any{"n": "ВОчередиНаПроведение", "t": "Логическое"}, map[string]any{"n": "ВОчередиНаРасчетПроводок", "t": "Логическое"}, map[string]any{"n": "operationResult", "t": "Логическое"}, map[string]any{"n": "message", "t": "Строка"},
	}, "d": []any{false, false, false, true, ""}}
}
func newCancelHarness(t *testing.T) (*SabyClient, *cancelHarness) {
	t.Helper()
	h := &cancelHarness{header: orderRealizationReadFixture(), line: fillLine(true)}
	for _, f := range []struct {
		name  string
		value any
	}{{"Лицо1", int64(41)}, {"Лицо3", int64(700)}} {
		h.header["s"] = append(h.header["s"].([]any), map[string]any{"n": f.name, "t": "Связь"})
		h.header["d"] = append(h.header["d"].([]any), f.value)
	}
	orderRealizationSet(h.line, "Закрыто", true)
	for _, raw := range h.line["s"].([]any) {
		f := raw.(map[string]any)
		if f["n"] == "Флаги" {
			f["t"].(map[string]any)["s"].(map[string]any)["24"] = "Непросчитано"
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Protocol int             `json:"protocol"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if r.URL.Path != "/service/" || json.NewDecoder(r.Body).Decode(&q) != nil {
			t.Error("unexpected auth/redirect transport")
			w.WriteHeader(400)
			return
		}
		var result any
		switch q.Method {
		case "ДокОтгрИсх.Прочитать":
			h.reads++
			if q.Protocol != 6 {
				t.Error("read protocol")
			}
			result = h.header
		case "РеалИсх.DocNomList":
			h.reads++
			if q.Protocol != 6 {
				t.Error("list protocol")
			}
			result = map[string]any{"_type": "recordset", "s": h.line["s"], "d": []any{h.line["d"]}}
		case "Документ.CancelPosting":
			h.calls++
			if q.Protocol != 7 {
				t.Error("cancel protocol")
			}
			p, err := sabyFillDecode(q.Params)
			if err != nil || len(p) != 2 || p["eventRec"] != nil {
				t.Error("wrong captured params")
			}
			rec, ok := p["docRec"].(map[string]any)
			if !ok || len(rec) != 4 || sabyValue(rec["f"]) != "0" || len(rec["s"].([]any)) != 1 || sabyValue(sabyRecordField(rec, "@Документ")) != "42737" {
				t.Error("wrong native record identity")
			}
			if h.status != 0 {
				if h.status == 302 {
					w.Header().Set("Location", "/unexpected-redirect")
				}
				w.WriteHeader(h.status)
				return
			}
			if !h.noClear {
				sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[10] = false
				sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[11] = false
				orderRealizationSet(h.line, "Закрыто", false)
			}
			if h.afterWrite != nil {
				h.afterWrite(h)
			}
			reply := cancelReply()
			if h.editReply != nil {
				h.editReply(reply)
			}
			result = reply
		default:
			t.Errorf("unexpected RPC %s", q.Method)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(srv.Close)
	c := NewSabyClient("fake", "fake", "fake", 278, 6)
	c.client = srv.Client()
	c.serviceURL = srv.URL + "/service/"
	c.token = "fake"
	c.tokenUntil = time.Now().Add(time.Hour)
	return c, h
}
func TestSabyOrderRealizationCancelRequiresFreshOwnedReadback(t *testing.T) {
	c, h := newCancelHarness(t)
	result, err := c.CancelOrderRealizationPosting(context.Background(), cancelRequest())
	if err != nil || result.Outcome != "unposted" || h.calls != 1 || h.reads != 6 {
		t.Fatalf("cancel %+v %v writes%d reads%d", result, err, h.calls, h.reads)
	}
	again, err := c.CancelOrderRealizationPosting(context.Background(), cancelRequest())
	if err != nil || again.Outcome != "already_unposted" || h.calls != 1 {
		t.Fatal("already cleared document repeated write", again, err, h.calls)
	}
}
func TestSabyOrderRealizationCancelRejectsUnownedOrChangedComposition(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*cancelHarness, *SabyOrderRealizationCancelRequest)
	}{
		{"public UUID", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			r.PublicUUID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{"native UUID", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			r.Identity.UUID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{"buyer", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.BuyerID++ }},
		{"organization", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.OrganizationID++ }},
		{"organization face", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			orderRealizationSet(h.header, "Лицо3", int64(701))
		}},
		{"regulation", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.RegulationID++ }},
		{"header warehouse", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.Identity.WarehouseID++ }},
		{"movement warehouse", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.Identity.MovementWarehouseID++ }},
		{"wrong canonical", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.Lines[0].NomenclatureID++ }},
		{"wrong quantity", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.Lines[0].Quantity = "1" }},
		{"wrong price", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.Lines[0].UnitPrice = "10.21" }},
		{"missing row IDs", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.ExpectedRowIDs = nil }},
		{"duplicate row IDs", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			r.ExpectedRowIDs = []int64{700001, 700001}
		}},
		{"replacement row ID", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			orderRealizationSet(h.line, "@НоменклатураДокумента", int64(700099))
		}},
		{"empty composition", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) { r.Lines = nil }},
		{"queued", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[18] = true
		}},
		{"partial entries", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[11] = false
		}},
		{"open posted line", func(h *cancelHarness, r *SabyOrderRealizationCancelRequest) {
			orderRealizationSet(h.line, "Закрыто", false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h := newCancelHarness(t)
			r := cancelRequest()
			tc.edit(h, &r)
			result, err := c.CancelOrderRealizationPosting(context.Background(), r)
			if err == nil || result.Outcome != "not_sent" || h.calls != 0 {
				t.Fatalf("unsafe cancel %+v %v writes%d", result, err, h.calls)
			}
		})
	}
}
func TestSabyOrderRealizationCancelUnknownNeverReplaysWrite(t *testing.T) {
	for _, mode := range []string{"401", "redirect", "false result", "null flag", "wrong schema", "message", "stale header", "open flag absent", "wrong final price", "replaced row", "nullable cleared flags"} {
		t.Run(mode, func(t *testing.T) {
			c, h := newCancelHarness(t)
			switch mode {
			case "401":
				h.status = 401
			case "redirect":
				h.status = 302
			case "false result":
				h.editReply = func(r map[string]any) { orderRealizationSet(r, "operationResult", false) }
			case "null flag":
				h.editReply = func(r map[string]any) { orderRealizationSet(r, "НеактуальныеПроводки", nil) }
			case "wrong schema":
				h.editReply = func(r map[string]any) { r["s"].([]any)[3].(map[string]any)["t"] = "Строка" }
			case "message":
				h.editReply = func(r map[string]any) { orderRealizationSet(r, "message", "unverified vendor details") }
			case "stale header":
				h.noClear = true
			case "open flag absent":
				h.afterWrite = func(h *cancelHarness) { orderRealizationSet(h.line, "Закрыто", nil) }
			case "replaced row":
				h.afterWrite = func(h *cancelHarness) {
					orderRealizationSet(h.line, "@НоменклатураДокумента", int64(700099))
				}
			case "nullable cleared flags":
				h.afterWrite = func(h *cancelHarness) {
					sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[10] = nil
				}
			case "wrong final price":
				h.afterWrite = func(h *cancelHarness) {
					orderRealizationSet(h.line, "Цена", json.Number("10.00"))
					orderRealizationSet(h.line, "СуммаЦен", json.Number("20.00"))
				}
			}
			result, err := c.CancelOrderRealizationPosting(context.Background(), cancelRequest())
			if err == nil || result.Outcome != "unknown" || h.calls != 1 {
				t.Fatalf("unknown cancellation replayed/accepted %+v %v writes%d", result, err, h.calls)
			}
		})
	}
}
