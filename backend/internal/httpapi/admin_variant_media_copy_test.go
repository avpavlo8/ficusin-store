package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
	"testing"
)

type variantMediaCopyStub struct {
	adminRepositoryStub
	calls              int
	variantID, mediaID int64
	missing            bool
}

func (s *variantMediaCopyStub) CopyVariantMedia(_ context.Context, _ admin.Actor, variantID, mediaID int64) (admin.ProductMedia, error) {
	s.calls++
	s.variantID = variantID
	s.mediaID = mediaID
	if s.missing {
		return admin.ProductMedia{}, pgx.ErrNoRows
	}
	return admin.ProductMedia{ID: 99, URL: "/photo.jpg"}, nil
}
func (s *variantMediaCopyStub) CopyProductMedia(ctx context.Context, actor admin.Actor, productID, mediaID int64) (admin.ProductMedia, error) {
	return s.CopyVariantMedia(ctx, actor, productID, mediaID)
}
func TestCopyVariantMediaRoute(t *testing.T) {
	for _, tc := range []struct {
		name, role, body string
		missing          bool
		status, calls    int
	}{
		{"copy", admin.RoleOwner, `{"mediaId":7}`, false, 201, 1},
		{"invalid", admin.RoleOwner, `{"mediaId":0}`, false, 400, 0},
		{"malformed", admin.RoleOwner, `{`, false, 400, 0},
		{"missing or foreign photo", admin.RoleOwner, `{"mediaId":7}`, true, 404, 1},
		{"denied", "viewer", `{"mediaId":7}`, false, 403, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, resource := range []string{"variants", "products"} {
				repo := &variantMediaCopyStub{missing: tc.missing}
				router := NewRouter(discardLogger(), adminDependencies(repo, tc.role, ""))
				response := httptest.NewRecorder()
				router.ServeHTTP(response, adminRequest("POST", "/api/v1/admin/"+resource+"/3/media/copy", tc.body))
				if response.Code != tc.status || repo.calls != tc.calls {
					t.Fatalf("status=%d calls=%d body=%s", response.Code, repo.calls, response.Body.String())
				}
				if repo.calls > 0 && (repo.variantID != 3 || repo.mediaID != 7) {
					t.Fatalf("wrong identifiers: %d %d", repo.variantID, repo.mediaID)
				}
			}
		})
	}
}
