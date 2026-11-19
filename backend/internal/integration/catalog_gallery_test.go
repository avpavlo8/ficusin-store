package integration

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"slices"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

func TestCatalogGalleryDeltaPreservesVendorPhotosAndMatchesRehostedFiles(t *testing.T) {
	ctx := context.Background()
	files := map[string][]byte{}
	for i, name := range []string{"remove-a", "remove-b", "remove-c", "keep-site", "manual"} {
		for _, size := range []int{80, 120} {
			img := image.NewRGBA(image.Rect(0, 0, size, size))
			for y := range size {
				for x := range size {
					img.SetRGBA(x, y, color.RGBA{
						R: byte(30 + i*45 + x*16/size),
						G: byte(30 + y*95/size),
						B: byte(120 + x*28/size),
						A: 255,
					})
				}
			}
			buf := new(bytes.Buffer)
			if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: size/2 + 25}); err != nil {
				t.Fatal(err)
			}
			files[fmt.Sprintf("/%s-%d.jpg", name, size)] = buf.Bytes()
		}
	}
	client := &http.Client{Transport: flowwowRoundTrip(func(r *http.Request) (*http.Response, error) {
		body, ok := files[r.URL.Path]
		if !ok {
			return nil, fmt.Errorf("unexpected image %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	})}
	site := "https://shop.example.com/"
	cdn := "https://seller.example.com/"
	prior := []string{site + "remove-a-120.jpg", site + "remove-b-120.jpg", site + "remove-c-120.jpg", site + "keep-site-120.jpg"}
	before := []string{cdn + "manual-80.jpg", cdn + "remove-a-80.jpg", cdn + "keep-site-80.jpg", cdn + "remove-b-80.jpg", cdn + "remove-c-80.jpg"}
	added := []string{site + "new-one.jpg", site + "new-two.jpg", site + "new-three.jpg", site + "new-four.jpg"}
	after := append([]string{prior[3]}, added...)
	c, err := catalogGalleryDelta(ctx, client, before, &ce.SitePhotoBaseline{Before: prior}, after)
	want := append([]string{before[0], before[2]}, added...)
	if err != nil || !slices.Equal(c.apply(before), want) || !slices.Equal(c.Removed, []int{1, 3, 4}) {
		t.Fatalf("unexpected change: %+v, %v", c, err)
	}
	t.Run("ambiguous legacy matches are not deleted", func(t *testing.T) {
		ambiguous := append(slices.Clone(before), before[1])
		if _, err := catalogGalleryDelta(ctx, client, ambiguous, &ce.SitePhotoBaseline{Before: prior}, after); err == nil {
			t.Fatal("removal accepted two matching photos")
		}
	})
	t.Run("unmodified gallery and order do not upload photos", func(t *testing.T) {
		reordered := []string{prior[3], prior[0], prior[2], prior[1]}
		c, err = catalogGalleryDelta(ctx, client, before, &ce.SitePhotoBaseline{Before: prior}, reordered)
		if err != nil || !slices.Equal(c.apply(before), before) {
			t.Fatalf("non-photo edit changed gallery: %+v, %v", c, err)
		}
	})
}

func TestCatalogExistingCardsApplyOnlySitePhotoChanges(t *testing.T) {
	ctx := context.Background()
	site := "https://shop.example.com/"
	before := []string{site + "one.jpg", site + "two.jpg", site + "three.jpg"}
	after := []string{site + "new-one.jpg", site + "new-two.jpg", site + "new-three.jpg", site + "new-four.jpg"}
	manual := "https://seller.example.com/manual.jpg"
	for _, channel := range []string{"wb", "ozon", "flowwow"} {
		t.Run(channel, func(t *testing.T) {
			req := cardFixture(channel)
			advance := func(s ce.Progress, advance func(ce.Progress) (ce.Result, error)) ce.Progress {
				t.Helper()
				for i := 0; i < 16; i++ {
					out, err := advance(s)
					if err != nil {
						t.Fatal(err)
					}
					if out.Status == "created" || out.Status == "synced" {
						return out.Progress
					}
					if out.Status != "queued" {
						t.Fatalf("unexpected publication result: %+v", out)
					}
					s = out.Progress
				}
				t.Fatal("publication never finished")
				return ce.Progress{}
			}
			s := ce.Progress{}
			if channel == "flowwow" {
				c := NewMockFlowwowClient()
				req = flowwowFixtureRequest()
				run := func(s ce.Progress) (ce.Result, error) { return c.Advance(ctx, channel, req, s) }
				req.Images = before
				advance(s, run)
				m := c.client.Transport.(*flowwowMockTransport)
				card := m.cards[req.SKU]
				card.Images = append([]string{manual}, card.Images...)
				m.cards[req.SKU] = card
				req.Images, req.Action = after, "update"
				s.SitePhotos = &ce.SitePhotoBaseline{Before: before}
				advance(s, run)
				if !slices.Equal(m.cards[req.SKU].Images, append([]string{manual}, after...)) {
					t.Fatalf("Flowwow lost seller's photo: %v", m.cards[req.SKU].Images)
				}
				return
			}

			p := NewMockCatalog()
			req.Images = before
			run := func(s ce.Progress) (ce.Result, error) { return p.Advance(ctx, channel, req, s) }
			published := advance(s, run)
			m := p.Executor.client.Transport.(*catalogMockTransport)
			key := channel + ":" + req.SellerArticle(channel)
			card := m.cards[key]
			card.Images = append([]string{manual}, card.Images...)
			m.cards[key] = card
			req.Images, req.Action = after, "update"
			req.ExistingID = published.IDs.OfferID
			if channel == "wb" {
				req.ExistingID = published.IDs.ProductID
			}
			s.SitePhotos = &ce.SitePhotoBaseline{Before: before}
			advance(s, run)
			if !slices.Equal(m.cards[key].Images, append([]string{manual}, after...)) {
				t.Fatalf("%s lost seller's photo: %v", channel, m.cards[key].Images)
			}
		})
	}
}
