package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

type flowwowReadStub struct{ calls int }

func (s *flowwowReadStub) List(context.Context) (any, error) { s.calls++; return map[string]any{}, nil }
func (s *flowwowReadStub) Reference(context.Context, string, string) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(`{}`), nil
}
func (s *flowwowReadStub) Remote(context.Context, int) (any, error) {
	s.calls++
	return map[string]any{}, nil
}
func TestFlowwowReadHandlersRejectAnonymousBeforeAccess(t *testing.T) {
	for _, kind := range []string{"list", "references", "products"} {
		s := &flowwowReadStub{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/admin/flowwow?page=0", nil)
		h := adminHandlers{auth: adminAuthStub{}}
		flowwowHandler(h, s, kind)(w, r)
		if w.Code != 401 || s.calls != 0 {
			t.Fatalf("%s: status %d, calls %d", kind, w.Code, s.calls)
		}
	}
}
