package photos

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateSourceURLAllowsOnlySupplierAndOwnStorage(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://disk.sbis.ru/photo.webp",
		"https://s3.twcstorage.ru/ficusin-photos/products/5/cover.webp",
	} {
		parsed, err := url.Parse(raw)
		if err != nil || validateSourceURL(parsed) != nil {
			t.Fatalf("safe media URL rejected: %s", raw)
		}
	}
	for _, raw := range []string{
		"http://s3.twcstorage.ru/file.webp",
		"https://s3.twcstorage.ru.evil.example/file.webp",
		"https://127.0.0.1/file.webp",
	} {
		parsed, _ := url.Parse(raw)
		if validateSourceURL(parsed) == nil {
			t.Fatalf("unsafe media URL accepted: %s", raw)
		}
	}
}

type memoryStore struct {
	mutex   sync.Mutex
	pending []string
	saved   map[string][2]string
	failed  map[string]string
}

func newMemoryStore(pending ...string) *memoryStore {
	return &memoryStore{
		pending: pending,
		saved:   map[string][2]string{},
		failed:  map[string]string{},
	}
}

func (store *memoryStore) Pending(_ context.Context, limit int) ([]string, error) {
	if limit < len(store.pending) {
		return store.pending[:limit], nil
	}
	return store.pending, nil
}

func (store *memoryStore) Save(_ context.Context, source, card, large string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.saved[source] = [2]string{card, large}
	return nil
}

func (store *memoryStore) Fail(_ context.Context, source, reason string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.failed[source] = reason
	return nil
}

func photograph(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x % 255), G: 120, B: 40, A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, picture, nil); err != nil {
		t.Fatalf("не подготовить снимок: %v", err)
	}
	return out.Bytes()
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestMirrorDefaultBatchDrainsStoreCatalogue(t *testing.T) {
	worker := NewMirror(newMemoryStore(), NewStorage("https://s3.example", "ru-1", "photos", "k", "s"), quiet())
	if worker.Batch < 250 {
		t.Fatalf("photo migration batch = %d, want at least one full catalogue", worker.Batch)
	}
	if worker.Pause < 250*time.Millisecond {
		t.Fatalf("photo migration pause = %s, supplier throttling was removed", worker.Pause)
	}
}

func TestPassMovesPhotos(t *testing.T) {
	original := photograph(t, 1600, 1200)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(original)
	}))
	defer source.Close()

	var mutex sync.Mutex
	uploaded := map[string]int{}
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mutex.Lock()
		uploaded[r.URL.Path] = len(body)
		mutex.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer bucket.Close()

	// httptest выдаёт http, а перенос ходит только по https, поэтому
	// подменяем адрес источника на заведомо https и правим транспорт.
	address := "https://disk.sbis.ru/photo.jpg"
	storage := NewStorage(bucket.URL, "ru-1", "photos", "key", "secret")
	store := newMemoryStore(address)
	worker := NewMirror(store, storage, quiet())
	worker.Pause = 0
	worker.client = source.Client()
	worker.client.Transport = redirect{to: source.URL}

	moved, err := worker.Pass(context.Background())
	if err != nil {
		t.Fatalf("проход не удался: %v", err)
	}
	if moved != 1 {
		t.Fatalf("ожидали один перенесённый снимок, получили %d", moved)
	}
	if len(uploaded) != len(Sizes) {
		t.Fatalf("ожидали %d размера в хранилище, получили %d", len(Sizes), len(uploaded))
	}
	links, ok := store.saved[address]
	if !ok {
		t.Fatal("перенос не записан")
	}
	if !strings.Contains(links[0], "-card.jpg") || !strings.Contains(links[1], "-large.jpg") {
		t.Fatalf("ссылки перепутаны местами: %v", links)
	}
	// Уменьшенный снимок обязан быть легче исходного, иначе перенос
	// бессмысленен.
	for path, size := range uploaded {
		if size >= len(original) {
			t.Errorf("%s не легче исходника: %d против %d", path, size, len(original))
		}
	}
}

// Битая ссылка не должна ронять проход: у поставщика их хватает.
func TestPassRecordsFailure(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer source.Close()

	address := "https://disk.sbis.ru/missing.jpg"
	store := newMemoryStore(address)
	worker := NewMirror(store, NewStorage("https://s3.example", "ru-1", "photos", "k", "s"), quiet())
	worker.Pause = 0
	worker.client = source.Client()
	worker.client.Transport = redirect{to: source.URL}

	moved, err := worker.Pass(context.Background())
	if err != nil {
		t.Fatalf("проход упал целиком: %v", err)
	}
	if moved != 0 {
		t.Fatalf("ожидали ноль переносов, получили %d", moved)
	}
	if reason := store.failed[address]; !strings.Contains(reason, "404") {
		t.Fatalf("причина неудачи потеряна: %q", reason)
	}
	if len(store.saved) != 0 {
		t.Fatal("неудачу записали как успех")
	}
}

