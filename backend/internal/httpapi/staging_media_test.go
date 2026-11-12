package httpapi

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
)

type stagingMediaStub struct{ calls int }

func (s *stagingMediaStub) ReadPublicObject(_ context.Context, key string) (string, []byte, error) {
	s.calls++
	return "image/jpeg", []byte("image"), nil
}
func TestStagingPublicImageGateAndPrivateBoundary(t *testing.T) {
	stub := &stagingMediaStub{}
	for _, test := range []struct {
		env, key string
		access   bool
		want     int
	}{{"staging", "products/1/cover.jpg", false, 403}, {"staging", "products/1/cover.jpg", true, 200}, {"staging", "chat/7/private", true, 404}, {"staging", "products/../private", true, 404}, {"production", "products/1/cover.jpg", true, 404}} {
		handler := EnvironmentGuard(stagingMediaHandler(test.env, stub), test.env, "test-staging-secret")
		token := base64.RawURLEncoding.EncodeToString([]byte(test.key))
		r := httptest.NewRequest("GET", "/api/v1/staging/media/"+token, nil)
		r.SetPathValue("key", token)
		if test.access {
			r.SetBasicAuth("staging", "test-staging-secret")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatal(test, w.Code, w.Body.String())
		}
	}
	if stub.calls != 1 {
		t.Fatal("private/anonymous requests reached public reader", stub.calls)
	}
}
