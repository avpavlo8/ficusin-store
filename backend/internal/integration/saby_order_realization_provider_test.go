package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

type realizationProviderHarness struct {
	mu                                sync.Mutex
	public                            map[string]any
	header                            map[string]any
	rows                              []any
	writes                            map[string]int
	staleAfterSave                    int
	staleRemaining                    int
	queueAfterPost                    int
	postRequested                     bool
	loseCreate, loseSave, foreignOnly bool
	provider                          *SabyOrderRealizationProvider
	publicReadError                   map[string]any
}

func realizationProviderRequest() ordersale.Request {
	return ordersale.Request{BuyerID: 41, IntentID: 901, SiteWarehouseID: 2, NativeWarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 700, RegulationID: 122498, OperationKey: "1eacfa69-a5be-4790-8b9a-a198b3493220", ExecutionMode: "live",
		Event: ordersale.Event{Key: "order:77", OrderID: 77, OrderNumber: "0000-77", CanPrepare: true, Fingerprint: strings.Repeat("a", 64), Lines: []ordersale.Line{{OrderItemID: 99, VariantID: 11, SabyID: "1101", Quantity: 2, UnitPrice: "10.20"}}}}
}

func realizationProviderLine(saved bool) map[string]any {
	row := fillLine(saved)
	for _, raw := range row["s"].([]any) {
		field := raw.(map[string]any)
		if field["n"] == "Флаги" {
			field["t"] = map[string]any{"n": "Флаги", "s": map[string]any{"0": "Удален", "2": "Непросчитано", "24": "Оплачено"}}
		}
	}
	setSabyRecordField(row, "Флаги", []any{false, false, nil})
	return row
}

func realizationProviderSet(rows []any) map[string]any {
	return map[string]any{"_type": "recordset", "s": realizationProviderLine(true)["s"], "d": rows}
}

