package httpapi

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http/httptest"
	"testing"
)

func TestProcurementManagerCanViewButCannotChangePurchases(t *testing.T) {
	router := NewRouter(discardLogger(), procurementDependencies(&procurementStub{}, admin.RoleManager))
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/admin/procurement", "", 200},
		{"GET", "/api/v1/admin/procurement/orders/4", "", 200},
		{"PATCH", "/api/v1/admin/procurement/order-lines/4", `{"invoiceLineIds":[5,6]}`, 403},
		{"POST", "/api/v1/admin/procurement/orders", "{}", 403},
		{"PATCH", "/api/v1/admin/procurement/orders/4/status", "{}", 403},
		{"POST", "/api/v1/admin/procurement/batches/1/approve", "{}", 403},
		{"GET", "/api/v1/admin/procurement/plan-drafts", "", 403},
	}
	for _, c := range cases {
		t.Run(c.method+c.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, adminRequest(c.method, c.path, c.body))
			if w.Code != c.status {
				t.Fatalf("got %d want %d: %s", w.Code, c.status, w.Body.String())
			}
		})
	}
}
