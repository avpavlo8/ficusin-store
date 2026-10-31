package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestSabyCanonicalReadOnly(t *testing.T) {
	if os.Getenv("SABY_CATALOG_LIVE_CHECK") != "FICUSIN-API-TEST-20261031" || os.Getenv("GITHUB_REF_NAME") != "probe/site-saby-adapter-20261031" {
		t.Skip("isolated synthetic read only")
	}
	c := NewSabyClient(os.Getenv("SABY_APP_CLIENT_ID"), os.Getenv("SABY_APP_SECRET"), os.Getenv("SABY_SECRET_KEY"), 278, 6)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, protocol := range []int{6, 3} {
		request := map[string]any{"jsonrpc": "2.0", "protocol": protocol, "method": "Номенклатура.Прочитать", "params": map[string]any{"ИдО": 3952, "Расширение": nil}, "id": 1}
		var result json.RawMessage
		err := c.authorizedJSON(ctx, http.MethodPost, c.serviceURL, request, &result)
		t.Logf("canonical synthetic read protocol %d: %s error %v", protocol, result, err)
	}
}
