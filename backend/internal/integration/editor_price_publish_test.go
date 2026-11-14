package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

func TestEditorPriceKeepsWBTupleAndOzonOldPrice(t *testing.T) {
	for _, ch := range []string{"wb", "ozon"} {
		t.Run(ch, func(t *testing.T) {
			writes := 0
			readPrice := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/upload/task":
					writes++
					var b struct {
						Data []struct {
							ID       int64 `json:"nmID"`
							Price    int64 `json:"price"`
							Discount int64 `json:"discount"`
						} `json:"data"`
					}
					json.NewDecoder(r.Body).Decode(&b)
					if len(b.Data) != 1 || b.Data[0].ID != 123 || b.Data[0].Price != 2000 || b.Data[0].Discount != 25 {
						t.Errorf("lost WBdiscount: %+v", b)
					}
					fmt.Fprint(w, `{"data":{"id":45},"error":false}`)
				case "/v1/product/import/prices":
					writes++
					var b struct {
						Prices []map[string]string `json:"prices"`
					}
					json.NewDecoder(r.Body).Decode(&b)
					if len(b.Prices) != 1 || b.Prices[0]["price"] != "1500" || b.Prices[0]["old_price"] != "2000" || b.Prices[0]["offer_id"] != "123" {
						t.Errorf("lost Ozonoldprice: %+v", b)
					}
					fmt.Fprint(w, `{"result":[{"offer_id":"123","updated":true,"errors":[]}]}`)
				case "/api/v2/list/goods/filter":
					discount := 0
					if readPrice {
						discount = 25
					}
					fmt.Fprintf(w, `{"data":{"listGoods":[{"nmID":123,"discount":%d,"sizes":[{"price":2000,"discountedPrice":1500}]}]}}`, discount)
				case "/v5/product/info/prices":
					old := 0
					if readPrice {
						old = 2000
					}
					fmt.Fprintf(w, `{"items":[{"offer_id":"123","price":{"price":"1500","old_price":%q}}]}`, strconv.Itoa(old))
				default:
					t.Errorf("unexpected price operation %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			e := NewMarketplaceExecutor("wb", "id", "key")
			e.wbBase = server.URL
			e.ozonBase = server.URL
			p := &EditorPricePublisher{Marketplaces: e}
			compare := int64(200000)
			r := ce.Request{Action: "price", Price: &ce.PriceRequest{TargetID: "123", PriceMinor: 150000, CompareAtMinor: &compare}}
			state := ce.Progress{}
			for i := 0; i < 2; i++ {
				res, err := p.Advance(context.Background(), ch, r, state)
				if err != nil {
					t.Fatal(err)
				}
				state = res.Progress
			}
			if writes != 1 {
				t.Fatalf("price writes %d", writes)
			}
			res, err := p.Advance(context.Background(), ch, r, state)
			if err != nil || res.Status != "queued" {
				t.Fatalf("accepted only effective price: %+v %v", res, err)
			}
			readPrice = true
			res, err = p.Advance(context.Background(), ch, r, res.Progress)
			if err != nil || res.Status != "synced" || writes != 1 {
				t.Fatalf("wrong final tuple: %+v %v writes%d", res, err, writes)
			}
		})
	}
}

type editorAvitoStub struct {
	writes, reads int
	failWrite     bool
}

func (s *editorAvitoStub) Configured() bool { return true }
func (s *editorAvitoStub) UpdatePrice(context.Context, string, float64) error {
	s.writes++
	if s.failWrite {
		return errors.New("lost reply")
	}
	return nil
}
func (s *editorAvitoStub) ReadPrice(context.Context, string) (int64, error) {
	s.reads++
	return 150000, nil
}
func TestEditorAvitoLostPriceReplyOnlyReads(t *testing.T) {
	a := &editorAvitoStub{failWrite: true}
	p := &EditorPricePublisher{Avito: a}
	r := ce.Request{Action: "price", Price: &ce.PriceRequest{TargetID: "listing-1", PriceMinor: 150025}}
	s := ce.Progress{}
	for i := 0; i < 3; i++ {
		res, err := p.Advance(context.Background(), "avito", r, s)
		if err != nil {
			t.Fatal(err)
		}
		s = res.Progress
		if i == 2 && res.Status != "synced" {
			t.Fatalf("readback not confirmed: %+v", res)
		}
	}
	if a.writes != 1 || a.reads != 1 {
		t.Fatalf("ambiguous write replayed %d %d", a.writes, a.reads)
	}
	// Recovered expired write lease: verification from an empty remote receipt
	// must still perform only GET, even when there was no accepted HTTP reply.
	_, err := p.Advance(context.Background(), "avito", r, ce.Progress{Stage: "price-verify", Ambiguous: true})
	if err != nil || a.writes != 1 {
		t.Fatal("recovery sent a second price")
	}
}
func TestEditorSabyPriceOnlyPreservesCardAndNeverReplaysLostReply(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	f.lostUpdate = true
	p := &EditorPricePublisher{Saby: f.publisher().Client}
	beforeName := sabyRecordField(f.card, "Name")
	beforeCodes, _ := json.Marshal(sabyRecordField(f.card, "CodeList"))
	beforeImages, _ := json.Marshal(sabyRecordField(f.card, "ImageData"))
	r := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "Xfixture", PriceMinor: 149099}}
	s := ce.Progress{}
	status := ""
	for i := 0; i < 6; i++ {
		res, err := p.Advance(context.Background(), "saby", r, s)
		if err != nil {
			t.Fatal(err)
		}
		s = res.Progress
		status = res.Status
		if status == "synced" {
			break
		}
	}
	if status != "synced" || f.updates != 1 || f.creates != 0 || f.uploads != 0 {
		t.Fatalf("wrong price flow %s %+v writes%d creates%d", status, s, f.updates, f.creates)
	}
	codes, _ := json.Marshal(sabyRecordField(f.card, "CodeList"))
	images, _ := json.Marshal(sabyRecordField(f.card, "ImageData"))
	if sabyRecordField(f.card, "Name") != beforeName || string(codes) != string(beforeCodes) || string(images) != string(beforeImages) {
		t.Fatal("price-only write modified content or media")
	}
}

