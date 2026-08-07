package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type collectionRepositoryStub struct {
	items []catalog.Collection
	err   error
}

func (stub collectionRepositoryStub) ListCollections(context.Context) ([]catalog.Collection, error) {
	return stub.items, stub.err
}

func collectionsBody(t *testing.T, repository collectionRepository) []catalog.Collection {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	response := httptest.NewRecorder()
	collectionsHandler(logger, repository).ServeHTTP(
		response, httptest.NewRequest(http.MethodGet, "/api/v1/collections", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("ожидали 200, получили %d", response.Code)
	}
	var body struct {
		Collections []catalog.Collection `json:"collections"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	return body.Collections
}

// Подборки — украшение витрины, а не её опора. Если база молчит, покупатель
// должен увидеть каталог без вкладок, а не страницу с ошибкой.
func TestCollectionsSurviveDatabaseFailure(t *testing.T) {
	got := collectionsBody(t, collectionRepositoryStub{err: errors.New("база недоступна")})
	if len(got) != 0 {
		t.Fatalf("ожидали пустой список, получили %d", len(got))
	}
}

// На голой установке и в тестах репозитория может не быть вовсе.
func TestCollectionsWithoutRepository(t *testing.T) {
	if got := collectionsBody(t, nil); len(got) != 0 {
		t.Fatalf("ожидали пустой список, получили %d", len(got))
	}
}

func TestCollectionsReturnWhatRepositoryGave(t *testing.T) {
	got := collectionsBody(t, collectionRepositoryStub{items: []catalog.Collection{
		{Slug: "easy", Title: "Неприхотливые", Note: "Простят забытый полив", Count: 4},
	}})
	if len(got) != 1 || got[0].Slug != "easy" || got[0].Count != 4 {
		t.Fatalf("подборка потерялась по дороге: %+v", got)
	}
}
