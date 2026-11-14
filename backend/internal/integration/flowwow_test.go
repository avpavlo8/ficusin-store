package integration

import (
	"context"
	"encoding/json"
	"errors"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"io"
	"net/http"
	"strings"
	"testing"
)

type flowwowRoundTrip func(*http.Request) (*http.Response, error)

func (f flowwowRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func flowwowFixtureRequest() ce.Request {
	return ce.Request{SKU: "SITE-1", Name: "Фикус с сайта", Description: "Описание сайта", PriceMinor: 123456, Images: []string{"https://images.example.test/plant.jpg"}, Channel: ce.Channel{Category: ce.Category{ID: "101"}, Attributes: map[string]any{"height": map[string]any{"val": 30, "measure": 3}, "stock": 5}}}
}
func TestFlowwowCreateUpdateAndInventory(t *testing.T) {
	ctx := context.Background()
	c := NewMockFlowwowClient()
	r := flowwowFixtureRequest()
	p := ce.Progress{}
	for i := 0; i < 3; i++ {
		out, e := c.Advance(ctx, "flowwow", r, p)
		if e != nil {
			t.Fatal(e)
		}
		p = out.Progress
		if i == 0 && p.Stage != "create" {
			t.Fatalf("stage %s", p.Stage)
		}
		if i == 2 && out.Status != "synced" {
			t.Fatalf("result %+v", out)
		}
	}
	remote, e := c.find(ctx, "SITE-1", "")
	if e != nil || remote == nil || remote.Name != r.Name || remote.Stock != 5 {
		t.Fatalf("remote %+v %v", remote, e)
	}
	id := remote.ProductID
	r.Name = "Новое название"
	r.Description = "Новое описание"
	r.PriceMinor = 223344
	r.Action = "update"
	p = ce.Progress{}
	for i := 0; i < 3; i++ {
		out, e := c.Advance(ctx, "flowwow", r, p)
		if e != nil {
			t.Fatal(e)
		}
		p = out.Progress
	}
	remote, _ = c.find(ctx, "SITE-1", "")
	if remote.ProductID != id || remote.Name != r.Name || remote.Price != "2233.44" {
		t.Fatalf("update %+v", remote)
	}
	if e = c.SyncInventory(ctx, "SITE-1", 340099, 0); e != nil {
		t.Fatal(e)
	}
	remote, _ = c.find(ctx, "SITE-1", "")
	if remote.Price != "3400.99" || remote.Stock != 0 {
		t.Fatalf("inventory %+v", remote)
	}
}
func TestFlowwowLostCreateIsNeverReplayed(t *testing.T) {
	c := NewMockFlowwowClient()
	transport := c.client.Transport
	creates := 0
	c.client.Transport = flowwowRoundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/create") {
			creates++
			_, e := transport.RoundTrip(r)
			if e != nil {
				t.Fatal(e)
			}
			return nil, errors.New("lost response")
		}
		return transport.RoundTrip(r)
	})
	req := flowwowFixtureRequest()
	out, e := c.Advance(context.Background(), "flowwow", req, ce.Progress{})
	if e != nil {
		t.Fatal(e)
	}
	p := out.Progress
	p.CreateAttempted = true
	_, e = c.Advance(context.Background(), "flowwow", req, p)
	if e == nil {
		t.Fatal("expected lost response")
	}
	p.Stage = "reconcile"
	out, e = c.Advance(context.Background(), "flowwow", req, p)
	if e != nil || out.Progress.Stage != "update" || creates != 1 {
		t.Fatalf("%+v %v creates=%d", out, e, creates)
	}
	p = ce.Progress{Stage: "reconcile", CreateAttempted: true}
	req.SKU = "MISSING"
	out, e = c.Advance(context.Background(), "flowwow", req, p)
	if e != nil || out.Status != "needs_attention" || creates != 1 {
		t.Fatalf("missing %+v %v creates=%d", out, e, creates)
	}
}
func TestFlowwowBatchErrorAndIdentityValidation(t *testing.T) {
	for _, response := range []string{`{"data":[],"errors":[{"offerId":"SITE-1","message":"invalid field"}]}`, `{"data":[{"offerId":"OTHER","productId":1}]}`, `{}`, `not json`} {
		c := NewFlowwowClient("secret", "10")
		c.client.Transport = flowwowRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("shopId") != "10" {
				t.Fatal("wrong authorization/scope")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
		})
		_, e := c.batch(context.Background(), "PUT", "prices/put", "offers", map[string]any{"offerId": "SITE-1", "price": "100.00"})
		if e == nil {
			t.Fatal("accepted failed/unconfirmed batch", response)
		}
	}
}
func TestFlowwowSchemaAndImageValidation(t *testing.T) {
	var fields []FlowwowField
	if e := json.Unmarshal([]byte(`[{"field":"stock","name":"Остаток","type":"decimal","required":true,"minValue":1,"dependsOn":["productionTime"]},{"field":"productionTime","name":"Срок","type":"measure","required":true,"dependsOn":["stock"],"measure":[{"id":7}]}]`), &fields); e != nil {
		t.Fatal(e)
	}
	if e := ValidateFlowwowFields(fields, map[string]any{"stock": 2}); e != nil {
		t.Fatal(e)
	}
	for _, values := range []map[string]any{{}, {"stock": 0}, {"stock": 2, "productionTime": map[string]any{"val": 30, "measure": 7}}, {"productionTime": map[string]any{"val": 30, "measure": 99}}} {
		if e := ValidateFlowwowFields(fields, values); e == nil {
			t.Fatal("accepted invalid fields", values)
		}
	}
	for _, s := range []string{"http://example.com/a.jpg", "https://localhost/a.jpg", "https://127.0.0.1/a.jpg", "https://10.0.0.1/a.jpg", "https://[::1]/a.jpg", "https://169.254.169.254/a.jpg", "https://a:b@example.com/a.jpg"} {
		if publicFlowwowImage(s) {
			t.Fatal("accepted private image", s)
		}
	}
}
func TestFlowwowUpdateCannotMoveCategoryOrCreateMissingLinkedCard(t *testing.T) {
	c := NewMockFlowwowClient()
	r := flowwowFixtureRequest()
	r.Action = "update"
	out, e := c.Advance(context.Background(), "flowwow", r, ce.Progress{})
	if e != nil || out.Status != "failed" || out.Progress.Stage == "create" {
		t.Fatalf("%+v %v", out, e)
	}
	_, e = c.productPayload(context.Background(), r, "SITE-1", &FlowwowProduct{Type: 1, SubCategoryID: 999})
	if e == nil {
		t.Fatal("allowed category change")
	}
}

