package httpapi

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

type accessPaymentStub struct {
	paymentService
	started int
	owner   int64
	token   string
}

func (*accessPaymentStub) Configured() bool { return true }
func (s *accessPaymentStub) StartOutstanding(context.Context, string) (string, payment.Balance, error) {
	s.started++
	return "https://payments.example.invalid", payment.Balance{Ready: true, Due: 100}, nil
}
func (s *accessPaymentStub) AuthorizeOrderPayment(_ context.Context, number, token string, id int64) (bool, error) {
	return number == "0000-1" && (s.owner > 0 && s.owner == id || s.owner == 0 && token != "" && token == s.token), nil
}
func TestPaymentAccessRejectsOtherCustomerAndGuessedNumber(t *testing.T) {
	for _, tc := range []struct {
		name            string
		owner, user     int64
		token, provided string
		want            int
	}{
		{"guest missing token", 0, 0, "secret", "", 404},
		{"guest wrong token", 0, 0, "secret", "wrong", 404},
		{"guest token", 0, 0, "secret", "secret", 200},
		{"owner", 42, 42, "", "", 200},
		{"different customer", 42, 43, "", "", 404},
		{"linked order rejects former guest token", 42, 0, "secret", "secret", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &accessPaymentStub{owner: tc.owner, token: tc.token}
			authentication := profileAuthStub{user: &auth.User{ID: tc.user}}
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetPathValue("orderNumber", "0000-1")
			req.Header.Set("X-Order-Payment-Token", tc.provided)
			if tc.user > 0 {
				req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "session"})
			}
			res := httptest.NewRecorder()
			startPaymentHandler(discardLogger(), authentication, provider).ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if tc.want != 200 && provider.started != 0 {
				t.Fatal("unauthorized request reached provider")
			}
			if tc.want == 200 && provider.started != 1 {
				t.Fatal("authorized request did not start the outstanding balance payment")
			}
		})
	}
}
