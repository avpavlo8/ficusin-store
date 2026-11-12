package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEditorReferencesOnlyReadApprovedVendorMethods(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/description-category/tree" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		if r.Header.Get("Client-Id") != "client" || r.Header.Get("Api-Key") != "key" {
			t.Error("missing vendor authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	e := NewMarketplaceExecutor("wb", "client", "key")
	e.ozonBase = server.URL
	if _, err := e.ReadEditorReference(context.Background(), "ozon", "categories", 0, 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReadEditorReference(context.Background(), "ozon", "import", 0, 0, 0, ""); err == nil {
		t.Fatal("write operation accepted")
	}
	if calls != 1 {
		t.Fatal("unexpected vendor request")
	}
	offline := NewMarketplaceExecutor("", "", "")
	if _, err := offline.ReadEditorReference(context.Background(), "ozon", "categories", 0, 0, 0, ""); err == nil {
		t.Fatal("missing credentials hidden")
	}
}