func TestFlowwowRespectsRetryAfterWithoutResending(t *testing.T) {
	c := NewFlowwowClient("secret", "1")
	calls := 0
	c.client.Transport = flowwowRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader(`{"message":"slow down"}`))}, nil
	})
	for i := 0; i < 2; i++ {
		_, _, err := c.Products(context.Background(), map[string]any{})
		var remote *FlowwowError
		if !errors.As(err, &remote) || remote.Status != 429 {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("ignored Retry-After", calls)
	}
}

func TestFlowwowDefinitiveCreateRejectionCanBeCorrected(t *testing.T) {
	for _, status := range []int{200, 400, 429} {
		c := NewMockFlowwowClient()
		req := flowwowFixtureRequest()
		prepared, e := c.Advance(context.Background(), "flowwow", req, ce.Progress{})
		if e != nil {
			t.Fatal(e)
		}
		c.client.Transport = flowwowRoundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{"errors":[{"offerId":"SITE-1","message":"invalid field"}]}`))}, nil
		})
		p := prepared.Progress
		p.CreateAttempted = true
		out, e := c.Advance(context.Background(), "flowwow", req, p)
		if e != nil || out.Progress.CreateAttempted {
			t.Fatalf("status=%d result=%+v err=%v", status, out, e)
		}
		if status == 429 {
			if out.Status != "queued" || out.Progress.Stage != "create" {
				t.Fatal(out)
			}
		} else if out.Status != "failed" || out.Progress.Stage != "" {
			t.Fatal(out)
		}
	}
}
