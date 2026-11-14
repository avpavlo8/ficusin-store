package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

type webhookPaymentStub struct {
	paymentService
	configured bool
	ids        []string
	err        error
}

func (s *webhookPaymentStub) Configured() bool { return s.configured }
func (s *webhookPaymentStub) SyncOutstanding(_ context.Context, id string) error {
	s.ids = append(s.ids, id)
	return s.err
}

func TestPaymentWebhookAcceptsFullProviderObjectAndRechecksOnlyItsID(t *testing.T) {
	for _, event := range []string{"payment.succeeded", "payment.waiting_for_capture", "payment.canceled"} {
		t.Run(event, func(t *testing.T) {
			service := &webhookPaymentStub{configured: true}
			// All monetary/status metadata is untrusted. The service receives
			// only the ID and must fetch the current object from its provider.
			body := `{"type":"notification","event":"` + event + `","object":{"id":"provider-payment-42","status":"succeeded","paid":true,"amount":{"value":"0.01","currency":"USD"},"metadata":{"order_id":"other-order"},"payment_method":{"type":"bank_card"},"future_provider_field":{"nested":true}}}`
			response := httptest.NewRecorder()
			yooKassaWebhookHandler(discardLogger(), service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
			if response.Code != http.StatusOK || len(service.ids) != 1 || service.ids[0] != "provider-payment-42" {
				t.Fatalf("full notification did not reach provider recheck: %d %v", response.Code, service.ids)
			}
		})
	}
}

func TestPaymentWebhookRejectsInvalidOrUnrelatedNotificationsBeforeProviderAccess(t *testing.T) {
	for _, body := range []string{
		`{`,
		`null`,
		`{"event":"payment.succeeded","object":{"id":"p"}}`,
		`{"type":"other","event":"payment.succeeded","object":{"id":"p"}}`,
		`{"type":"notification","event":"payment.succeeded","object":{"id":42}}`,
		`{"type":"notification","event":"payment.succeeded","object":{"id":" "}}`,
		`{"type":"notification","event":"payment.succeeded","object":null}`,
		`{"type":"notification","event":"payment.succeeded","object":{"status":"succeeded"}}`,
		`{"type":"notification","event":"payment.succeeded","object":{"id":"p"}} {}`,
		`{"type":"notification","event":"refund.succeeded","object":{"id":"refund-id"}}`,
		`{"type":"notification","event":"payment.succeeded","object":{"id":"p"},"unexpected":true}`,
		`{"type":"notification","event":"payment.succeeded","object":{"id":"p","padding":"` + strings.Repeat("x", int(maximumJSONBodySize)) + `"}}`,
	} {
		service := &webhookPaymentStub{configured: true}
		response := httptest.NewRecorder()
		yooKassaWebhookHandler(discardLogger(), service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if response.Code != http.StatusOK || len(service.ids) != 0 {
			t.Fatalf("invalid/unrelated notification reached provider: %d %v", response.Code, service.ids)
		}
	}
}

func TestPaymentWebhookRetriesProviderFailuresAndKeepsDisabledIntegrationIdle(t *testing.T) {
	for _, configured := range []bool{false, true} {
		service := &webhookPaymentStub{configured: configured, err: errors.New("provider unavailable")}
		response := httptest.NewRecorder()
		yooKassaWebhookHandler(discardLogger(), service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"notification","event":"payment.succeeded","object":{"id":"p"}}`)))
		if configured {
			if response.Code != http.StatusInternalServerError || len(service.ids) != 1 {
				t.Fatal("provider failure was acknowledged without retry")
			}
		} else if response.Code != http.StatusOK || len(service.ids) != 0 {
			t.Fatal("disabled integration attempted provider access")
		}
	}
}

func TestPaymentHandlersKeepMissingAuthDependencyAnonymous(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?delivery=pickup", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "old-session"})
	response := httptest.NewRecorder()
	paymentMethodsHandler(nil, nil).ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"id":"invoice"`) || !strings.Contains(response.Body.String(), `"id":"on_delivery"`) {
		t.Fatalf("missing auth granted wholesale payment or failed: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	startShipmentOfferPaymentHandler(discardLogger(), nil, nil).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("shipment payment proceeded without authentication: %d", response.Code)
	}
}