func TestContentSavePreservesApprovedMarketplacePriceTuple(t *testing.T) {
	for _, ch := range []string{"wb", "ozon"} {
		t.Run(ch, func(t *testing.T) {
			ctx := context.Background()
			cards := NewMockCatalog()
			r := cardFixture(ch)
			drive := func(req ce.Request) ce.Result {
				t.Helper()
				s := ce.Progress{}
				for i := 0; i < 25; i++ {
					res, err := cards.Advance(ctx, ch, req, s)
					if err != nil {
						t.Fatal(err)
					}
					s = res.Progress
					if res.Status == "created" || res.Status == "synced" {
						return res
					}
					if res.Status != "queued" {
						t.Fatalf("content failed: %+v", res)
					}
				}
				t.Fatal("content did not finish")
				return ce.Result{}
			}
			created := drive(r)
			price := &EditorPricePublisher{Marketplaces: cards.Executor, Mock: true}
			compare := int64(200000)
			target := created.Progress.IDs.ProductID
			if ch == "ozon" {
				target = created.Progress.IDs.OfferID
			}
			pr := ce.Request{Action: "price", Price: &ce.PriceRequest{TargetID: target, PriceMinor: 150000, CompareAtMinor: &compare}}
			state := ce.Progress{}
			for i := 0; i < 4; i++ {
				res, err := price.Advance(ctx, ch, pr, state)
				if err != nil {
					t.Fatal(err)
				}
				state = res.Progress
				if res.Status == "synced" {
					break
				}
			}
			if state.Stage != "done" {
				t.Fatal("price was not verified")
			}
			r.Action = "update"
			r.ExistingID = target
			r.Marketplace.Name = "Updated content"
			r.WBPrice = "9999"
			r.OzonPrice = "9999"
			drive(r)
			res, err := price.Advance(ctx, ch, pr, ce.Progress{Stage: "price-verify"})
			if err != nil || res.Status != "synced" {
				t.Fatalf("content overwrote approved tuple: %+v %v", res, err)
			}
		})
	}
}
func TestSabyContentSavePreservesExistingPrice(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	before, _ := json.Marshal(sabyRecordField(f.card, "PriceData"))
	r := sabyEditorRequestFixture()
	r.Action = "update"
	r.SabyID = "1101"
	r.PriceMinor = 999900
	res := driveSabyEditorFixture(t, f.publisher(), r, ce.Progress{})
	after, _ := json.Marshal(sabyRecordField(f.card, "PriceData"))
	if res.Status != "synced" || string(before) != string(after) {
		t.Fatal("content save changed price without price approval")
	}
}
func TestEditorMarketplaceLostPriceRepliesOnlyRead(t *testing.T) {
	for _, ch := range []string{"wb", "ozon"} {
		t.Run(ch, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/upload/task" || r.URL.Path == "/v1/product/import/prices" {
					writes++
					w.WriteHeader(500)
					fmt.Fprint(w, `{"error":"lost reply"}`)
					return
				}
				if ch == "wb" {
					fmt.Fprint(w, `{"data":{"listGoods":[{"nmID":123,"discount":25,"sizes":[{"price":2000,"discountedPrice":1500}]}]}}`)
				} else {
					fmt.Fprint(w, `{"items":[{"offer_id":"123","price":{"price":"1500","old_price":"2000"}}]}`)
				}
			}))
			defer server.Close()
			e := NewMarketplaceExecutor("wb", "id", "key")
			e.wbBase = server.URL
			e.ozonBase = server.URL
			p := &EditorPricePublisher{Marketplaces: e}
			compare := int64(200000)
			r := ce.Request{Action: "price", Price: &ce.PriceRequest{TargetID: "123", PriceMinor: 150000, CompareAtMinor: &compare}}
			s := ce.Progress{}
			for i := 0; i < 3; i++ {
				res, err := p.Advance(context.Background(), ch, r, s)
				if err != nil {
					t.Fatal(err)
				}
				s = res.Progress
				if i == 2 && res.Status != "synced" {
					t.Fatalf("unknown price not verified: %+v", res)
				}
			}
			if writes != 1 {
				t.Fatalf("ambiguous write replayed %d times", writes)
			}
		})
	}
}

