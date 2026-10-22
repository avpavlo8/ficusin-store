package analytics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMetrikaOwnershipCannotBeClaimedByBrowser(t *testing.T) {
	s := NewStore(nil)
	w := s.WithMetrika("secret", "https://ficusin.ru", func() string { return "111907858" })
	for _, test := range []struct {
		name    string
		allowed bool
		cid     string
		want    bool
	}{
		{"real visit", true, "1234567890123456789", true}, {"privacy", false, "12345", false}, {"no counter cookie", true, "", false}, {"malformed", true, "1&ea=bad", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := w.orderAttribution(Attribution{AnalyticsAllowed: test.allowed, MetrikaClientID: test.cid, MetrikaServer: true, MetrikaCounterID: "attacker"})
			if a.MetrikaServer != test.want {
				t.Fatalf("ownership=%+v", a)
			}
			if a.MetrikaServer && a.MetrikaCounterID != "111907858" {
				t.Fatal(a)
			}
		})
	}
	var disabled *MetrikaWorker
	if a := disabled.orderAttribution(Attribution{AnalyticsAllowed: true, MetrikaClientID: "123", MetrikaServer: true}); a.MetrikaServer {
		t.Fatal("disabled worker accepted ownership")
	}
}

func TestMetrikaDeliveryWireAndResponse(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		code                 int
		body, status, reason string
	}{
		{"accepted", 200, "<!-- OK -->", "sent", ""}, {"misleading200", 200, "<html>login</html>", "uncertain", "response_unknown_200"}, {"invalidsecret", 400, "secret must not leak", "failed", "rejected_400"}, {"provideroutage", 503, "", "uncertain", "response_unknown_503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Query().Get("ms") != "secret" || r.URL.Query().Get("tid") != "111907858" {
					t.Error("wrong request envelope")
				}
				body, _ := io.ReadAll(r.Body)
				hits := strings.Split(string(body), "\n")
				if len(hits) != 3 {
					t.Fatalf("hits=%d", len(hits))
				}
				for i, encoded := range hits {
					v, err := url.ParseQuery(encoded)
					if err != nil {
						t.Fatal(err)
					}
					if v.Get("cid") != "123456" || v.Get("dl") != "https://ficusin.ru/checkout" {
						t.Error(v)
					}
					if strings.Contains(encoded, "ORDER-CAPABILITY") || strings.Contains(encoded, "secret") {
						t.Fatal("private value exported")
					}
					if i == 0 && v.Get("t") != "pageview" {
						t.Error(v)
					}
					if i == 1 && (v.Get("ea") != "paid_order" || v.Get("ev") != "7450.00") {
						t.Error(v)
					}
					if i == 2 && (v.Get("ti") != "opaque-uuid" || v.Get("tr") != "7450.00") {
						t.Error(v)
					}
				}
				res.WriteHeader(tc.code)
				_, _ = res.Write([]byte(tc.body))
			}))
			defer server.Close()
			w := NewStore(nil).WithMetrika("secret", "https://ficusin.ru", func() string { return "111907858" })
			w.endpoint = server.URL
			status, reason := w.send(context.Background(), metrikaDelivery{EventID: "opaque-uuid", CounterID: "111907858", ClientID: "123456", Amount: "7450.00", Attempts: 1})
			if status != tc.status || reason != tc.reason {
				t.Fatalf("got %s/%s", status, reason)
			}
		})
	}
}

func TestMetrikaAmbiguousTransportIsNotRetried(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, r *http.Request) {
		h := res.(http.Hijacker)
		conn, _, err := h.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	w := NewStore(nil).WithMetrika("SECRET-MUST-NOT-LEAK", "https://ficusin.ru", func() string { return "111907858" })
	w.endpoint = server.URL
	status, reason := w.send(context.Background(), metrikaDelivery{CounterID: "111907858", ClientID: "123", Amount: "100.00", Attempts: 1})
	if status != "uncertain" || reason != "transport_unknown" {
		t.Fatalf("got %s/%s", status, reason)
	}
}
