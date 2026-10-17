package integration

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWBMissingOrPendingHistoryUsesBoundedSellerPriceReadback(t *testing.T) {
	for _, history := range []int{http.StatusNotFound, http.StatusOK} {
		for _, matched := range []bool{false, true} {
			t.Run(fmtCase(history, matched), func(t *testing.T) {
				writes, reads := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						writes++
						t.Errorf("unexpected write: %s", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/api/v2/history/tasks":
						if history == http.StatusNotFound {
							http.NotFound(w, r)
							return
						}
						_, _ = w.Write([]byte(`{"data":{"status":1},"error":false}`))
					case "/api/v2/list/goods/filter":
						reads++
						body := `{"data":{"listGoods":[]}}`
						if matched {
							body = `{"data":{"listGoods":[{"nmID":123,"sizes":[{"discountedPrice":1990}]}]}}`
						}
						_, _ = w.Write([]byte(body))
					default:
						t.Errorf("unexpected endpoint: %s", r.URL.Path)
					}
				}))
				defer server.Close()
				executor := NewMarketplaceExecutor("token", "", "")
				executor.wbBase, executor.client = server.URL, server.Client()
				item := procurement.ActionItem{Channel: "wb", ExternalArticle: "123", NewValue: 1990, ExternalOperationID: "42", Attempts: priceCheckMaxAttempts}
				result, err := executor.Execute(context.Background(), item)
				if matched && (err != nil || !result.Completed) {
					t.Fatalf("confirmed price=%+v %v", result, err)
				}
				if !matched && (err == nil || result.Completed || !strings.Contains(err.Error(), "Повторная проверка")) {
					t.Fatalf("missing card never terminates: %+v %v", result, err)
				}
				if writes != 0 || reads != 1 || result.ExternalOperationID != "42" {
					t.Fatalf("writes=%d reads=%d result=%+v", writes, reads, result)
				}
			})
		}
	}
}
func fmtCase(status int, matched bool) string {
	if status == http.StatusNotFound {
		if matched {
			return "missing_history_confirmed_price"
		}
		return "missing_history_missing_card"
	}
	if matched {
		return "pending_history_confirmed_price"
	}
	return "pending_history_missing_card"
}

type priceCooldownStub struct{ reserved bool }

func (s *priceCooldownStub) IntegrationRequestDelay(context.Context, string, string) (time.Duration, error) {
	return time.Hour, nil
}
func (s *priceCooldownStub) ReserveIntegrationRequest(context.Context, string, string, time.Duration) (time.Duration, error) {
	s.reserved = true
	return 0, nil
}
func (*priceCooldownStub) DeferIntegrationRequests(context.Context, string, string, time.Duration) error {
	return nil
}
func TestOzonCooldownDoesNotBlockOtherActionWork(t *testing.T) {
	limiter := &priceCooldownStub{}
	executor := NewMarketplaceExecutor("", "client", "secret").WithIntegrationRequestLimiter(limiter)
	called := false
	executor.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := executor.Execute(ctx, procurement.ActionItem{Channel: "ozon", ExternalArticle: "offer", NewValue: 1990})
	if err == nil || result.RetryAfter != time.Hour || called || limiter.reserved {
		t.Fatalf("result=%+v err=%v called=%v reserved=%v", result, err, called, limiter.reserved)
	}
}
