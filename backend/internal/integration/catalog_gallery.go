package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"net/http"
	"slices"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"golang.org/x/image/draw"
)

type catalogGalleryChanges struct {
	Removed []int
	Added   []string
}

func (c catalogGalleryChanges) apply(current []string) []string {
	out := make([]string, 0, len(current)-len(c.Removed)+len(c.Added))
	for i, photo := range current {
		if !slices.Contains(c.Removed, i) {
			out = append(out, photo)
		}
	}
	return append(out, c.Added...)
}

func catalogSitePhotoDelta(baseline *ce.SitePhotoBaseline, after []string) (removed, added []string) {
	var before, unmanaged []string
	if baseline != nil {
		before, unmanaged = baseline.Before, baseline.Unmanaged
	}
	for _, source := range before {
		if !slices.Contains(after, source) && !slices.Contains(unmanaged, source) {
			removed = append(removed, source)
		}
	}
	for _, source := range after {
		if !slices.Contains(before, source) {
			added = append(added, source)
		}
	}
	return
}

func catalogGalleryDelta(ctx context.Context, client *http.Client, current []string, baseline *ce.SitePhotoBaseline, after []string) (catalogGalleryChanges, error) {
	return newCatalogPhotoMatcher(client).delta(ctx, current, baseline, after, nil)
}

func (matcher *catalogPhotoMatcher) delta(ctx context.Context, current []string, baseline *ce.SitePhotoBaseline, after, previouslySent []string) (catalogGalleryChanges, error) {
	removed, added := catalogSitePhotoDelta(baseline, after)
	changes := catalogGalleryChanges{}
	for _, source := range removed {
		matches, err := matcher.matches(ctx, source, current)
		if err != nil {
			return changes, err
		}
		if len(matches) > 1 || len(matches) == 1 && slices.Contains(changes.Removed, matches[0]) {
			return changes, errors.New("Найдено несколько совпадений фотографий. Состав галереи на площадке требует проверки")
		}
		if len(matches) == 1 {
			changes.Removed = append(changes.Removed, matches[0])
		}
	}
	for _, source := range added {
		if i := slices.Index(current, source); i >= 0 && !slices.Contains(changes.Removed, i) {
			continue
		}
		if slices.Contains(previouslySent, source) {
			matches, err := matcher.matches(ctx, source, current)
			if err != nil {
				return changes, err
			}
			if slices.ContainsFunc(matches, func(i int) bool { return !slices.Contains(changes.Removed, i) }) {
				continue
			}
		}
		changes.Added = append(changes.Added, source)
	}
	return changes, nil
}

const catalogPhotoSampleSize = 24

type catalogPhotoSample struct {
	width, height int
	pixels        *image.RGBA
}
type catalogPhotoMatcher struct {
	client      *http.Client
	cache       map[string]catalogPhotoSample
	wildberries bool
}

func newCatalogPhotoMatcher(client *http.Client) *catalogPhotoMatcher {
	if client == nil {
		client = newSabyEditorImageClient()
	}
	return &catalogPhotoMatcher{client: client, cache: map[string]catalogPhotoSample{}}
}

func (m *catalogPhotoMatcher) matches(ctx context.Context, source string, candidates []string) ([]int, error) {
	var exact []int
	for i, candidate := range candidates {
		if candidate == source {
			exact = append(exact, i)
		}
	}
	if len(exact) > 0 || len(candidates) == 0 {
		return exact, nil
	}
	want, err := m.sample(ctx, source)
	if err != nil {
		return nil, err
	}
	var similar []int
	for i, candidate := range candidates {
		got, err := m.sample(ctx, candidate)
		if err != nil {
			return nil, err
		}
		if want.equivalent(got, m.wildberries) {
			similar = append(similar, i)
		}
	}
	return similar, nil
}

func (m *catalogPhotoMatcher) sample(ctx context.Context, source string) (catalogPhotoSample, error) {
	if cached, ok := m.cache[source]; ok {
		return cached, nil
	}
	if len(m.cache) >= 100 {
		return catalogPhotoSample{}, errors.New("Слишком много фотографий для безопасной сверки галереи")
	}
	raw, _, err := downloadSabyEditorImage(ctx, m.client, source)
	if err != nil {
		return catalogPhotoSample{}, fmt.Errorf("Не удалось сверить фотографии площадки: %w", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 25_000_000 {
		return catalogPhotoSample{}, errors.New("Не удалось проверить размер фотографии площадки")
	}
	img, err := photos.Decode(raw)
	if err != nil {
		return catalogPhotoSample{}, errors.New("Не удалось прочитать фотографию при сверке галереи площадки")
	}
	b := img.Bounds()
	if m.wildberries {
		b = wildberriesPhotoContentBounds(img)
	}
	sample := catalogPhotoSample{width: b.Dx(), height: b.Dy(), pixels: image.NewRGBA(image.Rect(0, 0, catalogPhotoSampleSize, catalogPhotoSampleSize))}
	draw.BiLinear.Scale(sample.pixels, sample.pixels.Bounds(), img, b, draw.Src, nil)
	m.cache[source] = sample
	return sample, nil
}

func (a catalogPhotoSample) equivalent(b catalogPhotoSample, wildberries bool) bool {
	// Accept normal resize and recompression, with conservative tolerances
	// for colour and aspect ratio. Cropped/edited photos are distinct.
	crossA, crossB := int64(a.width)*int64(b.height), int64(b.width)*int64(a.height)
	if (max(crossA, crossB)-min(crossA, crossB))*100 > 2*max(crossA, crossB) {
		return false
	}
	var total, outliers int
	for i, x := range a.pixels.Pix {
		y := b.pixels.Pix[i]
		diff := max(int(x), int(y)) - min(int(x), int(y))
		total += diff
		if diff > 12 {
			outliers++
		}
	}
	n := len(a.pixels.Pix)
	if wildberries {
		// WB recompresses to WebP and makes small shifts to the same image.
		// A size/aspect match and a unique pixel match are still required.
		return total <= 5*n && outliers*100 <= 9*n
	}
	return total <= 3*n && outliers*100 <= n
}

func catalogPhotoWriteConfirmed(ctx context.Context, client *http.Client, actual []string, progress ce.Progress) (bool, error) {
	return newCatalogPhotoMatcher(client).confirms(ctx, actual, progress)
}

func (matcher *catalogPhotoMatcher) confirms(ctx context.Context, actual []string, progress ce.Progress) (bool, error) {
	if progress.PhotoCount == nil {
		return true, nil
	}
	if len(actual) != *progress.PhotoCount {
		return false, nil
	}
	for _, source := range progress.PhotoWrite {
		matches, err := matcher.matches(ctx, source, actual)
		if err != nil || len(matches) == 0 {
			return false, err
		}
	}
	return true, nil
}

func catalogPhotoURLs(raw any) ([]string, bool) {
	var urls []string
	switch value := raw.(type) {
	case string:
		if value != "" {
			urls = append(urls, value)
		}
	case []any:
		for _, item := range value {
			switch photo := item.(type) {
			case string:
				if photo != "" {
					urls = append(urls, photo)
					continue
				}
			case map[string]any:
				for _, key := range []string{"big", "url", "file_name"} {
					if url, ok := photo[key].(string); ok && url != "" {
						urls = append(urls, url)
						break
					}
				}
			}
		}
		return urls, len(urls) == len(value)
	case nil:
	default:
		return nil, false
	}
	return urls, true
}
