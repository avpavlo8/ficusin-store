package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
)

type editorPriceStub struct {
	adminRepositoryStub
	mutationCalls int
	fingerprint   string
	fail          error
}

func (s *editorPriceStub) EditorPrices(context.Context, int64) ([]admin.EditorPricePublication, error) {
	return []admin.EditorPricePublication{{ID: 9, VariantID: 1, Channel: "wb", PriceMinor: 120000}}, nil
}
func (s *editorPriceStub) EditorPricePublicationMode() string { return "mock" }
func (s *editorPriceStub) ApproveEditorPrice(_ context.Context, _ admin.Actor, productID, priceID int64, fingerprint string) (admin.EditorPricePublication, error) {
	s.mutationCalls++
	s.fingerprint = fingerprint
	if productID != 7 || priceID != 9 {
		return admin.EditorPricePublication{}, errors.New("wrong target")
	}
	return admin.EditorPricePublication{ID: priceID, VariantID: 1, Channel: "wb", Status: "queued"}, s.fail
}
func (s *editorPriceStub) RetryEditorPrice(ctx context.Context, actor admin.Actor, productID, priceID int64, fingerprint string) (admin.EditorPricePublication, error) {
	return s.ApproveEditorPrice(ctx, actor, productID, priceID, fingerprint)
}
func TestEditorPriceReadDoesNotApprove(t *testing.T) {
	repo := &editorPriceStub{}
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), adminDependencies(repo, admin.RoleManager, "")).ServeHTTP(response, adminRequest("GET", "/api/v1/admin/products/7/editor-prices", ""))
	if response.Code != 200 || repo.mutationCalls != 0 || !strings.Contains(response.Body.String(), `"publicationMode":"mock"`) {
		t.Fatalf("read=%d calls=%d %s", response.Code, repo.mutationCalls, response.Body.String())
	}
}
func TestEditorPriceMutationsRequireOwnerAndExactSavedFingerprint(t *testing.T) {
	for _, action := range []string{"approve", "retry"} {
		for _, scenario := range []struct {
			name, role, body string
			fail             error
			want, calls      int
		}{
			{"manager", admin.RoleManager, `{"fingerprint":"saved"}`, nil, 403, 0},
			{"missing fingerprint", admin.RoleOwner, `{}`, nil, 400, 0},
			{"stale", admin.RoleOwner, `{"fingerprint":"saved"}`, pgx.ErrNoRows, 409, 1},
			{"disabled", admin.RoleOwner, `{"fingerprint":"saved"}`, admin.ErrPriceChannelUnavailable, 409, 1},
			{"exact approval", admin.RoleOwner, `{"fingerprint":"saved"}`, nil, 200, 1},
		} {
			t.Run(action+" "+scenario.name, func(t *testing.T) {
				repo := &editorPriceStub{fail: scenario.fail}
				response := httptest.NewRecorder()
				NewRouter(discardLogger(), adminDependencies(repo, scenario.role, "")).ServeHTTP(response, adminRequest("POST", "/api/v1/admin/products/7/editor-prices/9/"+action, scenario.body))
				if response.Code != scenario.want || repo.mutationCalls != scenario.calls {
					t.Fatalf("status=%d calls=%d %s", response.Code, repo.mutationCalls, response.Body.String())
				}
				if scenario.calls > 0 && repo.fingerprint != "saved" {
					t.Fatalf("fingerprint changed: %q", repo.fingerprint)
				}
			})
		}
	}
}
