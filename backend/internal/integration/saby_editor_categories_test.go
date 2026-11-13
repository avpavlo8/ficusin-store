package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSabyEditorCategoriesVendorPagingAndFiltering(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		var req struct {
			Method string
			Params map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Method != "NomenclatureCategory.List" {
			t.Errorf("unexpected mutation/method: %s", req.Method)
		}
		nav, _ := req.Params["Навигация"].(map[string]any)
		if sabyRecordField(nav, "Страница") != float64(calls) || sabyRecordField(nav, "РазмерСтраницы") != float64(30) || sabyRecordField(nav, "ЕстьЕще") != true {
			t.Errorf("bad native navigation: %+v", nav)
		}
		if req.Params["Сортировка"] != nil {
			t.Error("unexpected sorting")
		}
		schema := []any{map[string]any{"n": "Id", "t": "Число целое"}, map[string]any{"n": "Name", "t": "Строка"}, map[string]any{"n": "IsCategory", "t": "Логическое"}, map[string]any{"n": "Deleted", "t": "Логическое"}}
		rows := []any{}
		if calls == 0 {
			for i := 0; i < 30; i++ {
				rows = append(rows, []any{1000 + i, "Растения", true, false})
			}
		} else {
			rows = append(rows, []any{2000, "Папка", nil, false}, []any{2001, "Архив", true, true}, []any{2002, "Грунты", true, false})
		}
		calls++
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"_type": "recordset", "s": schema, "d": rows}})
	}))
	defer server.Close()
	client := NewSabyClient("id", "secret", "key", 278, 6)
	client.authURL = server.URL + "/auth"
	client.serviceURL = server.URL + "/rpc"
	source := &SabyEditorMetadataSource{Client: client, Hierarchy: &SabyEditorHierarchySource{Client: client}}
	got, err := source.ListSabyEditorCategories(context.Background())
	if err != nil || len(got) != 31 || got[30].ID != "2002" || calls != 2 {
		t.Fatalf("got %+v/%v calls=%d", got, err, calls)
	}
}
func TestSabyEditorCategoryRowsRejectMalformedNativeSchema(t *testing.T) {
	for _, bad := range []map[string]any{{}, {"s": []any{}, "d": []any{"not row"}}, {"s": []any{map[string]any{"n": "Id"}}, "d": []any{[]any{1, 2}}}} {
		if _, err := sabyEditorCategoryRows(bad); err == nil {
			t.Fatalf("malformed schema accepted: %+v", bad)
		}
	}
}
