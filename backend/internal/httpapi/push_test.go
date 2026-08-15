package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
)

type pushStub struct {
	subscribed   notify.Subscription
	unsubscribed string
}

func (stub *pushStub) PublicKey() string { return "public-key" }
func (stub *pushStub) Subscribe(_ context.Context, _ *int64, subscription notify.Subscription) error {
	stub.subscribed = subscription
	return nil
}
func (stub *pushStub) Unsubscribe(_ context.Context, endpoint string) error {
	stub.unsubscribed = endpoint
	return nil
}

func TestPushSubscribeValidatesAndStoresBrowserSubscription(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &pushStub{}
	handler := pushSubscribeHandler(logger, nil, stub)

	bad := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(`{"endpoint":"https://push.example/1","keys":{}}`))
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("incomplete subscription status = %d, want 400", badResponse.Code)
	}

	good := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", strings.NewReader(`{"endpoint":"https://push.example/1","keys":{"p256dh":"browser-key","auth":"auth-key"}}`))
	good.Header.Set("User-Agent", "Mobile Browser")
	goodResponse := httptest.NewRecorder()
	handler.ServeHTTP(goodResponse, good)
	if goodResponse.Code != http.StatusOK || stub.subscribed.Endpoint != "https://push.example/1" || stub.subscribed.UserAgent != "Mobile Browser" {
		t.Fatalf("subscription was not stored: status=%d subscription=%+v", goodResponse.Code, stub.subscribed)
	}
}

func TestPushUnsubscribeRejectsInvalidEndpoint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &pushStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/push/unsubscribe", strings.NewReader(`{"endpoint":"not-a-push-url"}`))
	response := httptest.NewRecorder()
	pushUnsubscribeHandler(logger, stub).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || stub.unsubscribed != "" {
		t.Fatalf("status=%d unsubscribe=%q", response.Code, stub.unsubscribed)
	}
}
