package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

type marketplaceChannelsRepository struct{ adminRepositoryStub }

func (*marketplaceChannelsRepository) ReadEditorPlan(context.Context, int64) (admin.EditorPlanView, error) {
	return admin.EditorPlanView{Links: map[int64]map[string]string{10: {"saby": "X-SABY", "ozon": "ozon-article", "wb": "123", "unknown": "future-channel"}}}, nil
}
func (*marketplaceChannelsRepository) SaveEditorPlan(context.Context, admin.Actor, int64, admin.EditorPlan, bool) (admin.EditorPlanView, error) {
	return admin.EditorPlanView{}, errors.New("unexpected write")
}
func (*marketplaceChannelsRepository) CachedEditorReference(context.Context, string, string, string) (json.RawMessage, error) {
	return nil, nil
}
func (*marketplaceChannelsRepository) CacheEditorReference(context.Context, string, string, string, json.RawMessage) error {
	return errors.New("unexpected reference write")
}
func (*marketplaceChannelsRepository) SabyEditorReferences(context.Context) (map[string]any, error) {
	return nil, errors.New("unexpected Saby lookup")
}

type marketplaceChannelsReader struct {
	mu    sync.Mutex
	calls map[string]string
}

func (*marketplaceChannelsReader) ReadEditorReference(context.Context, string, string, int64, int64, int64, string, ...string) (json.RawMessage, error) {
	return nil, errors.New("unexpected reference lookup")
}
func (s *marketplaceChannelsReader) ReadEditorCard(_ context.Context, channel, identifier string) (ce.RemoteCard, error) {
	s.mu.Lock()
	s.calls[channel] = identifier
	s.mu.Unlock()
	if channel != "wb" && channel != "ozon" {
		return ce.RemoteCard{}, errors.New("подключение площадки не настроено")
	}
	return ce.RemoteCard{Card: ce.Request{Channel: ce.Channel{Category: ce.Category{Name: "Loaded category"}}}, ExternalIDs: ce.RemoteIDs{ProductID: identifier}}, nil
}

func TestMarketplaceHydrationIgnoresSabyAndUnknownChannels(t *testing.T) {
	repo := &marketplaceChannelsRepository{}
	source := &marketplaceChannelsReader{calls: map[string]string{}}
	deps := adminDependencies(repo, admin.RoleOwner, "owner@example.invalid")
	deps.EditorReferences = source
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), deps).ServeHTTP(response, adminRequest(http.MethodGet, "/api/v1/admin/products/1/marketplace-cards?variantId=10", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Cards  map[string]ce.RemoteCard `json:"cards"`
		Errors map[string]string        `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 || len(result.Cards) != 2 {
		t.Fatalf("unrelated Saby link leaked into marketplace hydration: %+v", result)
	}
	if len(source.calls) != 2 || source.calls["ozon"] != "ozon-article" || source.calls["wb"] != "123" {
		t.Fatalf("unexpected read-only channel calls: %v", source.calls)
	}
}
