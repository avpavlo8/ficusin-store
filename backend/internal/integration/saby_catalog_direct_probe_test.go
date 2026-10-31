package integration

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestSabyCatalogueDirectReadOnly(t *testing.T) {
	if os.Getenv("SABY_CATALOG_LIVE_CHECK") != "FICUSIN-API-TEST-20261031" || os.Getenv("GITHUB_REF_NAME") != "probe/site-saby-adapter-20261031" {
		t.Skip("isolated probe only")
	}
	c := NewSabyClient(os.Getenv("SABY_APP_CLIENT_ID"), os.Getenv("SABY_APP_SECRET"), os.Getenv("SABY_SECRET_KEY"), 278, 6)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var row map[string]any
	err := c.authorizedJSON(ctx, http.MethodGet, c.apiBase+"/retail/nomenclature/6ed33cff-133b-4400-8d3f-9f37a01e5717", nil, &row)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic direct response: %+v", row)
}
