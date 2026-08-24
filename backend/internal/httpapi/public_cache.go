package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"golang.org/x/sync/singleflight"
)

const publicCacheTTL = 15 * time.Second

type publicCacheEntry struct {
	body      []byte
	etag      string
	expiresAt time.Time
}

type publicJSONCache struct {
	mu         sync.RWMutex
	entries    map[string]publicCacheEntry
	generation uint64
	loads      singleflight.Group
}

func newPublicJSONCache() *publicJSONCache {
	return &publicJSONCache{entries: make(map[string]publicCacheEntry)}
}

func (cache *publicJSONCache) invalidate() {
	cache.mu.Lock()
	cache.generation++
	cache.entries = make(map[string]publicCacheEntry)
	cache.mu.Unlock()
}

func (cache *publicJSONCache) current(key string, now time.Time) (publicCacheEntry, bool) {
	cache.mu.RLock()
	entry, found := cache.entries[key]
	cache.mu.RUnlock()
	return entry, found && now.Before(entry.expiresAt)
}

func (cache *publicJSONCache) get(
	ctx context.Context,
	key string,
	loader func(context.Context) (any, error),
) (entry publicCacheEntry, hit bool, loadDuration, encodeDuration time.Duration, err error) {
	if entry, hit = cache.current(key, time.Now()); hit {
		return entry, true, 0, 0, nil
	}

	value, err, shared := cache.loads.Do(key, func() (any, error) {
		if cached, found := cache.current(key, time.Now()); found {
			return cacheLoadResult{entry: cached, hit: true}, nil
		}
		cache.mu.RLock()
		generation := cache.generation
		cache.mu.RUnlock()

		loadStarted := time.Now()
		payload, loadErr := loader(ctx)
		loadDuration := time.Since(loadStarted)
		if loadErr != nil {
			return nil, loadErr
		}
		encodeStarted := time.Now()
		body, encodeErr := json.Marshal(payload)
		encodeDuration := time.Since(encodeStarted)
		if encodeErr != nil {
			return nil, encodeErr
		}
		body = append(body, '\n')
		hash := sha256.Sum256(body)
		entry := publicCacheEntry{
			body:      body,
			etag:      `"` + hex.EncodeToString(hash[:16]) + `"`,
			expiresAt: time.Now().Add(publicCacheTTL),
		}
		cache.mu.Lock()
		if cache.generation == generation {
			cache.entries[key] = entry
		}
		cache.mu.Unlock()
		return cacheLoadResult{entry: entry, loadDuration: loadDuration, encodeDuration: encodeDuration}, nil
	})
	if err != nil {
		return publicCacheEntry{}, false, 0, 0, err
	}
	result := value.(cacheLoadResult)
	return result.entry, result.hit || shared, result.loadDuration, result.encodeDuration, nil
}

type cacheLoadResult struct {
	entry          publicCacheEntry
	hit            bool
	loadDuration   time.Duration
	encodeDuration time.Duration
}

type detailCacheEntry struct {
	detail    catalog.ProductDetail
	expiresAt time.Time
}

type cachedCatalogRepository struct {
	source  catalogRepository
	mu      sync.RWMutex
	details map[string]detailCacheEntry
	loads   singleflight.Group
}

func newCachedCatalogRepository(source catalogRepository) *cachedCatalogRepository {
	return &cachedCatalogRepository{source: source, details: make(map[string]detailCacheEntry)}
}

func (repository *cachedCatalogRepository) ListAvailable(ctx context.Context) ([]catalog.Product, error) {
	return repository.source.ListAvailable(ctx)
}

func (repository *cachedCatalogRepository) ListCategories(ctx context.Context) ([]catalog.Category, error) {
	return repository.source.ListCategories(ctx)
}

func (repository *cachedCatalogRepository) DetailBySlug(ctx context.Context, slug string) (catalog.ProductDetail, error) {
	repository.mu.RLock()
	entry, found := repository.details[slug]
	repository.mu.RUnlock()
	if found && time.Now().Before(entry.expiresAt) {
		return entry.detail, nil
	}
	value, err, _ := repository.loads.Do(slug, func() (any, error) {
		repository.mu.RLock()
		entry, found := repository.details[slug]
		repository.mu.RUnlock()
		if found && time.Now().Before(entry.expiresAt) {
			return entry.detail, nil
		}
		detail, loadErr := repository.source.DetailBySlug(ctx, slug)
		if loadErr != nil {
			return catalog.ProductDetail{}, loadErr
		}
		repository.mu.Lock()
		repository.details[slug] = detailCacheEntry{detail: detail, expiresAt: time.Now().Add(publicCacheTTL)}
		repository.mu.Unlock()
		return detail, nil
	})
	if err != nil {
		return catalog.ProductDetail{}, err
	}
	return value.(catalog.ProductDetail), nil
}

func (repository *cachedCatalogRepository) invalidate() {
	repository.mu.Lock()
	repository.details = make(map[string]detailCacheEntry)
	repository.mu.Unlock()
}

func writePublicCacheResponse(
	response http.ResponseWriter,
	request *http.Request,
	entry publicCacheEntry,
	hit bool,
	loadDuration, encodeDuration time.Duration,
) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "public, max-age=5, must-revalidate")
	response.Header().Set("ETag", entry.etag)
	if hit {
		response.Header().Set("X-Ficusin-Cache", "HIT")
		response.Header().Set("Server-Timing", `cache;desc="hit"`)
	} else {
		response.Header().Set("X-Ficusin-Cache", "MISS")
		response.Header().Set("Server-Timing", fmt.Sprintf("load;dur=%.3f, encode;dur=%.3f", float64(loadDuration.Microseconds())/1000, float64(encodeDuration.Microseconds())/1000))
	}
	if etagMatches(request.Header.Get("If-None-Match"), entry.etag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(entry.body)
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusCapturingWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusCapturingWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(body)
}

func invalidatePublicCacheAfterMutation(cache *publicJSONCache, invalidateDetails func(), next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !affectsPublicCatalog(request) {
			next.ServeHTTP(response, request)
			return
		}
		writer := &statusCapturingWriter{ResponseWriter: response}
		next.ServeHTTP(writer, request)
		if writer.status >= 200 && writer.status < 300 {
			cache.invalidate()
			invalidateDetails()
		}
	})
}

func affectsPublicCatalog(request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return false
	}
	path := request.URL.Path
	for _, prefix := range []string{
		"/api/v1/admin/products",
		"/api/v1/admin/variants",
		"/api/v1/admin/categories",
		"/api/v1/admin/attributes",
		"/api/v1/admin/catalog-filters",
		"/api/v1/admin/collections",
		"/api/v1/admin/collection-definitions",
		"/api/v1/admin/reviews",
		"/api/v1/admin/orders",
		"/api/v1/integrations/saby/catalog",
		"/api/v1/orders",
		"/api/v1/payments",
		"/api/v1/account/reviews",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return strings.HasPrefix(path, "/api/v1/products/") && strings.HasSuffix(path, "/reviews")
}
