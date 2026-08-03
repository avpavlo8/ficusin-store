package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The store runs as a single container, so keeping the counters in memory
// is enough and costs nothing. If the app is ever scaled to several
// instances these limits become per-instance and should move to the
// database or a cache shared between them.

// rateLimiter counts requests per key inside a sliding window.
type rateLimiter struct {
	mutex  sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
	// sweptAt keeps the map from growing forever: stale keys are dropped
	// once in a while rather than on every request.
	sweptAt time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:    make(map[string][]time.Time),
		limit:   limit,
		window:  window,
		sweptAt: time.Now(),
	}
}

// allow records one request for key and reports whether it fits the limit.
func (limiter *rateLimiter) allow(key string) bool {
	now := time.Now()
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	if now.Sub(limiter.sweptAt) > limiter.window {
		for stored, times := range limiter.hits {
			if len(times) == 0 || now.Sub(times[len(times)-1]) > limiter.window {
				delete(limiter.hits, stored)
			}
		}
		limiter.sweptAt = now
	}

	recent := limiter.hits[key][:0]
	for _, moment := range limiter.hits[key] {
		if now.Sub(moment) < limiter.window {
			recent = append(recent, moment)
		}
	}
	if len(recent) >= limiter.limit {
		limiter.hits[key] = recent
		return false
	}
	limiter.hits[key] = append(recent, now)
	return true
}

// guard wraps a handler so a single client cannot flood it. The message is
// written in the caller's own words because these responses reach the
// customer.
func (limiter *rateLimiter) guard(message string, next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if !limiter.allow(clientIP(request)) {
			writeJSON(response, http.StatusTooManyRequests, errorResponse{Error: message})
			return
		}
		next(response, request)
	}
}

// clientIP reports who is calling. The app sits behind Timeweb's proxy, so
// the direct remote address is always the proxy; the left-most entry of
// X-Forwarded-For is the closest thing to the real client we have.
func clientIP(request *http.Request) string {
	forwarded := request.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if first = strings.TrimSpace(first); first != "" {
			return first
		}
	}
	if real := strings.TrimSpace(request.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