func newRealizationProviderHarness(t *testing.T) *realizationProviderHarness {
	t.Helper()
	h := &realizationProviderHarness{header: orderRealizationReadFixture(), rows: []any{}, writes: map[string]int{}}
	orderRealizationSet(h.header, "Регламент", int64(122498))
	orderRealizationSet(h.header, "ДокументРасширение.ИдентификаторВИ", realizationProviderRequest().OperationKey)
	flags := sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)
	for _, i := range []int{10, 11, 18, 27} {
		flags[i] = nil
	}
	for _, f := range []struct {
		name  string
		value any
	}{{"Лицо1", nil}, {"Лицо3", int64(700)}, {"Дата", "2026-11-13"}} {
		h.header["s"] = append(h.header["s"].([]any), map[string]any{"n": f.name, "t": "Связь"})
		h.header["d"] = append(h.header["d"].([]any), f.value)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		var q struct {
			Protocol int             `json:"protocol"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if r.URL.Path != "/service/" || json.NewDecoder(r.Body).Decode(&q) != nil {
			t.Error("unexpected auth or RPC")
			w.WriteHeader(400)
			return
		}
		params, e := sabyFillDecode(q.Params)
		if e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		var reply any
		switch q.Method {
		case "sabyWarehouse.List":
			reply = map[string]any{"warehouses": []any{map[string]any{"warehouseId": int64(283), "organization": map[string]any{"inn": "123456789012"}}}}
		case "СБИС.ПрочитатьДокумент":
			if h.public == nil {
				failure := h.publicReadError
				if failure == nil {
					failure = map[string]any{"code": -32000, "data": map[string]any{"classid": "{00000000-0000-0000-0000-1fa000010007}", "error_code": -1}, "message": "Не найден документ с идентификатором '" + realizationProviderRequest().OperationKey + "', подробнее в базе знаний (https://saby.ru/help/integration/api/error_solution/not_doc)"}
				}
				json.NewEncoder(w).Encode(map[string]any{"error": failure})
				return
			}
			reply = h.public
		case "СБИС.ЗаписатьДокумент":
			h.writes[q.Method]++
			if q.Protocol != 3 {
				t.Error("public create protocol changed")
			}
			h.public = params["Документ"].(map[string]any)
			h.public["Регламент"] = map[string]any{"Название": "Реализация", "Идентификатор": "8861254e-4354-11e1-9d64-005056a63fed"}
			if h.public["Идентификатор"] != realizationProviderRequest().OperationKey || h.public["Тип"] != "ДокОтгрИсх" {
				t.Error("create identity changed")
			}
			if _, ok := h.public["Контрагент"]; ok {
				t.Error("unverified public buyer mapping was sent")
			}
			if h.loseCreate {
				connection, _, _ := w.(http.Hijacker).Hijack()
				connection.Close()
				return
			}
			// The response need only return the durable UUID; full verification is RO.
			reply = map[string]any{"Идентификатор": h.public["Идентификатор"]}
		case "ДокОтгрИсх.СписокХраним":
			id := int64(42737)
			if h.foreignOnly {
				id = 999
			}
			reply = map[string]any{"_type": "recordset", "s": []any{map[string]any{"n": "@Документ", "t": "Идентификатор"}}, "d": []any{[]any{[]any{id, "ДокОтгрИсх"}}}}
		case "ДокОтгрИсх.Прочитать":
			var id int64
			raw, _ := json.Marshal(params["ИдО"])
			json.Unmarshal(raw, &id)
			if h.foreignOnly && id == 999 {
				copyRaw, _ := json.Marshal(h.header)
				copyDoc, _ := sabyFillDecode(copyRaw)
				setSabyRecordField(copyDoc, "@Документ", int64(999))
				setSabyRecordField(copyDoc, "ДокументРасширение.ИдентификаторВИ", "ffffffff-ffff-4fff-8fff-ffffffffffff")
				reply = copyDoc
				break
			}
			if h.postRequested {
				flags := sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)
				if h.queueAfterPost > 0 {
					h.queueAfterPost--
					flags[10], flags[11], flags[18], flags[27] = nil, nil, true, true
				} else {
					flags[10], flags[11], flags[18], flags[27] = true, true, false, false
					for _, raw := range h.rows {
						raw.([]any)[6] = true
					}
				}
			}
			reply = h.header
		case "РеалИсх.DocNomList":
			rows := h.rows
			if h.staleRemaining > 0 {
				h.staleRemaining--
				rows = []any{}
			}
			reply = realizationProviderSet(rows)
		case "РеалИсх.Записать":
			h.writes[q.Method]++
			if q.Protocol != 7 {
				t.Error("buyer bind protocol changed")
			}
			record := params["Запись"].(map[string]any)
			if len(record["s"].([]any)) != 2 {
				t.Error("buyer binding sends unrelated fields")
			}
			id, _ := sabyInt64Field(record, "@Документ")
			buyer, _ := sabyInt64Field(record, "Лицо1")
			if id != 42737 || buyer != 41 {
				t.Error("buyer binding identity changed")
			}
			setSabyRecordField(h.header, "Лицо1", buyer)
			reply = id
		case "РеалИсх.DocNomCreate":
			h.writes[q.Method]++
			if q.Protocol != 7 {
				t.Error("prepare protocol changed")
			}
			reply = realizationProviderLine(false)
		case "РеалИсх.CustomDocNomUpdate":
			h.writes[q.Method]++
			if q.Protocol != 7 {
				t.Error("save protocol changed")
			}
			record := params["Запись"].(map[string]any)
			if sabyValue(sabyRecordField(record, "PreservedNativeNumber")) != "9007199254740993" {
				t.Error("native number lost precision")
			}
			saved := realizationProviderLine(true)
			h.rows = []any{saved["d"]}
			h.staleRemaining = h.staleAfterSave
			if h.loseSave {
				connection, _, _ := w.(http.Hijacker).Hijack()
				connection.Close()
				return
			}
			reply = sabyRecord(map[string]any{"result": int64(700001), "details": map[string]any{}, "nav_res": "", "new_rs": realizationProviderSet(h.rows), "upd_rs": map[string]any{"_type": "recordset", "s": []any{}, "d": []any{}}, "del_rs": nil, "missed_rs": nil, "data": nil, "total_rec": sabyRecord(map[string]any{})})
			for _, raw := range reply.(map[string]any)["s"].([]any) {
				field := raw.(map[string]any)
				field["t"] = map[string]string{"result": "Число целое", "details": "JSON-объект", "nav_res": "Строка", "new_rs": "Выборка", "upd_rs": "Выборка", "del_rs": "Выборка", "missed_rs": "Выборка", "data": "Запись", "total_rec": "Запись"}[field["n"].(string)]
			}
		case "Документ.PostAsync":
			h.writes[q.Method]++
			if q.Protocol != 7 {
				t.Error("posting protocol changed")
			}
			h.postRequested = true
			reply = nil // Actual live successful asynchronous reply.
		default:
			t.Errorf("unverified RPC %s", q.Method)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": reply})
	}))
	t.Cleanup(server.Close)
	c := NewSabyClient("fixture", "fixture", "fixture", 278, 6)
	c.client = server.Client()
	c.serviceURL = server.URL + "/service/"
	c.authURL = server.URL + "/auth/"
	c.token = "fixture-token"
	c.tokenUntil = time.Now().Add(time.Hour)
	h.provider = &SabyOrderRealizationProvider{Client: c, PublicRegulationUUID: "8861254e-4354-11e1-9d64-005056a63fed", Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }}
	return h
}

func providerWriteCount(h *realizationProviderHarness) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.writes {
		n += c
	}
	return n
}

func runProviderStep(t *testing.T, h *realizationProviderHarness, r ordersale.Request, p ordersale.Progress) ordersale.Result {
	t.Helper()
	before := providerWriteCount(h)
	result, err := h.provider.Step(context.Background(), r, p)
	if err != nil {
		t.Fatal(err)
	}
	writes := providerWriteCount(h) - before
	if writes > 1 || writes > 0 && p.Stage != "create" && p.Stage != "fill" && p.Stage != "post" {
		t.Fatalf("provider violated durable one-write stage boundary: %s %d", p.Stage, writes)
	}
	// Recreate persisted JSON at every boundary, retaining native integer values.
	raw, _ := json.Marshal(result.Progress)
	if json.Unmarshal(raw, &result.Progress) != nil {
		t.Fatal("invalid persisted progress")
	}
	return result
}

func TestSabyRealizationProviderActualNullAsyncReceiptAndStaleSavedReadback(t *testing.T) {
	h := newRealizationProviderHarness(t)
	h.staleAfterSave = 2
	h.queueAfterPost = 2
	r := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect"}
	var final ordersale.Result
	acknowledged := false
	for i := 0; i < 35; i++ {
		final = runProviderStep(t, h, r, p)
		p = final.Progress
		var native sabyRealizationProviderReceipt
		json.Unmarshal(p.Native, &native)
		if native.Phase == "saved" && native.SavedRowID == 700001 {
			acknowledged = true
		}
		if final.State != "queued" {
			break
		}
	}
	if final.State != "posted" || !acknowledged || !final.StockVerified || final.Progress.Unknown || len(final.StockProof) == 0 {
		t.Fatalf("real flow did not reach independently verified posting: %s %s", final.State, final.Message)
	}
	for _, method := range []string{"СБИС.ЗаписатьДокумент", "РеалИсх.Записать", "РеалИсх.DocNomCreate", "РеалИсх.CustomDocNomUpdate", "Документ.PostAsync"} {
		if h.writeCount(method) != 1 {
			t.Fatalf("write %s repeated or omitted: %d", method, h.writeCount(method))
		}
	}
	var proof ordersale.StockProof
	if json.Unmarshal(final.StockProof, &proof) != nil || proof.Test || proof.DocumentID != "42737" || proof.DocumentUUID == r.OperationKey || len(proof.Lines) != 1 || proof.Lines[0].UnitPrice != "10.20" || proof.Lines[0].LineSum != "20.40" || !proof.Lines[0].Closed {
		t.Fatalf("proof did not retain separate native identity and vendor prices: %+v", proof)
	}
}

func TestSabyRealizationProviderLostCreateStopsWithoutSecondDocument(t *testing.T) {
	h := newRealizationProviderHarness(t)
	h.loseCreate = true
	r := realizationProviderRequest()
	prepared := runProviderStep(t, h, r, ordersale.Progress{Stage: "inspect"})
	result := runProviderStep(t, h, r, prepared.Progress)
	if result.State != "needs_attention" || !result.Progress.Unknown || result.Progress.DocumentID != "" || h.writeCount("СБИС.ЗаписатьДокумент") != 1 {
		t.Fatalf("unknown create was not stopped: %+v", result)
	}
	for i := 0; i < 3; i++ {
		result = runProviderStep(t, h, r, result.Progress)
	}
	if h.writeCount("СБИС.ЗаписатьДокумент") != 1 {
		t.Fatal("unknown create replayed")
	}
}

func TestSabyRealizationProviderLostSavedReplyReconcilesWithoutDuplicate(t *testing.T) {
	h := newRealizationProviderHarness(t)
	h.loseSave = true
	h.staleAfterSave = 2
	r := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect"}
	var result ordersale.Result
	for i := 0; i < 35; i++ {
		result = runProviderStep(t, h, r, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "posted" || h.writeCount("РеалИсх.CustomDocNomUpdate") != 1 || h.writeCount("РеалИсх.DocNomCreate") != 1 {
		t.Fatalf("lost save duplicated or stalled: %s %s writes%v", result.State, result.Message, h.writes)
	}
}

func TestSabyRealizationProviderNativeDiscoveryNeverUsesNumberAlone(t *testing.T) {
	h := newRealizationProviderHarness(t)
	h.foreignOnly = true
	r := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect"}
	var result ordersale.Result
	for i := 0; i < 20; i++ {
		result = runProviderStep(t, h, r, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "needs_attention" || p.DocumentID != "" || providerWriteCount(h) != 1 {
		t.Fatalf("foreign native document was adopted or polled forever: %+v writes%v", result, h.writes)
	}
}

func TestSabyRealizationProviderChangedRequestOrMissingBuyerNeverWrites(t *testing.T) {
	for _, mode := range []string{"buyer", "mode", "changed-request", "wrong-regulation"} {
		t.Run(mode, func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			p := ordersale.Progress{Stage: "inspect"}
			if mode == "changed-request" {
				p = runProviderStep(t, h, r, p).Progress
				r.Event.Lines[0].Quantity++
			}
			if mode == "buyer" {
				r.BuyerID = 0
			}
			if mode == "mode" {
				r.ExecutionMode = "mock"
			}
			if mode == "wrong-regulation" {
				h.provider.PublicRegulationUUID = ""
			}
			result := runProviderStep(t, h, r, p)
			if result.State != "needs_attention" || providerWriteCount(h) != 0 {
				t.Fatalf("invalid request reached writes: %+v", result)
			}
		})
	}
}

func TestSabyRealizationProviderGroupedCanonicalPriceAndWholeQuantity(t *testing.T) {
	lines := []ordersale.Line{{SabyID: "1101", Quantity: 1, UnitPrice: "0.10"}, {SabyID: "1101", Quantity: 2, UnitPrice: "0.1"}, {SabyID: "1101", Quantity: 1, UnitPrice: "0.20"}}
	plan, err := sabyRealizationProviderPlan(lines)
	if err != nil || len(plan) != 2 {
		t.Fatal(plan, err)
	}
	got := map[string]string{}
	for _, l := range plan {
		got[string(l.UnitPrice)] = string(l.Quantity)
	}
	if got["0.10"] != "3" || got["0.20"] != "1" {
		t.Fatal("different prices merged or exact duplicate quantities lost", got)
	}
	for _, bad := range []string{"X1101", "01101", "0", "-1"} {
		lines[0].SabyID = bad
		if _, err = sabyRealizationProviderPlan(lines); err == nil {
			t.Fatal("noncanonical identity accepted", bad)
		}
	}
}

func TestSabyRealizationProviderAcknowledgedSaveNeverReaddsAfterTenStalePolls(t *testing.T) {
	h := newRealizationProviderHarness(t)
	h.staleAfterSave = 30
	r := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect"}
	var result ordersale.Result
	for i := 0; i < 35; i++ {
		result = runProviderStep(t, h, r, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "needs_attention" || h.writeCount("РеалИсх.CustomDocNomUpdate") != 1 || h.writeCount("РеалИсх.DocNomCreate") != 1 || h.writeCount("Документ.PostAsync") != 0 {
		t.Fatalf("stale acknowledged save was replayed or waited forever: %+v %v", result, h.writes)
	}
	var native sabyRealizationProviderReceipt
	json.Unmarshal(p.Native, &native)
	if native.Polls != 10 {
		t.Fatal("readback limit not retained", native.Polls)
	}
}

func TestSabyRealizationProviderPreparedCreateCannotSmuggleDifferentUUIDOrPosting(t *testing.T) {
	for _, key := range []string{"Идентификатор", "Провести"} {
		t.Run(key, func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			p := runProviderStep(t, h, r, ordersale.Progress{Stage: "inspect"}).Progress
			var native sabyRealizationProviderReceipt
			json.Unmarshal(p.Native, &native)
			var doc map[string]any
			json.Unmarshal(native.CreatePayload, &doc)
			if key == "Идентификатор" {
				doc[key] = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			} else {
				doc[key] = "Да"
			}
			native.CreatePayload, _ = json.Marshal(doc)
			p.Native, _ = json.Marshal(native)
			result := runProviderStep(t, h, r, p)
			if result.State != "needs_attention" || providerWriteCount(h) != 0 {
				t.Fatal("tampered durable create payload reached vendor", result)
			}
		})
	}
}

func TestSabyRealizationProviderExpiredPreparationLeaseIsReadOnly(t *testing.T) {
	h := newRealizationProviderHarness(t)
	r := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect"}
	for i := 0; i < 15; i++ {
		var native sabyRealizationProviderReceipt
		json.Unmarshal(p.Native, &native)
		if p.Stage == "fill" && native.Phase == "prepare" {
			break
		}
		p = runProviderStep(t, h, r, p).Progress
	}
	p.Stage, p.Unknown = "reconcile-fill", true
	before := providerWriteCount(h)
	result := runProviderStep(t, h, r, p)
	if result.State != "needs_attention" || providerWriteCount(h) != before || h.writeCount("РеалИсх.DocNomCreate") != 0 {
		t.Fatal("expired uncertain preparation was repeated", result, h.writes)
	}
}

func TestSabyRealizationProviderExpiredPostLeaseOnlyVerifiesClosedMovement(t *testing.T) {
	h := newRealizationProviderHarness(t)
	r := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect"}
	for i := 0; i < 20; i++ {
		if p.Stage == "post" {
			break
		}
		p = runProviderStep(t, h, r, p).Progress
	}
	if p.Stage != "post" {
		t.Fatal("did not reach post boundary", p.Stage)
	}
	// Simulate the external operation completing before the process committed its
	// response. Only the earlier durable post-stage receipt survived the crash.
	h.mu.Lock()
	h.postRequested = true
	h.writes["Документ.PostAsync"] = 1
	h.mu.Unlock()
	p.Stage, p.Unknown = "reconcile-post", true
	result := runProviderStep(t, h, r, p)
	if result.State != "posted" || !result.StockVerified || h.writeCount("Документ.PostAsync") != 1 {
		t.Fatal("expired post lease repeated a stock mutation", result, h.writes)
	}
}

func TestSabyRealizationProviderAcknowledgedRowCannotBeReplacedByEqualGroup(t *testing.T) {
	for _, phase := range []string{"saved", "known-prefix"} {
		t.Run(phase, func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			p := ordersale.Progress{Stage: "inspect"}
			reached := false
			for i := 0; i < 20; i++ {
				var native sabyRealizationProviderReceipt
				json.Unmarshal(p.Native, &native)
				if phase == "saved" && p.Stage == "verify-fill" && native.Phase == "saved" || phase == "known-prefix" && p.Stage == "post" {
					reached = true
					break
				}
				p = runProviderStep(t, h, r, p).Progress
			}
			if !reached {
				t.Fatalf("did not reach acknowledged save boundary: stage=%s native=%s", p.Stage, p.Native)
			}
			h.mu.Lock()
			if len(h.rows) != 1 {
				h.mu.Unlock()
				t.Fatal("did not reach saved row boundary")
			}
			h.rows[0].([]any)[0] = int64(700099)
			h.mu.Unlock()
			var result ordersale.Result
			for i := 0; i < 12; i++ {
				result = runProviderStep(t, h, r, p)
				p = result.Progress
				if result.State != "queued" {
					break
				}
			}
			if result.State != "needs_attention" || h.writeCount("Документ.PostAsync") != 0 || h.writeCount("РеалИсх.CustomDocNomUpdate") != 1 || h.writeCount("РеалИсх.DocNomCreate") != 1 {
				t.Fatalf("equal totals replaced acknowledged native identity: %+v %v", result, h.writes)
			}
		})
	}
}

func TestSabyRealizationProviderOnlyActualDocumentNotFoundPermitsCreate(t *testing.T) {
	for _, mode := range []string{"actual", "wrong-code", "wrong-class", "different-uuid", "auth", "unrelated-not-found"} {
		t.Run(mode, func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			errorData := map[string]any{"classid": "{00000000-0000-0000-0000-1fa000010007}", "error_code": -1}
			errorBody := map[string]any{"code": -32000, "data": errorData, "message": "Не найден документ с идентификатором '" + r.OperationKey + "', подробнее в базе знаний (https://saby.ru/help/integration/api/error_solution/not_doc)"}
			switch mode {
			case "wrong-code":
				errorBody["code"] = -32001
			case "wrong-class":
				errorData["classid"] = "unrelated"
			case "different-uuid":
				errorBody["message"] = "Не найден документ с идентификатором 'ffffffff-ffff-4fff-8fff-ffffffffffff', подробнее в базе знаний (https://saby.ru/help/integration/api/error_solution/not_doc)"
			case "auth":
				errorBody["message"] = "Нет прав доступа"
			case "unrelated-not-found":
				errorBody["message"] = "Не найден сервер"
			}
			h.publicReadError = errorBody
			result := runProviderStep(t, h, r, ordersale.Progress{Stage: "inspect"})
			if mode == "actual" {
				if result.Progress.Stage != "create" || result.State != "queued" {
					t.Fatal("verified vendor absence did not prepare creation", result)
				}
			} else if result.State != "needs_attention" {
				t.Fatal("unrelated read error was treated as permission to create", result)
			}
			if providerWriteCount(h) != 0 {
				t.Fatal("read preflight wrote a document")
			}
		})
	}
}

func (h *realizationProviderHarness) writeCount(method string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writes[method]
}
