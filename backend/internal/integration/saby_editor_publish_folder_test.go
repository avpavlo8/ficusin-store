package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSabyEditorFolderMoveScopesOneCardAndNeverAnswersVendorQuestions(t *testing.T) {
	questions := true
	checks, moves := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		var req struct {
			Method string
			Params map[string]any
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "Nomenclature.CheckBeforeMove":
			checks++
			record, _ := req.Params["record"].(map[string]any)
			ids, _ := sabyRecordField(record, "@Номенклатура").([]any)
			if len(ids) != 1 || ids[0] != float64(1101) || sabyRecordField(record, "@Раздел") != float64(3333) || sabyRecordField(record, "MODIFIERS_TRANSFER_QUESTION") != nil {
				t.Error("unscoped or auto-confirmed preflight")
			}
			q := []any{}
			if questions {
				q = append(q, map[string]any{"id": "CONSTRAINT_QUESTION"})
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"_type": "record", "s": []any{map[string]any{"n": "questions", "t": "JSON-объект"}}, "d": []any{q}}})
		case "Nomenclature.Move":
			moves++
			if req.Params["folder_id"] != float64(3333) {
				t.Error("wrong folder")
			}
			filter, _ := req.Params["filter"].(map[string]any)
			selection, _ := sabyRecordField(filter, "selection").(map[string]any)
			marked, _ := sabyRecordField(selection, "marked").([]any)
			if len(marked) != 1 || marked[0] != "1101" || sabyRecordField(selection, "recursive") != false {
				t.Error("move can affect other cards")
			}
			io.WriteString(w, `{"result":true}`)
		default:
			t.Errorf("unexpected RPC %s", req.Method)
		}
	}))
	defer server.Close()
	c := NewSabyClient("id", "secret", "key", 278, 6)
	c.authURL = server.URL + "/auth"
	c.serviceURL = server.URL + "/service/"
	if err := c.moveSabyEditorFolder(context.Background(), 1101, 3333); err == nil || moves != 0 {
		t.Fatal("vendor confirmation was automatically accepted")
	}
	questions = false
	if err := c.moveSabyEditorFolder(context.Background(), 1101, 3333); err != nil || moves != 1 || checks != 2 {
		t.Fatalf("safe move failed: %v checks=%d moves=%d", err, checks, moves)
	}
}