// СБИС отдаёт webp, а уменьшить его нечем. Такой снимок обязан всё равно
// оказаться в нашем хранилище — иначе витрина останется на чужих ссылках.
func TestUnreadableFormatIsCopiedAsIs(t *testing.T) {
	// Заголовок RIFF….WEBP — этого хватает, чтобы формат был опознан, и не
	// хватает, чтобы картинка разобралась.
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(webp)
	}))
	defer source.Close()

	var mutex sync.Mutex
	uploaded := map[string]string{}
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		uploaded[r.URL.Path] = r.Header.Get("Content-Type")
		mutex.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer bucket.Close()

	address := "https://disk.sbis.ru/photo.webp"
	store := newMemoryStore(address)
	worker := NewMirror(store, NewStorage(bucket.URL, "ru-1", "photos", "k", "s"), quiet())
	worker.Pause = 0
	worker.client = source.Client()
	worker.client.Transport = redirect{to: source.URL}

	moved, err := worker.Pass(context.Background())
	if err != nil || moved != 1 {
		t.Fatalf("снимок не перенесён: moved=%d err=%v", moved, err)
	}
	if len(uploaded) != 1 {
		t.Fatalf("ожидали одну копию, получили %d", len(uploaded))
	}
	for path, kind := range uploaded {
		if !strings.HasSuffix(path, "-original.webp") {
			t.Errorf("неожиданное имя файла: %s", path)
		}
		if !strings.HasPrefix(kind, "image/webp") {
			t.Errorf("тип файла потерялся: %s", kind)
		}
	}
	links := store.saved[address]
	if links[0] == "" || links[0] != links[1] {
		t.Errorf("обе ссылки должны вести на одну копию: %v", links)
	}
}

func TestDownloadRefusesPlainHTTP(t *testing.T) {
	worker := NewMirror(newMemoryStore(), NewStorage("https://s3.example", "ru-1", "b", "k", "s"), quiet())
	if _, _, err := worker.download(context.Background(), "http://sbis.example/a.jpg"); err == nil {
		t.Fatal("простой http приняли")
	}
}

func TestDownloadRefusesNonSabyAndUnsafeRedirectTargets(t *testing.T) {
	worker := NewMirror(newMemoryStore(), NewStorage("https://s3.example", "ru-1", "b", "k", "s"), quiet())
	for _, source := range []string{"https://127.0.0.1/a.jpg", "https://metadata.google.internal/a", "https://evil.example/a.jpg", "https://disk.sbis.ru:8443/a.jpg"} {
		if _, _, err := worker.download(context.Background(), source); err == nil { t.Fatalf("небезопасный URL принят: %s", source) }
	}
	redirectTarget, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	if err := worker.client.CheckRedirect(redirectTarget, nil); err == nil { t.Fatal("небезопасный redirect принят") }
}

// Имя файла выводится из ссылки: повторный перенос обязан лечь на то же
// место, а разные ссылки — не столкнуться.
func TestKeyIsStable(t *testing.T) {
	first := Key("https://sbis.example/a.jpg", SizeCard)
	if first != Key("https://sbis.example/a.jpg", SizeCard) {
		t.Fatal("имя файла пляшет от вызова к вызову")
	}
	if first == Key("https://sbis.example/b.jpg", SizeCard) {
		t.Fatal("разные снимки получили одно имя")
	}
	if first == Key("https://sbis.example/a.jpg", SizeLarge) {
		t.Fatal("размеры получили одно имя")
	}
	if !strings.HasPrefix(first, "products/") || !strings.HasSuffix(first, "-card.jpg") {
		t.Fatalf("неожиданное имя файла: %s", first)
	}
}

// redirect уводит любой запрос на тестовый сервер, сохраняя видимость
// внешнего https-адреса.
type redirect struct {
	to string
}

func (r redirect) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	target := strings.TrimPrefix(r.to, "http://")
	clone.URL.Scheme = "http"
	clone.URL.Host = target
	clone.Host = target
	return http.DefaultTransport.RoundTrip(clone)
}
