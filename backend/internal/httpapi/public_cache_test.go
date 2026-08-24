package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

func TestPublicCacheInvalidation(t *testing.T) {
	t.Parallel()
	cache := newPublicJSONCache()
	var loads atomic.Int64
	loader := func(context.Context) (any, error) {
		return map[string]int64{"generation": loads.Add(1)}, nil
	}

	first, hit, _, _, err := cache.get(context.Background(), "catalog", loader)
	if err != nil || hit {
		t.Fatalf("first load: hit=%v err=%v", hit, err)
	}
	second, hit, _, _, err := cache.get(context.Background(), "catalog", loader)
	if err != nil || !hit || first.etag != second.etag {
		t.Fatalf("cached load: hit=%v err=%v etag=%q/%q", hit, err, first.etag, second.etag)
	}
	cache.invalidate()
	third, hit, _, _, err := cache.get(context.Background(), "catalog", loader)
	if err != nil || hit || third.etag == first.etag || loads.Load() != 2 {
		t.Fatalf("after invalidation: hit=%v err=%v loads=%d", hit, err, loads.Load())
	}
}

func TestPublicCacheSingleflight(t *testing.T) {
	t.Parallel()
	cache := newPublicJSONCache()
	var loads atomic.Int64
	loader := func(context.Context) (any, error) {
		loads.Add(1)
		time.Sleep(25 * time.Millisecond)
		return map[string]bool{"ok": true}, nil
	}
	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if _, _, _, _, err := cache.get(context.Background(), "catalog", loader); err != nil {
				t.Errorf("cache get: %v", err)
			}
		}()
	}
	close(start)
	wait.Wait()
	if loads.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1", loads.Load())
	}
}

func TestSuccessfulMutationInvalidatesPublicCache(t *testing.T) {
	t.Parallel()
	cache := newPublicJSONCache()
	loader := func(context.Context) (any, error) { return map[string]bool{"ok": true}, nil }
	if _, _, _, _, err := cache.get(context.Background(), "catalog", loader); err != nil {
		t.Fatal(err)
	}
	handler := invalidatePublicCacheAfterMutation(cache, func() {}, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, "/api/v1/admin/products/5", nil))
	if _, found := cache.current("catalog", time.Now()); found {
		t.Fatal("successful mutation left a public cache entry behind")
	}
}

func TestCatalogETagReturnsNotModified(t *testing.T) {
	t.Parallel()
	repository := catalogStub{products: []catalog.Product{{ID: "5", Name: "Аглаонема"}}}
	router := NewRouter(discardLogger(), testDependencies(repository, authStub{}))

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("first response: status=%d etag=%q", first.Code, etag)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	request.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, request)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional response: status=%d bytes=%d", second.Code, second.Body.Len())
	}
}

func TestPublicCatalogCachePerformanceGate(t *testing.T) {
	products := make([]catalog.Product, 643)
	for index := range products {
		products[index] = catalog.Product{
			ID: fmt.Sprint(index + 1), Name: fmt.Sprintf("Товар %d", index + 1),
			SKU: fmt.Sprintf("SKU-%d", index + 1), Price: 1490, Stock: 5,
			Image: "https://s3.example.invalid/products/card.jpg",
			FilterAttributes: []catalog.ProductAttribute{{Code: "light", Name: "Свет", Value: "Рассеянный", Filterable: true}},
		}
	}
	router := NewRouter(discardLogger(), testDependencies(catalogStub{products: products}, authStub{}))
	coldStarted := time.Now()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	if elapsed := time.Since(coldStarted); elapsed > 2*time.Second {
		t.Fatalf("cold realistic catalog took %s", elapsed)
	}

	warmStarted := time.Now()
	for range 50 {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
	}
	if elapsed := time.Since(warmStarted); elapsed > time.Second {
		t.Fatalf("50 cached catalog responses took %s", elapsed)
	}
}
