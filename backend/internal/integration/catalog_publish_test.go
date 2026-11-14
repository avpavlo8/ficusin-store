package integration

import (
	"context"
	"encoding/json"
	"errors"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cardFixture(ch string) ce.Request {
	r := ce.Request{SiteAttributes: map[string]any{"pot_diameter_cm": float64(13)}, Name: "Нолина", Description: "Описание растения", Barcode: "2000000000015", Images: []string{"https://images.example.test/cover.jpg", "https://images.example.test/variant.jpg"}, Packaging: ce.Packaging{Season: "winter", Length: 30.5, Width: 20, Height: 45, Weight: 1250}, Marketplace: ce.Marketplace{Name: "Название площадки", Description: "Описание площадки", Article: "fixture-13", Brand: "Фикусин", BrandID: 900101, TNVED: "0602909900", TNVEDID: 900102, Model: "Нолина", Units: 1}, OzonPrice: "5000.25", WBPrice: "4000.10"}
	r.Channel = ce.Channel{Mode: "create", Enabled: true, Category: ce.Category{ID: "900001", TypeID: 900002}, Attributes: map[string]any{"90002": []ce.Value{{Value: "Россия", DictionaryID: 900103}}}}
	r.Marketplace.Ozon = r.Channel
	r.Schema = []ce.Attribute{{ID: 85, Name: "Бренд", DictionaryID: 85, IsRequired: true}, {ID: 22232, Name: "Код ТН ВЭД", DictionaryID: 22232, IsRequired: true}, {ID: 8229, Name: "Тип", DictionaryID: 8229, IsRequired: true}, {ID: 9048, Name: "Название модели", IsRequired: true}, {ID: 4191, Name: "Аннотация", IsRequired: true}, {ID: 90001, Name: "Единиц в одном товаре", Type: "Integer", IsRequired: true}, {ID: 90002, Name: "Страна производства", DictionaryID: 90002, IsRequired: true}}
	if ch == "wb" {
		r.Channel.Category = ce.Category{ID: "900003"}
		r.Channel.Attributes = map[string]any{"15000001": "Россия", "15000002": "99"}
		r.Schema = []ce.Attribute{{CharcID: 15000001, Name: "Страна производства", Required: true, CharcType: 1, MaxCount: 1}, {CharcID: 15000002, Name: "Диаметр горшка", Required: true, CharcType: 4, MaxCount: 1}, {CharcID: 15000003, Name: "Код ТН ВЭД", Required: true, CharcType: 1, MaxCount: 1}}
	}
	return r
}
func TestCatalogPayloadUsesSharedContentAndFrozenPackaging(t *testing.T) {
	o, e := BuildOzonCard(cardFixture("ozon"), false)
	if e != nil {
		t.Fatal(e)
	}
	item := o["items"].([]any)[0].(map[string]any)
	if item["name"] != "Название площадки" || item["depth"] != int64(305) || item["weight"] != int64(1250) || item["price"] != "5000.25" || item["vat"] != "0" {
		t.Fatalf("incorrect Ozon payload: %#v", item)
	}
	attrs := item["attributes"].([]map[string]any)
	found := false
	for _, a := range attrs {
		if a["id"] == int64(90002) {
			found = true
			values := a["values"].([]map[string]any)
			if values[0]["dictionary_value_id"] != int64(900103) {
				t.Fatal("dictionary ID lost")
			}
		}
	}
	if !found {
		t.Fatal("required country missing")
	}
	w, e := BuildWBCard(cardFixture("wb"), false)
	if e != nil {
		t.Fatal(e)
	}
	v := w[0]["variants"].([]any)[0].(map[string]any)
	dims := v["dimensions"].(map[string]any)
	if dims["length"] != int64(31) || dims["weightBrutto"] != 1.25 {
		t.Fatalf("WB units incorrect: %#v", dims)
	}
	for _, a := range v["characteristics"].([]map[string]any) {
		if a["id"] == int64(15000002) && a["value"] != float64(13) {
			t.Fatal("WB numeric characteristic became text")
		}
	}
}
func TestOzonTransportWeightPreservesCanonicalGrams(t *testing.T) {
	for _, grams := range []float64{1, 1000, 1250} {
		r := cardFixture("ozon")
		r.Packaging.Weight = grams
		body, err := BuildOzonCard(r, false)
		if err != nil {
			t.Fatal(err)
		}
		item := body["items"].([]any)[0].(map[string]any)
		if item["weight"] != int64(grams) || item["weight_unit"] != "g" {
			t.Fatalf("canonical grams converted twice: %#v", item)
		}
	}
}

func TestCatalogRejectsInvalidDictionaryAndPrivateImages(t *testing.T) {
	r := cardFixture("ozon")
	r.Channel.Attributes["90002"] = "Россия"
	if _, e := BuildOzonCard(r, false); e == nil {
		t.Fatal("dictionary text accepted without ID")
	}
	r = cardFixture("ozon")
	r.Images = []string{"http://127.0.0.1:8092/a.jpg"}
	if _, e := BuildOzonCard(r, false); e == nil {
		t.Fatal("local image accepted for live API")
	}
	if _, e := BuildOzonCard(r, true); e != nil {
		t.Fatal(e)
	}
}
func TestCatalogMockRunsFullVendorProtocolsAndLinksWithoutCreating(t *testing.T) {
	for _, channel := range []string{"ozon", "wb"} {
		t.Run(channel, func(t *testing.T) {
			p := NewMockCatalog()
			r := cardFixture(channel)
			s := ce.Progress{}
			writes := 0
			for i := 0; i < 12; i++ {
				result, e := p.Advance(context.Background(), channel, r, s)
				if e != nil {
					t.Fatal(e)
				}
				if s.Stage == "create" {
					writes++
				}
				s = result.Progress
				if result.Status == "created" {
					break
				}
			}
			if s.IDs.ProductID == "" || writes != 1 {
				t.Fatalf("protocol did not finish exactly once: %+v writes=%d", s, writes)
			}
			_, e := p.Advance(context.Background(), channel, r, ce.Progress{})
			if e == nil || !strings.Contains(e.Error(), "уже существует") {
				t.Fatalf("duplicate article was not blocked: %v", e)
			}
			r.Channel.Mode = "link"
			r.Channel.ExistingID = r.Marketplace.Article
			r.Schema = nil
			r.Images = nil
			result, e := p.Advance(context.Background(), channel, r, ce.Progress{})
			if e != nil || result.Status != "linked" || result.Progress.IDs.ProductID != s.IDs.ProductID {
				t.Fatalf("link failed: %+v %v", result, e)
			}
			// A restarted worker must reconcile a submitted request, not create again.
			r = cardFixture(channel)
			result, e = p.Advance(context.Background(), channel, r, ce.Progress{Stage: "reconcile", CreateAttempted: true})
			if e != nil || result.Progress.IDs.ProductID != s.IDs.ProductID {
				t.Fatalf("recovery failed: %v", e)
			}
		})
	}
}

type rejectCatalogNetwork struct{}

func (rejectCatalogNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, errors.New("test lost response")
}
func TestCatalogLostCreateResponseNeverResends(t *testing.T) {
	for _, ch := range []string{"ozon", "wb"} {
		p := NewMockCatalog()
		p.Executor.client = &http.Client{Transport: rejectCatalogNetwork{}}
		result, e := p.Advance(context.Background(), ch, cardFixture(ch), ce.Progress{Stage: "create"})
		if e != nil || result.Progress.Stage != "reconcile" || !result.Progress.CreateAttempted {
			t.Fatalf("unsafe recovery: %+v %v", result, e)
		}
	}
}
func TestCatalogOzonHTTP200WithAttributeErrorsIsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"items": []any{map[string]any{"offer_id": "fixture-13", "product_id": 123, "status": "imported", "errors": []any{map[string]any{"code": "required", "message": "Нет страны", "level": "error"}}}}}})
	}))
	defer server.Close()
	p := NewMockCatalog()
	p.Executor.ozonBase = server.URL
	p.Executor.client = server.Client()
	_, e := p.Advance(context.Background(), "ozon", cardFixture("ozon"), ce.Progress{Stage: "ozon-poll", TaskID: 123})
	if e == nil || !strings.Contains(e.Error(), "Нет страны") {
		t.Fatalf("API error was treated as success: %v", e)
	}
}
func TestCatalogMockTransportCannotReachSeller(t *testing.T) {
	p := NewMockCatalog()
	p.Executor.ozonBase = defaultOzonBase
	if _, e := p.Executor.LookupEditorCard(context.Background(), "ozon", "fixture"); e == nil {
		t.Fatal("mock allowed external endpoint")
	}
}

