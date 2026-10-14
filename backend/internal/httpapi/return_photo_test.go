package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type returnPhotoStub struct {
	adminRepositoryStub
	marketplaceReturnRepository
	gotBytes int
}

func (s *returnPhotoStub) AddReturnPhoto(_ context.Context, _ admin.Actor, id int64, _ string, data string) (admin.MarketplaceReturn, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	s.gotBytes = len(raw)
	return admin.MarketplaceReturn{ID: id}, err
}
func TestReturnPhotoAcceptsJSONLargerThanOrdinaryLimit(t *testing.T) {
	repo := &returnPhotoStub{}
	handler := NewRouter(discardLogger(), adminDependencies(repo, admin.RoleManager, "manager@example.invalid"))
	body, _ := json.Marshal(map[string]string{"contentType": "image/jpeg", "data": base64.StdEncoding.EncodeToString(make([]byte, 2<<20))})
	request := httptest.NewRequest("POST", "/api/v1/admin/returns/1/photos", bytes.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "ficusin_session", Value: "test"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 201 || repo.gotBytes != 2<<20 {
		t.Fatalf("status=%d bytes=%d body=%s", w.Code, repo.gotBytes, w.Body.String())
	}
	body, _ = json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(make([]byte, 6<<20))})
	request = httptest.NewRequest("POST", "/api/v1/admin/returns/1/photos", bytes.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "ficusin_session", Value: "test"})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 400 {
		t.Fatalf("oversized photo status=%d", w.Code)
	}
}
