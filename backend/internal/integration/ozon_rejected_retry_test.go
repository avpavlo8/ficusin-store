package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

type ozonRetryTransport func(*http.Request) (*http.Response, error)

func (f ozonRetryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func rejectedOzonFixture() (*CatalogPublisher, *catalogMockTransport, ce.Request, ce.Progress) {
	p := NewMockCatalog()
	transport := p.Executor.client.Transport.(*catalogMockTransport)
	r := cardFixture("ozon")
	r.Packaging.Weight = 1000
	transport.cards["ozon:"+r.SellerArticle("ozon")] = mockCard{ID: 990000111, Offer: r.SellerArticle("ozon"), Barcode: r.Barcode, Images: r.Images, Document: map[string]any{"weight": float64(1)}}
	return p, transport, r, ce.Progress{Stage: "reconcile", CreateAttempted: true, ReapplyPayload: true, TaskID: 111}
}

func TestOzonRejectedDraftReimportsCorrectedPayloadAfterIdentityReadback(t *testing.T) {
	p, transport, r, progress := rejectedOzonFixture()
	reads, writes := 0, 0
	p.Executor.client.Transport = ozonRetryTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v3/product/info/list" {
			reads++
		}
		if request.URL.Path == "/v3/product/import" {
			writes++
			if reads < 2 {
				t.Error("corrected import happened before both ownership checks")
			}
		}
		return transport.RoundTrip(request)
	})
	first, err := p.Advance(context.Background(), "ozon", r, progress)
	if err != nil || first.Progress.Stage != "update" || writes != 0 {
		t.Fatalf("reconciliation skipped update: %+v %v", first, err)
	}
	progress = first.Progress
	for range 5 {
		result, err := p.Advance(context.Background(), "ozon", r, progress)
		if err != nil {
			t.Fatal(err)
		}
		progress = result.Progress
		if result.Status == "created" {
			break
		}
	}
	card := transport.cards["ozon:"+r.SellerArticle("ozon")]
	if writes != 1 || len(transport.cards) != 1 || card.ID != 990000111 || card.Document["weight"] != float64(1000) || card.Document["weight_unit"] != "g" {
		t.Fatalf("old rejected payload was merely read back or a second card created: writes=%d card=%+v", writes, card)
	}
}

func TestOzonRejectedRetryNeverAdoptsAnUnprovenArticle(t *testing.T) {
	for _, name := range []string{"no prior attempt", "wrong barcode", "missing barcode", "different remote id", "not found"} {
		t.Run(name, func(t *testing.T) {
			p, transport, r, progress := rejectedOzonFixture()
			card := transport.cards["ozon:"+r.SellerArticle("ozon")]
			switch name {
			case "no prior attempt":
				progress.CreateAttempted = false
			case "wrong barcode":
				card.Barcode = "foreign"
			case "missing barcode":
				card.Barcode = ""
			case "different remote id":
				progress.IDs.ProductID = "foreign-id"
			case "not found":
				delete(transport.cards, "ozon:"+r.SellerArticle("ozon"))
			}
			if name != "not found" {
				transport.cards["ozon:"+r.SellerArticle("ozon")] = card
			}
			writes := 0
			p.Executor.client.Transport = ozonRetryTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/v3/product/import" {
					writes++
				}
				return transport.RoundTrip(request)
			})
			result, err := p.Advance(context.Background(), "ozon", r, progress)
			if name == "not found" {
				if err != nil || result.Progress.Stage != "reconcile" {
					t.Fatalf("unknown outcome entered another create: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("unproven existing article accepted")
			}
			if writes != 0 {
				t.Fatal("unproven ownership reached import")
			}
		})
	}
}

func TestOzonCorrectedImportUnknownOutcomeNeverRepostsBlindly(t *testing.T) {
	p, transport, r, progress := rejectedOzonFixture()
	writes, reads := 0, 0
	p.Executor.client.Transport = ozonRetryTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v3/product/info/list" {
			reads++
		}
		response, err := transport.RoundTrip(request)
		if request.URL.Path == "/v3/product/import" {
			writes++
			if response != nil {
				response.Body.Close()
			}
			return nil, errors.New("lost response after accepting corrected import")
		}
		return response, err
	})
	result, err := p.Advance(context.Background(), "ozon", r, progress)
	if err != nil {
		t.Fatal(err)
	}
	result, err = p.Advance(context.Background(), "ozon", r, result.Progress)
	if err != nil || result.Progress.Stage != "ozon-reapply-unknown" {
		t.Fatalf("unknown import did not pause: %+v %v", result, err)
	}
	for range 2 {
		result, err = p.Advance(context.Background(), "ozon", r, result.Progress)
		if err != nil || result.Status != "needs_attention" {
			t.Fatalf("unknown update not surfaced: %+v %v", result, err)
		}
	}
	if writes != 1 || reads < 4 {
		t.Fatalf("unknown import replayed or readback skipped: %d/%d", writes, reads)
	}
}
