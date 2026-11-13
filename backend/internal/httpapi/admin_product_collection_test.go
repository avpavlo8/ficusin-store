package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
	"testing"
)

type productCollectionStub struct {
	adminRepositoryStub
	calls    int
	included bool
	err      error
}

func (s *productCollectionStub) SetProductCollectionMembership(_ context.Context, _ admin.Actor, productID, collectionID int64, included bool) error {
	if productID != 3 || collectionID != 7 {
		panic("wrong IDs")
	}
	s.calls++
	s.included = included
	return s.err
}
func TestProductCollectionMembership(t *testing.T) {
	for _, tc := range []struct {
		name, role, body string
		err              error
		status, calls    int
		included         bool
	}{
		{"include", admin.RoleOwner, `{"included":true}`, nil, 200, 1, true},
		{"exclude", admin.RoleOwner, `{"included":false}`, nil, 200, 1, false},
		{"missing flag", admin.RoleOwner, `{}`, nil, 400, 0, false},
		{"malformed", admin.RoleOwner, `{`, nil, 400, 0, false},
		{"denied", "viewer", `{"included":true}`, nil, 403, 0, false},
		{"dynamic", admin.RoleOwner, `{"included":true}`, admin.ErrInvalidInput, 400, 1, true},
		{"missing product", admin.RoleOwner, `{"included":true}`, pgx.ErrNoRows, 404, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &productCollectionStub{err: tc.err}
			router := NewRouter(discardLogger(), adminDependencies(repo, tc.role, ""))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, adminRequest("PUT", "/api/v1/admin/products/3/collections/7", tc.body))
			if response.Code != tc.status || repo.calls != tc.calls || repo.included != tc.included {
				t.Fatalf("status=%d calls=%d included=%v body=%s", response.Code, repo.calls, repo.included, response.Body.String())
			}
		})
	}
}
