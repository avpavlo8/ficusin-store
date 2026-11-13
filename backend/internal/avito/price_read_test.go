package avito

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPriceReadUsesRemoteExactListingNotMirror(t *testing.T) {
	for _, status := range []string{"active", "blocked"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/core/v1/items" {
					t.Fatalf("not a listing read: %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprintf(w, `{"resources":[{"id":11,"status":"active","price":9000},{"id":"22","status":%q,"price":1500}]}`, status)
			}))
			defer server.Close()
			s := New(nil, "id", "secret", "https://example.test", "", "", "", "", "")
			s.baseURL = server.URL
			s.token = "existing-token"
			s.tokenUntil = time.Now().Add(time.Hour)
			got, err := s.ReadPrice(context.Background(), "22")
			if status == "active" && (err != nil || got != 150000) {
				t.Fatalf("wrong price %d %v", got, err)
			}
			if status == "blocked" && err == nil {
				t.Fatal("blocked listing accepted")
			}
			if calls != 1 {
				t.Fatalf("requests=%d", calls)
			}
		})
	}
}
