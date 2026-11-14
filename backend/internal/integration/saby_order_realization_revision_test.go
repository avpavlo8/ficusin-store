package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

type revisionHarness struct {
	base                                                        *realizationProviderHarness
	mu                                                          sync.Mutex
	writes                                                      map[string]int
	nextID                                                      int64
	loseDelete, loseSave, loseUnpost, staleDelete, replaceSaved bool
	deleted                                                     []any
}

func revisionNativeReply(values map[string]any) map[string]any {
	record := sabyRecord(values)
	kinds := map[string]string{"result": "Число целое", "details": "JSON-объект", "nav_res": "Строка", "new_rs": "Выборка", "upd_rs": "Выборка", "del_rs": "Выборка", "missed_rs": "Выборка", "data": "Запись", "total_rec": "Запись"}
	for _, raw := range record["s"].([]any) {
		f := raw.(map[string]any)
		f["t"] = kinds[f["n"].(string)]
	}
	return record
}

func newRevisionHarness(t *testing.T) *revisionHarness {
	t.Helper()
	h := &revisionHarness{base: newRealizationProviderHarness(t), writes: map[string]int{}, nextID: 800000}
	baseURL := h.base.provider.Client.serviceURL
	baseClient := h.base.provider.Client.client
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var query struct {
			Method   string          `json:"method"`
			Protocol int             `json:"protocol"`
			Params   json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &query) != nil {
			t.Error("bad query")
			w.WriteHeader(400)
			return
		}
		params, err := sabyFillDecode(query.Params)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		h.mu.Lock()
		var reply any
		handled := true
		lose := false
		switch query.Method {
		case "РеалИсх.CustomDocNomDelete":
			h.writes[query.Method]++
			if query.Protocol != 7 {
				t.Error("delete protocol")
			}
			ids, _ := params["ИдО"].([]any)
			additional, _ := params["ДопПоля"].(map[string]any)
			if len(ids) != 1 || sabyValue(additional["СкладскойДокумент"]) != "42737" || additional["new_list"] != true {
				t.Error("delete binding changed")
			}
			id, _ := strconv.ParseInt(sabyValue(ids[0]), 10, 64)
			h.base.mu.Lock()
			kept, deleted := []any{}, []any{}
			for _, raw := range h.base.rows {
				if sabyValue(raw.([]any)[0]) == strconv.FormatInt(id, 10) {
					deleted = append(deleted, raw)
				} else {
					kept = append(kept, raw)
				}
			}
			if !h.staleDelete {
				h.base.rows = kept
			} else {
				h.deleted = kept
			}
			h.base.mu.Unlock()
			if len(deleted) != 1 {
				t.Error("wrong owned row removed")
			}
			reply = revisionNativeReply(map[string]any{"result": int64(1), "details": map[string]any{}, "nav_res": "", "new_rs": nil, "upd_rs": nil, "del_rs": realizationProviderSet(deleted), "missed_rs": nil, "data": nil, "total_rec": sabyRecord(map[string]any{})})
			lose = h.loseDelete
		case "РеалИсх.DocNomCreate":
			h.writes[query.Method]++
			filter := params["Фильтр"].(map[string]any)
			row := realizationProviderLine(false)
			for _, name := range []string{"Номенклатура", "Количество", "Цена"} {
				value := sabyRecordField(filter, name)
				if name == "Цена" {
					value = json.Number(sabyValue(value))
				}
				setSabyRecordField(row, name, value)
			}
			q, _ := new(big.Rat).SetString(sabyValue(sabyRecordField(row, "Количество")))
			p, _ := new(big.Rat).SetString(sabyValue(sabyRecordField(row, "Цена")))
			setSabyRecordField(row, "СуммаЦен", json.Number(new(big.Rat).Mul(q, p).FloatString(2)))
			reply = row
		case "РеалИсх.CustomDocNomUpdate":
			h.writes[query.Method]++
			h.nextID++
			row := params["Запись"].(map[string]any)
			setSabyRecordField(row, "@НоменклатураДокумента", json.Number(strconv.FormatInt(h.nextID, 10)))
			setSabyRecordField(row, "Закрыто", false)
			id := h.nextID
			h.base.mu.Lock()
			h.base.rows = append(h.base.rows, row["d"])
			h.base.mu.Unlock()
			reply = revisionNativeReply(map[string]any{"result": id, "details": map[string]any{}, "nav_res": "", "new_rs": realizationProviderSet([]any{row["d"]}), "upd_rs": map[string]any{"_type": "recordset", "s": []any{}, "d": []any{}}, "del_rs": nil, "missed_rs": nil, "data": nil, "total_rec": sabyRecord(map[string]any{})})
			lose = h.loseSave
		case "Документ.PostAsync":
			h.writes[query.Method]++
			h.base.mu.Lock()
			h.base.cancelRequested = false
			h.base.postRequested = true
			h.base.mu.Unlock()
			reply = nil
		case "Документ.CancelPosting":
			h.writes[query.Method]++
			h.base.mu.Lock()
			h.base.cancelRequested = true
			h.base.postRequested = false
			h.base.mu.Unlock()
			reply = sabyRecord(map[string]any{"НеактуальныеПроводки": false, "ВОчередиНаПроведение": false, "ВОчередиНаРасчетПроводок": false, "operationResult": true, "message": ""})
			lose = h.loseUnpost
		default:
			handled = false
		}
		h.mu.Unlock()
		if handled {
			if lose {
				c, _, _ := w.(http.Hijacker).Hijack()
				c.Close()
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": reply, "id": 1})
			return
		}
		forward, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, baseURL, bytes.NewReader(body))
		response, err := baseClient.Do(forward)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	}))
	t.Cleanup(server.Close)
	h.base.provider.Client.serviceURL = server.URL + "/service/"
	h.base.provider.Client.client = server.Client()
	return h
}