func TestCatalogWBCurrentBatchErrorsAreNotIgnored(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"items":[{"errors":{"fixture-13":["Неверный ТН ВЭД"]}}],"cursor":{"next":false}},"error":false}`))
	}))
	defer server.Close()
	p := NewMockCatalog()
	p.Executor.wbContentBase = server.URL
	p.Executor.client = server.Client()
	if e := p.wbCardErrors(context.Background(), "fixture-13"); e == nil || !strings.Contains(e.Error(), "Неверный ТН ВЭД") {
		t.Fatalf("batch errors ignored: %v", e)
	}
}
func TestCatalogDefiniteRejectionDoesNotBecomeEndlessReconciliation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"message":"Неверный атрибут"}`))
	}))
	defer server.Close()
	p := NewMockCatalog()
	p.Executor.ozonBase = server.URL
	p.Executor.client = server.Client()
	result, e := p.Advance(context.Background(), "ozon", cardFixture("ozon"), ce.Progress{Stage: "create", CreateAttempted: true})
	if e == nil || result.Progress.CreateAttempted || result.Progress.Stage != "check" {
		t.Fatalf("definite failure cannot be corrected: %+v %v", result, e)
	}
}

func TestEditorReadsExistingCardWithoutCreatingOrChangingIt(t *testing.T) {
	for _, channel := range []string{"wb", "ozon"} {
		t.Run(channel, func(t *testing.T) {
			p := NewMockCatalog()
			request := cardFixture(channel)
			progress := ce.Progress{}
			for i := 0; i < 14; i++ {
				result, err := p.Advance(context.Background(), channel, request, progress)
				if err != nil {
					t.Fatal(err)
				}
				progress = result.Progress
				if result.Status == "created" {
					break
				}
			}
			transport := p.Executor.client.Transport.(*catalogMockTransport)
			serial := transport.serial
			card, err := p.Executor.ReadEditorCard(context.Background(), channel, request.Marketplace.Article)
			if err != nil {
				t.Fatal(err)
			}
			if card.ExternalIDs.ProductID != progress.IDs.ProductID || card.Card.Name != "Название площадки" || card.Card.Marketplace.Article != request.Marketplace.Article || len(card.Card.Channel.Attributes) == 0 || card.Card.Channel.Category.ID == "" {
				t.Fatalf("existing card fields were not loaded: %+v", card)
			}
			if transport.serial != serial {
				t.Fatal("reading a card created another product")
			}
			if channel == "ozon" && (card.Card.Marketplace.BrandID != 900101 || card.Card.Marketplace.Model != "Нолина") {
				t.Fatal("dictionary or model lost")
			}
		})
	}
}

func TestExistingRemoteEmptyDescriptionDoesNotRestoreOldSiteContent(t *testing.T) {
	r := cardFixture("wb")
	r.Action = "updating"
	r.Marketplace.ContentFromMarketplace = true
	r.Marketplace.Description = ""
	r.Description = "Описание старой карточки сайта"
	body, err := BuildWBCard(r, true)
	if err != nil {
		t.Fatal(err)
	}
	variant := body[0]["variants"].([]any)[0].(map[string]any)
	if variant["description"] != "" {
		t.Fatal("old site description was restored", variant["description"])
	}
}