func TestEditorOzonPricePreservesApprovedKopecks(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/product/import/prices":
			writes++
			var body struct {
				Prices []map[string]string `json:"prices"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Prices) != 1 || body.Prices[0]["price"] != "123.45" || body.Prices[0]["old_price"] != "199.99" {
				t.Errorf("truncated approved price: %+v", body)
			}
			fmt.Fprint(w, `{"result":[{"offer_id":"exact","updated":true,"errors":[]}]}`)
		case "/v5/product/info/prices":
			fmt.Fprint(w, `{"items":[{"offer_id":"exact","price":{"price":"123.45","old_price":"199.99"}}]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	e := NewMarketplaceExecutor("", "id", "key")
	e.ozonBase = server.URL
	p := &EditorPricePublisher{Marketplaces: e}
	old := int64(19999)
	r := ce.Request{Action: "price", Price: &ce.PriceRequest{TargetID: "exact", PriceMinor: 12345, CompareAtMinor: &old}}
	s := ce.Progress{}
	for i := 0; i < 3; i++ {
		res, err := p.Advance(context.Background(), "ozon", r, s)
		if err != nil {
			t.Fatal(err)
		}
		s = res.Progress
		if i == 2 && res.Status != "synced" {
			t.Fatalf("exact cents not confirmed %+v", res)
		}
	}
	if writes != 1 {
		t.Fatal("price re-sent")
	}
}
func TestEditorMissingPriceKeysAreKnownFailureUntilWriteWasAttempted(t *testing.T) {
	p := &EditorPricePublisher{}
	r := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "123", PriceMinor: 150000}}
	for _, ch := range []string{"wb", "ozon", "saby", "avito"} {
		t.Run(ch, func(t *testing.T) {
			res, err := p.Advance(context.Background(), ch, r, ce.Progress{Stage: "price-send"})
			if err != nil || res.Status != "failed" || res.Progress.Stage != "price-rejected" || res.Progress.Ambiguous {
				t.Fatalf("pre-HTTP config became unknown: %+v %v", res, err)
			}
			res, err = p.Advance(context.Background(), ch, r, ce.Progress{Stage: "price-verify", Ambiguous: true})
			if err == nil || res.Progress.Stage != "price-verify" || !res.Progress.Ambiguous {
				t.Fatalf("lost ambiguous receipt when keys removed: %+v %v", res, err)
			}
		})
	}
}

func TestEditorSabyPriceDoesNotPublishRemoteDraft(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	for i, raw := range f.card["s"].([]any) {
		if raw.(map[string]any)["n"] == "IsDraft" {
			f.card["d"].([]any)[i] = true
		}
	}
	p := &EditorPricePublisher{Saby: f.publisher().Client}
	r := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "Xfixture", PriceMinor: 149099}}
	res, err := p.Advance(context.Background(), "saby", r, ce.Progress{Stage: "price-send"})
	if err != nil || res.Status != "needs_attention" || f.updates != 0 || f.creates != 0 {
		t.Fatalf("price action published a draft %+v %v", res, err)
	}
}
