package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresSpoofedForwardingFromPublicPeer(t *testing.T) {
	request := httptest.NewRequest("GET", "https://ficusin.ru/api/v1/health", nil)
	request.RemoteAddr = "203.0.113.20:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.99")
	if got := clientIP(request); got != "203.0.113.20" {
		t.Fatalf("clientIP = %q, want direct public peer", got)
	}
}

func TestClientIPUsesNearestPublicAddressBehindPrivateProxy(t *testing.T) {
	request := httptest.NewRequest("GET", "https://ficusin.ru/api/v1/health", nil)
	request.RemoteAddr = "10.0.0.4:8080"
	request.Header.Set("X-Forwarded-For", "192.0.2.10, 198.51.100.7, 10.0.0.3")
	if got := clientIP(request); got != "198.51.100.7" {
		t.Fatalf("clientIP = %q, want nearest public address", got)
	}
}
