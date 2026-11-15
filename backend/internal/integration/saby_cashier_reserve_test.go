package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/cashierstock"
)

type cashierHarness struct {
	mu                     sync.Mutex
	provider               *SabyCashierReserveProvider
	header                 map[string]any
	rows                   []cashierInvoiceRow
	reserve                map[int64]int64
	writes                 map[string]int
	nextRow                int64
	delayReserveVisibility bool
	staleReserveReads      int
}

func cashierTestRequest() cashierstock.Request {
	return cashierstock.Request{ReservationID: 42, OrderID: 77, OrderNumber: "0000-77", Config: cashierstock.Config{WarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 259, RegulationID: 121326}, Lines: []cashierstock.Line{{NomenclatureID: 3899, Quantity: 1, UnitPrice: "1.00"}}}
}
func cashierTestRecord(fields map[string]any) map[string]any { return sabyRecord(fields) }
func cashierTestRow(row cashierInvoiceRow, prepared bool) map[string]any {
	var id any = row.ID
	var closed any = false
	if prepared {
		id = nil
		closed = nil
	}
	price := json.Number(row.Line.UnitPrice)
	return cashierTestRecord(map[string]any{"@НоменклатураДокумента": id, "Номенклатура": row.Line.NomenclatureID, "СкладскойДокумент": int64(42866), "Склад": int64(284), "Тип": int64(4), "Количество": row.Line.Quantity, "Цена": price, "СуммаЦен": json.Number(fmt.Sprintf("%.2f", float64(row.Line.Quantity)*mustCashierFloat(row.Line.UnitPrice))), "Закрыто": closed})
}
func mustCashierFloat(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
func cashierTestSet(records []map[string]any, empty map[string]any) map[string]any {
	schema := empty["s"]
	data := []any{}
	for _, record := range records {
		values := []any{}
		for _, field := range schema.([]any) {
			values = append(values, sabyRecordField(record, field.(map[string]any)["n"].(string)))
		}
		data = append(data, values)
	}
	return map[string]any{"_type": "recordset", "s": schema, "d": data}
}

func newCashierHarness(t *testing.T) *cashierHarness {
	t.Helper()
	h := &cashierHarness{reserve: map[int64]int64{}, writes: map[string]int{}, nextRow: 175790}
	h.header = orderRealizationReadFixture()
	for name, value := range map[string]any{"@Документ": int64(42866), "ИдентификаторДокумента": "01a1de0d-4726-7e92-98d8-5478ead64c28", "ТипДокумента": int64(231), "ТипДокумента.ТипДокумента": "СчетИсх", "ДокументНашаОрганизация": int64(259), "СкладскойДокумент.Склад": int64(283), "Регламент": int64(121326), "Лицо1": nil, "Номер": nil, "ДокументРасширение.Название": nil} {
		if !setSabyRecordField(h.header, name, value) {
			h.header["s"] = append(h.header["s"].([]any), map[string]any{"n": name, "t": "Строка"})
			h.header["d"] = append(h.header["d"].([]any), value)
		}
	}
	flags := sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)
	for i := range flags {
		flags[i] = nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		var q struct {
			Protocol int             `json:"protocol"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if r.URL.Path != "/service/" || json.NewDecoder(r.Body).Decode(&q) != nil || q.Protocol != 7 {
			t.Error("unexpected transport/protocol")
			w.WriteHeader(400)
			return
		}
		params, err := sabyFillDecode(q.Params)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var reply any
		switch q.Method {
		case "СчетСкИсх.Создать":
			h.writes[q.Method]++
			reply = h.header
		case "СчетСкИсх.ПрочитатьДляУчастника":
			reply = h.header
		case "СчетСкИсх.WriteForParticipant":
			h.writes[q.Method]++
			record := params["Запись"].(map[string]any)
			for _, name := range []string{"Номер", "ДокументРасширение.Название"} {
				setSabyRecordField(h.header, name, sabyRecordField(record, name))
			}
			if buyer := sabyRecordField(record, "Лицо1"); buyer != nil {
				setSabyRecordField(h.header, "Лицо1", buyer)
			}
			reply = true
		case "СчетСкИсх.DocNomList":
			records := []map[string]any{}
			for _, row := range h.rows {
				records = append(records, cashierTestRow(row, false))
			}
			reply = cashierTestSet(records, cashierTestRow(cashierInvoiceRow{Line: cashierTestRequest().Lines[0]}, false))
		case "СчетСкИсх.DocNomCreate":
			h.writes[q.Method]++
			filter := params["Фильтр"].(map[string]any)
			qty, _ := sabyInt64Field(filter, "Количество")
			sku, _ := sabyInt64Field(filter, "Номенклатура")
			reply = cashierTestRow(cashierInvoiceRow{Line: cashierstock.Line{NomenclatureID: sku, Quantity: qty, UnitPrice: sabyValue(sabyRecordField(filter, "Цена"))}}, true)
		case "СчетСкИсх.CustomDocNomUpdate":
			h.writes[q.Method]++
			record := params["Запись"].(map[string]any)
			row, e := cashierParseRow(record, cashierInvoiceIdentity{ID: 42866, Config: cashierTestRequest().Config}, true)
			if e != nil {
				t.Error(e)
			}
			h.nextRow++
			row.ID = h.nextRow
			h.rows = append(h.rows, row)
			reply = cashierTestRecord(map[string]any{"result": row.ID})
		case "СчетСкИсх.CustomDocNomDelete":
			h.writes[q.Method]++
			id, _ := strconv.ParseInt(params["ИдО"].([]any)[0].(string), 10, 64)
			if h.reserve[id] != 0 {
				t.Error("deletion reached a reserved row")
			}
			kept := h.rows[:0]
			for _, row := range h.rows {
				if row.ID != id {
					kept = append(kept, row)
				}
			}
			h.rows = kept
			reply = true
		case "Reserve.Apply":
			h.writes[q.Method]++
			id, _ := strconv.ParseInt(sabyValue(params["DocNomId"]), 10, 64)
			qty, _ := strconv.ParseInt(sabyValue(params["Count"]), 10, 64)
			h.reserve[id] = qty
			if h.delayReserveVisibility {
				h.staleReserveReads = 1
			}
			reply = []int64{175785, 175786}
		case "Reserve.Remove":
			h.writes[q.Method]++
			id, _ := strconv.ParseInt(sabyValue(params["DocNomId"]), 10, 64)
			delete(h.reserve, id)
			reply = true
		case "Reserve.ListByDn":
			id, _ := strconv.ParseInt(sabyValue(params["DocNomIds"].([]any)[0]), 10, 64)
			record := cashierTestRecord(map[string]any{"@НоменклатураДокумента": id, "Количество": h.reserve[id], "КоличествоОтгружено": int64(0), "Активен": true, "Склад": int64(283), "ЕстьСписаниеОтгрузкой": false, "ByBaseWh": true})
			records := []map[string]any{}
			if h.staleReserveReads > 0 {
				h.staleReserveReads--
			} else if h.reserve[id] > 0 {
				records = append(records, record)
			}
			reply = cashierTestSet(records, record)
		default:
			t.Errorf("forbidden/unverified RPC reached native server: %s", q.Method)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": reply})
	}))
	t.Cleanup(server.Close)
	c := NewSabyClient("fixture", "fixture", "fixture", 278, 19)
	c.client = server.Client()
	c.serviceURL = server.URL + "/service/"
	c.token = "fixture"
	c.tokenUntil = time.Now().Add(time.Hour)
	h.provider = &SabyCashierReserveProvider{Client: c}
	return h
}

func cashierDrive(t *testing.T, h *cashierHarness, r cashierstock.Request, progress cashierstock.Progress) cashierstock.Progress {
	t.Helper()
	for i := 0; i < 100; i++ {
		plan, err := h.provider.Next(context.Background(), r, progress)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Attention {
			t.Fatalf("attention at step %d: %s native=%s", i, plan.Message, plan.Native)
		}
		progress = cashierstock.Progress{Native: plan.Native}
		if plan.Done {
			return progress
		}
		if plan.Command != nil {
			progress.Command = plan.Command
			progress.Attempted = true
			progress.Reply, err = h.provider.Execute(context.Background(), *plan.Command)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("native reserve did not settle")
	return progress
}

func TestCashierNativeReservationCreateEditCancelWithoutPosting(t *testing.T) {
	h := newCashierHarness(t)
	r := cashierTestRequest()
	progress := cashierDrive(t, h, r, cashierstock.Progress{})
	if len(h.rows) != 1 || h.reserve[h.rows[0].ID] != 1 {
		t.Fatal("first native reservation missing")
	}
	r.Lines[0].Quantity = 2
	progress = cashierDrive(t, h, r, progress)
	if len(h.rows) != 1 || h.reserve[h.rows[0].ID] != 2 || h.writes["СчетСкИсх.Создать"] != 1 || h.writes["СчетСкИсх.CustomDocNomDelete"] != 1 {
		t.Fatalf("revision duplicated or did not replace native rows: %+v", h.writes)
	}
	r.Terminal = "cancelled"
	r.Lines = []cashierstock.Line{}
	cashierDrive(t, h, r, progress)
	if len(h.reserve) != 0 || len(h.rows) != 1 || h.writes["СчетСкИсх.Создать"] != 1 {
		t.Fatal("cancellation did not release the exact native reserve")
	}
}

func TestCashierNativeCompletionOnlyReleasesReservation(t *testing.T) {
	h := newCashierHarness(t)
	r := cashierTestRequest()
	progress := cashierDrive(t, h, r, cashierstock.Progress{})
	r.Terminal = "completed"
	r.Lines = []cashierstock.Line{}
	progress = cashierDrive(t, h, r, progress)
	before := cashierstock.Fingerprint(h.writes)
	cashierDrive(t, h, r, progress)
	if len(h.reserve) != 0 || before != cashierstock.Fingerprint(h.writes) {
		t.Fatal("completion replayed a native write")
	}
}

func TestCashierUnknownCreateAndSavedReplyNeverReplays(t *testing.T) {
	for _, kind := range []string{"create", "save-row"} {
		t.Run(kind, func(t *testing.T) {
			h := newCashierHarness(t)
			r := cashierTestRequest()
			progress := cashierstock.Progress{}
			for i := 0; i < 50; i++ {
				plan, err := h.provider.Next(context.Background(), r, progress)
				if err != nil || plan.Attention {
					t.Fatalf("plan failed: %v %s", err, plan.Message)
				}
				progress = cashierstock.Progress{Native: plan.Native}
				if plan.Command == nil {
					continue
				}
				reply, err := h.provider.Execute(context.Background(), *plan.Command)
				if err != nil {
					t.Fatal(err)
				}
				progress.Command = plan.Command
				progress.Attempted = true
				progress.Reply = reply
				if plan.Command.Kind != kind {
					continue
				}
				progress.Reply = nil
				progress.Error = "lost reply"
				before := cashierstock.Fingerprint(h.writes)
				for j := 0; j < 2; j++ {
					stopped, e := h.provider.Next(context.Background(), r, progress)
					if e != nil || !stopped.Attention || stopped.Command != nil {
						t.Fatal("ambiguous create/save was allowed to resume writes")
					}
				}
				if before != cashierstock.Fingerprint(h.writes) {
					t.Fatal("unknown native write replayed")
				}
				return
			}
			t.Fatal("requested native write not reached")
		})
	}
}

func TestCashierPreparedMutationRechecksNativeOwnership(t *testing.T) {
	h := newCashierHarness(t)
	r := cashierTestRequest()
	progress := cashierDrive(t, h, r, cashierstock.Progress{})
	r.Terminal = "cancelled"
	r.Lines = nil
	for i := 0; i < 15; i++ {
		plan, err := h.provider.Next(context.Background(), r, progress)
		if err != nil || plan.Attention {
			t.Fatalf("plan failed: %v %s", err, plan.Message)
		}
		progress = cashierstock.Progress{Native: plan.Native}
		if plan.Command == nil {
			continue
		}
		if plan.Command.Kind != "remove" {
			t.Fatal("expected release before other mutation")
		}
		setSabyRecordField(h.header, "ДокументРасширение.Название", "changed outside site")
		before := cashierstock.Fingerprint(h.writes)
		if _, err = h.provider.Execute(context.Background(), *plan.Command); err == nil {
			t.Fatal("modified remote invoice was written")
		}
		if before != cashierstock.Fingerprint(h.writes) {
			t.Fatal("ownership check ran after mutation")
		}
		return
	}
	t.Fatal("release command not prepared")
}

func TestCashierTerminalBeforeNativeCreationSendsNoRPC(t *testing.T) {
	h := newCashierHarness(t)
	r := cashierTestRequest()
	r.Terminal = "cancelled"
	r.Lines = nil
	cashierDrive(t, h, r, cashierstock.Progress{})
	if len(h.writes) != 0 {
		t.Fatal("cancelled order created a native invoice")
	}
}

func TestCashierLostAcknowledgementReconcilesOnlyByNativeReads(t *testing.T) {
	for _, kind := range []string{"header", "apply", "remove", "delete-row"} {
		t.Run(kind, func(t *testing.T) {
			h := newCashierHarness(t)
			r := cashierTestRequest()
			progress := cashierstock.Progress{}
			if kind == "remove" || kind == "delete-row" {
				progress = cashierDrive(t, h, r, progress)
				if kind == "remove" {
					r.Terminal = "cancelled"
					r.Lines = nil
				} else {
					r.Lines[0].Quantity = 2
				}
			}
			method := map[string]string{"header": "СчетСкИсх.WriteForParticipant", "apply": "Reserve.Apply", "remove": "Reserve.Remove", "delete-row": "СчетСкИсх.CustomDocNomDelete"}[kind]
			for i := 0; i < 80; i++ {
				plan, err := h.provider.Next(context.Background(), r, progress)
				if err != nil || plan.Attention {
					t.Fatalf("plan failed: %v %s", err, plan.Message)
				}
				progress = cashierstock.Progress{Native: plan.Native}
				if plan.Command == nil {
					continue
				}
				reply, err := h.provider.Execute(context.Background(), *plan.Command)
				if err != nil {
					t.Fatal(err)
				}
				progress.Command = plan.Command
				progress.Attempted = true
				progress.Reply = reply
				if plan.Command.Kind != kind {
					continue
				}
				before := h.writes[method]
				progress.Reply = nil
				progress.Error = "lost acknowledgement"
				cashierDrive(t, h, r, progress)
				if h.writes[method] != before {
					t.Fatalf("lost %s acknowledgement repeated a mutation", kind)
				}
				return
			}
			t.Fatal("expected native command not reached")
		})
	}
}

func TestCashierDelayedReservationVisibilityKeepsReadOnlyVerificationStage(t *testing.T) {
	h := newCashierHarness(t)
	h.delayReserveVisibility = true
	r := cashierTestRequest()
	progress := cashierstock.Progress{}
	waits := 0
	for i := 0; i < 100; i++ {
		plan, err := h.provider.Next(context.Background(), r, progress)
		if err != nil || plan.Attention {
			t.Fatalf("stale read lost verification stage: %v %s", err, plan.Message)
		}
		if plan.RetryAfter > 0 {
			waits++
			if plan.Command != nil {
				t.Fatal("verification wait prepared another mutation")
			}
		}
		progress = cashierstock.Progress{Native: plan.Native}
		if plan.Done {
			if waits != 1 || h.writes["Reserve.Apply"] != 1 {
				t.Fatal("stale native read repeated apply or did not wait")
			}
			return
		}
		if plan.Command != nil {
			progress.Command = plan.Command
			progress.Attempted = true
			progress.Reply, err = h.provider.Execute(context.Background(), *plan.Command)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("delayed reservation never settled")
}