func (h *revisionHarness) count(method string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writes[method]
}

func revisionCommand(t *testing.T, h *revisionHarness) (ordersale.RevisionCommand, ordersale.Progress) {
	return revisionCommandFor(t, h, realizationProviderRequest())
}

func revisionCommandFor(t *testing.T, h *revisionHarness, r ordersale.Request) (ordersale.RevisionCommand, ordersale.Progress) {
	t.Helper()
	p := ordersale.Progress{Stage: "inspect", DesiredState: "posted"}
	var result ordersale.Result
	for i := 0; i < 30; i++ {
		if p.Stage == "create" {
			p.CreateAttempted = true
		}
		if p.Stage == "post" {
			p.PostAttempted = true
		}
		var err error
		result, err = h.base.provider.Step(context.Background(), r, p)
		if err != nil {
			t.Fatal(err)
		}
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "posted" {
		t.Fatalf("origin not posted: %s %s", result.State, result.Message)
	}
	target := r
	target.RevisionID = 1
	target.Event.Fingerprint = strings.Repeat("b", 64)
	target.Event.Lines = []ordersale.Line{{OrderItemID: 100, VariantID: 11, SabyID: "1101", Quantity: 3, UnitPrice: "8.25"}}
	p.ActiveOwnedRowIDs = append([]int64(nil), p.OwnedRowIDs...)
	p.Stage = "revision-inspect"
	return ordersale.RevisionCommand{Origin: r, Current: r, Target: target, RevisionID: 1}, p
}

func TestSabyRealizationRevisionReplacesEveryOwnedGroupAndRetainsHistory(t *testing.T) {
	h := newRevisionHarness(t)
	origin := realizationProviderRequest()
	origin.Event.Lines = append(origin.Event.Lines, ordersale.Line{OrderItemID: 101, VariantID: 12, SabyID: "1102", Quantity: 1, UnitPrice: "0.00"})
	c, p := revisionCommandFor(t, h, origin)
	c.Target.Event.Lines = append(c.Target.Event.Lines, ordersale.Line{OrderItemID: 102, VariantID: 12, SabyID: "1102", Quantity: 2, UnitPrice: "3.15"})
	var result ordersale.Result
	for i := 0; i < 75; i++ {
		result = revisionStep(t, h, c, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "posted" || h.count("РеалИсх.CustomDocNomDelete") != 2 || len(p.ActiveOwnedRowIDs) != 2 || len(p.OwnedRowIDs) != 4 || len(p.RetiredRowIDs) != 2 {
		t.Fatalf("multirow replacement incomplete: %s %s owned=%v retired=%v", result.State, result.Message, p.OwnedRowIDs, p.RetiredRowIDs)
	}
	// A second edit carries the last applied request, not the origin contents.
	next := c.Target
	next.RevisionID = 2
	next.Event.Fingerprint = strings.Repeat("c", 64)
	next.Event.Lines = []ordersale.Line{{OrderItemID: 103, VariantID: 12, SabyID: "1102", Quantity: 1, UnitPrice: "1.11"}}
	c = ordersale.RevisionCommand{Origin: origin, Current: c.Target, Target: next, RevisionID: 2, CurrentRevisionID: 1}
	p.Stage, p.RevisionNative, p.RevisionPostAttempted, p.RevisionUnpostAttempted, p.RevisionUnpostVerified = "revision-inspect", nil, false, false, false
	for i := 0; i < 60; i++ {
		result = revisionStep(t, h, c, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "posted" || h.count("РеалИсх.CustomDocNomDelete") != 4 || len(p.OwnedRowIDs) != 5 || len(p.RetiredRowIDs) != 4 || len(p.ActiveOwnedRowIDs) != 1 || h.base.writeCount("СБИС.ЗаписатьДокумент") != 1 {
		t.Fatalf("next revision lost history/identity: %s %s", result.State, result.Message)
	}
}

func revisionStep(t *testing.T, h *revisionHarness, c ordersale.RevisionCommand, p ordersale.Progress) ordersale.Result {
	t.Helper()
	if p.Stage == "revision-post" {
		p.RevisionPostAttempted = true
	}
	if p.Stage == "revision-unpost" {
		p.RevisionUnpostAttempted = true
	}
	before := h.count("РеалИсх.CustomDocNomDelete") + h.count("РеалИсх.DocNomCreate") + h.count("РеалИсх.CustomDocNomUpdate") + h.count("Документ.PostAsync") + h.count("Документ.CancelPosting")
	r, err := h.base.provider.StepRevision(context.Background(), c, p)
	if err != nil {
		t.Fatal(err)
	}
	after := h.count("РеалИсх.CustomDocNomDelete") + h.count("РеалИсх.DocNomCreate") + h.count("РеалИсх.CustomDocNomUpdate") + h.count("Документ.PostAsync") + h.count("Документ.CancelPosting")
	if after-before > 1 {
		t.Fatal("multiple mutation boundary")
	}
	if after > before && !map[string]bool{"revision-unpost": true, "revision-delete": true, "revision-prepare": true, "revision-save": true, "revision-post": true, "revision-cancel": true}[p.Stage] {
		t.Fatalf("mutation from readonly stage %s", p.Stage)
	}
	raw, _ := json.Marshal(r.Progress)
	if json.Unmarshal(raw, &r.Progress) != nil {
		t.Fatal("progress")
	}
	return r
}

func TestSabyRealizationRevisionSameDocumentAndImmutableOrigin(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	// Backfilled immutable origin rows have a database revision ID while their
	// historical Request deliberately retains revisionId=0 and its original hash.
	c.CurrentRevisionID = 19
	origin := string(p.Native)
	documentID, uuid := p.DocumentID, p.DocumentUUID
	var final ordersale.Result
	for i := 0; i < 50; i++ {
		final = revisionStep(t, h, c, p)
		p = final.Progress
		if final.State != "queued" {
			break
		}
	}
	if final.State != "posted" || !final.RevisionVerified {
		t.Fatalf("revision not posted: %s %s stage %s", final.State, final.Message, p.Stage)
	}
	if string(p.Native) != origin || p.DocumentID != documentID || p.DocumentUUID != uuid || h.base.writeCount("СБИС.ЗаписатьДокумент") != 1 {
		t.Fatal("origin changed or another document created")
	}
	if h.count("Документ.CancelPosting") != 1 || h.count("РеалИсх.CustomDocNomDelete") != 1 || h.count("Документ.PostAsync") != 2 || h.count("РеалИсх.CustomDocNomUpdate") != 2 {
		t.Fatal("unexpected revision mutation counts")
	}
	var proof ordersale.StockProof
	if json.Unmarshal(final.RevisionProof, &proof) != nil || proof.RevisionID != 1 || len(proof.Lines) != 1 || proof.Lines[0].Quantity != "3" || proof.Lines[0].UnitPrice != "8.25" || proof.Lines[0].LineSum != "24.75" {
		t.Fatalf("wrong revision proof %+v", proof)
	}
	if len(p.RetiredRowIDs) != 1 || len(p.OwnedRowIDs) != 2 || len(p.ActiveOwnedRowIDs) != 1 || p.ActiveOwnedRowIDs[0] == p.RetiredRowIDs[0] {
		t.Fatal("lost history or active rows")
	}
}

func TestSabyRealizationRevisionLostDeleteNeverReplays(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	h.loseDelete = true
	h.staleDelete = true
	var result ordersale.Result
	for i := 0; i < 25; i++ {
		result = revisionStep(t, h, c, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "needs_attention" || h.count("РеалИсх.CustomDocNomDelete") != 1 || h.count("Документ.PostAsync") != 1 {
		t.Fatalf("unsafe delete replay: %s %s", result.State, result.Message)
	}
	h.mu.Lock()
	h.staleDelete = false
	h.base.mu.Lock()
	h.base.rows = h.deleted
	h.base.mu.Unlock()
	h.mu.Unlock()
	for i := 0; i < 35; i++ {
		result = revisionStep(t, h, c, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "posted" || h.count("РеалИсх.CustomDocNomDelete") != 1 {
		t.Fatalf("late delete did not reconcile: %s %s", result.State, result.Message)
	}
}

func TestSabyRealizationRevisionCancellationDuringReplacementDoesNotFillOrPost(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	for i := 0; i < 20; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		if len(p.RetiredRowIDs) == 1 {
			break
		}
	}
	if len(p.RetiredRowIDs) != 1 {
		t.Fatal("did not delete original row")
	}
	p.DesiredState = "cancelled"
	r := revisionStep(t, h, c, p)
	if r.State != "cancelled" || !r.CancellationVerified || h.count("Документ.PostAsync") != 1 || h.count("РеалИсх.CustomDocNomUpdate") != 1 {
		t.Fatalf("cancel resumed replacement: %s %s", r.State, r.Message)
	}
}

func TestSabyRealizationRevisionLostUnpostReconcilesOnce(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	h.loseUnpost = true
	var result ordersale.Result
	for i := 0; i < 50; i++ {
		result = revisionStep(t, h, c, p)
		p = result.Progress
		if result.State != "queued" {
			break
		}
	}
	if result.State != "posted" || h.count("Документ.CancelPosting") != 1 || h.count("РеалИсх.CustomDocNomUpdate") != 2 || h.count("Документ.PostAsync") != 2 {
		t.Fatalf("lost reply replayed a mutation: %s %s", result.State, result.Message)
	}
}

func TestSabyRealizationRevisionUnknownSaveCannotAdoptMatchingRow(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	h.loseSave = true
	var r ordersale.Result
	for i := 0; i < 40; i++ {
		r = revisionStep(t, h, c, p)
		p = r.Progress
		if r.State != "queued" {
			break
		}
	}
	if r.State != "needs_attention" || h.count("РеалИсх.CustomDocNomUpdate") != 2 || h.count("Документ.PostAsync") != 1 || len(p.ActiveOwnedRowIDs) != 0 || len(p.OwnedRowIDs) != 1 {
		t.Fatalf("unknown save adopted a matching row: %s %s ids=%v", r.State, r.Message, p.ActiveOwnedRowIDs)
	}
	for i := 0; i < 3; i++ {
		r = revisionStep(t, h, c, p)
		p = r.Progress
	}
	if h.count("РеалИсх.CustomDocNomUpdate") != 2 || h.count("Документ.PostAsync") != 1 || len(p.ActiveOwnedRowIDs) != 0 {
		t.Fatal("retry adopted/reposted unknown row")
	}
}

func TestSabyRealizationRevisionAcknowledgedIDCannotBeReplaced(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	reached := false
	for i := 0; i < 30; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		var receipt sabyRealizationRevisionReceipt
		json.Unmarshal(p.RevisionNative, &receipt)
		if p.Stage == "revision-verify-save" && receipt.SavedRowID > 0 {
			reached = true
			h.base.mu.Lock()
			h.base.rows[0].([]any)[0] = int64(990001)
			h.base.mu.Unlock()
			break
		}
		if r.State != "queued" {
			t.Fatalf("did not reach saved acknowledgement: %s", r.Message)
		}
	}
	if !reached {
		t.Fatal("did not test positive save receipt")
	}
	var r ordersale.Result
	for i := 0; i < 12; i++ {
		r = revisionStep(t, h, c, p)
		p = r.Progress
		if r.State != "queued" {
			break
		}
	}
	if r.State != "needs_attention" || h.count("Документ.PostAsync") != 1 || h.count("РеалИсх.CustomDocNomUpdate") != 2 {
		t.Fatal("equal-group replacement was accepted or repeated")
	}
}

func TestSabyRealizationRevisionChangedTargetCannotReplaceUnknownReceipt(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	h.loseDelete, h.staleDelete = true, true
	for i := 0; i < 15; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		if p.Stage == "revision-verify-delete" && p.Unknown {
			break
		}
	}
	if !p.Unknown {
		t.Fatal("did not exercise unknown write")
	}
	c.Target.Event.Lines[0].Quantity = 20
	r := revisionStep(t, h, c, p)
	if r.State != "needs_attention" || h.count("РеалИсх.CustomDocNomDelete") != 1 || h.count("Документ.PostAsync") != 1 {
		t.Fatal("changed target reset unknown write")
	}
}

func TestSabyRealizationRevisionBeforeOriginFillPublishesOnlyTarget(t *testing.T) {
	h := newRevisionHarness(t)
	origin := realizationProviderRequest()
	p := ordersale.Progress{Stage: "inspect", DesiredState: "posted"}
	for i := 0; i < 10; i++ {
		if p.Stage == "create" {
			p.CreateAttempted = true
		}
		r, err := h.base.provider.Step(context.Background(), origin, p)
		if err != nil {
			t.Fatal(err)
		}
		p = r.Progress
		if p.DocumentID != "" {
			break
		}
	}
	if p.DocumentID == "" || p.PostAttempted || h.count("РеалИсх.CustomDocNomUpdate") != 0 {
		t.Fatal("origin filled before revision")
	}
	target := origin
	target.RevisionID = 1
	target.Event.Fingerprint = strings.Repeat("b", 64)
	target.Event.Lines = []ordersale.Line{{OrderItemID: 100, VariantID: 11, SabyID: "1101", Quantity: 3, UnitPrice: "8.25"}}
	c := ordersale.RevisionCommand{Origin: origin, Current: origin, Target: target, RevisionID: 1}
	var final ordersale.Result
	for i := 0; i < 35; i++ {
		final = revisionStep(t, h, c, p)
		p = final.Progress
		if final.State != "queued" {
			break
		}
	}
	if final.State != "posted" || h.count("РеалИсх.CustomDocNomUpdate") != 1 || h.count("Документ.PostAsync") != 1 || h.count("Документ.CancelPosting") != 0 || h.count("РеалИсх.CustomDocNomDelete") != 0 {
		t.Fatalf("original plan published before target: %s %s", final.State, final.Message)
	}
}

func TestSabyRealizationRevisionCancelAfterUnknownPostNeverReposts(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	for i := 0; i < 40; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		if p.Stage == "revision-verify-post" {
			break
		}
		if r.State != "queued" {
			t.Fatal(r.Message)
		}
	}
	if p.Stage != "revision-verify-post" {
		t.Fatal("target post was not sent")
	}
	p.DesiredState = "cancelled"
	var final ordersale.Result
	for i := 0; i < 15; i++ {
		final = revisionStep(t, h, c, p)
		p = final.Progress
		if final.State != "queued" {
			break
		}
	}
	if final.State != "cancelled" || !final.CancellationVerified || h.count("Документ.PostAsync") != 2 || h.count("Документ.CancelPosting") != 2 {
		t.Fatalf("cancel did not reconcile existing post: %s %s", final.State, final.Message)
	}
}

func TestSabyRealizationDeleteRejectsUnownedIDBeforeWrite(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	for i := 0; i < 15; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		if p.Stage == "revision-verify-delete" {
			break
		}
	}
	var receipt sabyRealizationRevisionReceipt
	json.Unmarshal(p.RevisionNative, &receipt)
	identity, err := sabyRealizationProviderIdentity(c.Origin, p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.base.provider.Client.DeleteOrderRealizationRow(context.Background(), SabyOrderRealizationDeleteRequest{Identity: identity, PublicUUID: c.Origin.OperationKey, OrganizationID: c.Origin.OrganizationID, RegulationID: c.Origin.RegulationID, BuyerID: c.Origin.BuyerID, ExpectedLines: receipt.KnownLines, RowID: 990001})
	if err == nil || r.Outcome != "not_sent" || h.count("РеалИсх.CustomDocNomDelete") != 0 {
		t.Fatal("unowned row was sent for deletion")
	}
}

func TestSabyRealizationRevisionCancelBeforePlannedDeleteDoesNotDelete(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	for i := 0; i < 15; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		if p.Stage == "revision-delete" {
			break
		}
	}
	if p.Stage != "revision-delete" {
		t.Fatal("did not reach planned deletion")
	}
	p.DesiredState = "cancelled"
	r := revisionStep(t, h, c, p)
	if r.State != "cancelled" || !r.CancellationVerified || h.count("РеалИсх.CustomDocNomDelete") != 0 || h.count("Документ.PostAsync") != 1 {
		t.Fatalf("cancel executed an unsent deletion: %s %s", r.State, r.Message)
	}
}

func TestSabyRealizationRevisionUnknownRecoveryStageNeverRestarts(t *testing.T) {
	h := newRevisionHarness(t)
	c, p := revisionCommand(t, h)
	for i := 0; i < 25; i++ {
		r := revisionStep(t, h, c, p)
		p = r.Progress
		if p.Stage == "revision-verify-prepare" {
			break
		}
		if r.State != "queued" {
			t.Fatal(r.Message)
		}
	}
	if p.Stage != "revision-verify-prepare" {
		t.Fatal("did not prepare new row")
	}
	before := h.count("РеалИсх.DocNomCreate")
	p.Stage, p.Unknown = "reconcile-revision-prepare", true
	r := revisionStep(t, h, c, p)
	if r.State != "needs_attention" || h.count("РеалИсх.DocNomCreate") != before {
		t.Fatal("unknown stage restarted an unsafe preparation")
	}
}
