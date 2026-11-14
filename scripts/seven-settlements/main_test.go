package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T) (approved, integration.SettlementReceiptRequest) {
	t.Helper()
	a := scope[0]
	client := integration.NewYooKassaClient("fixture", "fixture", 0, 1)
	original := integration.PaymentRequest{Email: "fixture@example.invalid", Items: []integration.PaymentItem{{Name: "Fixture", Price: 4620, Quantity: 1, VATCode: 1, Subject: "commodity", Mode: "full_prepayment"}}}
	req, err := (&guardedProvider{YooKassaClient: client}).PrepareSettlementReceipt(a.Provider, "fixture-stable-key", original, original.Items)
	if err != nil {
		t.Fatal(err)
	}
	return a, req
}
func TestScopeIsExactlyOwnerApproved(t *testing.T) {
	seen := map[int64]bool{}
	var total int64
	for _, a := range scope {
		if seen[a.Payment] || a.Order <= 0 || a.Provider == "" {
			t.Fatal("duplicate or incomplete scope")
		}
		seen[a.Payment] = true
		total += a.Minor
	}
	if len(scope) != 7 || total != 5704600 {
		t.Fatal("approved scope changed")
	}
}

func TestExactNumericAndLegacyStringQuantities(t *testing.T) {
	for _, raw := range []string{`{"quantity":1}`, `{"quantity":"1.000"}`} {
		var got item
		if err := json.Unmarshal([]byte(raw), &got); err != nil || !equalDecimal(string(got.Quantity), "1") {
			t.Fatalf("quantity decode: %v", err)
		}
	}
	for _, raw := range []string{`{"quantity":null}`, `{"quantity":true}`, `{"quantity":"not-a-number"}`} {
		var got item
		if json.Unmarshal([]byte(raw), &got) == nil {
			t.Fatal("invalid quantity accepted")
		}
	}
	_, req := fixture(t)
	body := jsonBody(req.Body)
	if body["items"].([]any)[0].(map[string]any)["measure"] != "piece" {
		t.Fatal("counted unit missing from durable request")
	}
}
func TestBodyRejectsWrongPaymentAmountAndCashSettlement(t *testing.T) {
	a, req := fixture(t)
	if _, err := checkBody(req, a); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){
		func(b map[string]any) { b["payment_id"] = "different" },
		func(b map[string]any) {
			b["settlements"] = []any{map[string]any{"type": "cashless", "amount": map[string]string{"value": "4620.00", "currency": "RUB"}}}
		},
		func(b map[string]any) {
			b["settlements"] = []any{map[string]any{"type": "prepayment", "amount": map[string]string{"value": "4621.00", "currency": "RUB"}}}
		},
		func(b map[string]any) { b["items"].([]any)[0].(map[string]any)["payment_mode"] = "full_prepayment" },
	} {
		copy := req
		copy.Body = jsonBody(req.Body)
		mutate(copy.Body)
		if _, err := checkBody(copy, a); err == nil {
			t.Fatal("accepted altered financial request")
		}
	}
}
func TestProviderComparisonChecksVATQuantityAndMode(t *testing.T) {
	a, req := fixture(t)
	want, err := checkBody(req, a)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]item{}, want...)
	got[0].Mode = "full_prepayment"
	got[0].Quantity = "1.000"
	if !matchingItems(got, want, "full_prepayment") {
		t.Fatal("equivalent quantity rejected")
	}
	got[0].VAT = 2
	if matchingItems(got, want, "full_prepayment") {
		t.Fatal("VAT changed")
	}
	got[0].VAT = 1
	got[0].Quantity = "2"
	if matchingItems(got, want, "full_prepayment") {
		t.Fatal("quantity changed")
	}
}
func TestExistingClosingReceiptIsAdoptedWithoutAnotherPost(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-send", true: "existing-receipt"}[existing], func(t *testing.T) {
			a, req := fixture(t)
			items, _ := checkBody(req, a)
			pre := append([]item{}, items...)
			pre[0].Mode = "full_prepayment"
			posts := 0
			tr := transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.yookassa.ru" {
					t.Fatal("unexpected provider")
				}
				var out any
				switch {
				case r.Method == "GET" && r.URL.Path == "/v3/payments/"+a.Provider:
					out = map[string]any{"id": a.Provider, "status": "succeeded", "paid": true, "test": false, "receipt_registration": "succeeded", "amount": map[string]string{"value": "4620.00", "currency": "RUB"}}
				case r.Method == "GET" && r.URL.Path == "/v3/receipts":
					receipts := []any{map[string]any{"id": "prepayment", "payment_id": a.Provider, "type": "payment", "status": "succeeded", "items": pre}}
					if existing {
						receipts = append(receipts, map[string]any{"id": "existing-final", "payment_id": a.Provider, "type": "payment", "status": "succeeded", "items": items, "settlements": req.Body["settlements"]})
					}
					out = map[string]any{"items": receipts}
				case r.Method == "POST" && r.URL.Path == "/v3/receipts":
					posts++
					if r.Header.Get("Idempotence-Key") != req.Key {
						t.Fatal("idempotence key changed")
					}
					out = map[string]any{"id": "created-final", "payment_id": a.Provider, "type": "payment", "status": "pending"}
				default:
					t.Fatalf("unexpected operation %s %s", r.Method, r.URL.Path)
				}
				raw, _ := json.Marshal(out)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}}, nil
			})
			saved := http.DefaultTransport
			http.DefaultTransport = tr
			defer func() { http.DefaultTransport = saved }()
			p := &guardedProvider{YooKassaClient: integration.NewYooKassaClient("fixture", "fixture", 0, 1), shop: "fixture", secret: "fixture", http: &http.Client{Transport: tr}, allowed: map[string]approved{a.Provider: a}}
			receipt, err := p.CreateSettlementReceipt(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if existing && (posts != 0 || p.adopted != 1 || receipt.ID != "existing-final") {
				t.Fatal("duplicated existing receipt")
			}
			if !existing && (posts != 1 || p.posts != 1 || receipt.ID != "created-final") {
				t.Fatal("first receipt not sent exactly once")
			}
		})
	}
}
