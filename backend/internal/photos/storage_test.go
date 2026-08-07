package photos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPutSignsAndSendsBody(t *testing.T) {
	var got *http.Request
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		buffer := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buffer)
		body = buffer
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	storage := NewStorage(server.URL, "ru-1", "ficusin-photos", "AKIAТЕСТ", "секрет")
	if err := storage.Put(context.Background(), "products/monstera-600.jpg", []byte("картинка"), "image/jpeg"); err != nil {
		t.Fatalf("отправка не удалась: %v", err)
	}

	if got.URL.Path != "/ficusin-photos/products/monstera-600.jpg" {
		t.Errorf("файл ушёл не туда: %s", got.URL.Path)
	}
	if string(body) != "картинка" {
		t.Errorf("тело изменилось по дороге: %q", body)
	}
	auth := got.Header.Get("Authorization")
	for _, want := range []string{
		"AWS4-HMAC-SHA256",
		"Credential=AKIAТЕСТ/",
		"/ru-1/s3/aws4_request",
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date",
		"Signature=",
	} {
		if !strings.Contains(auth, want) {
			t.Errorf("в подписи нет %q: %s", want, auth)
		}
	}
	if got.Header.Get("X-Amz-Content-Sha256") == "" || got.Header.Get("X-Amz-Date") == "" {
		t.Error("не хватает заголовков подписи")
	}
	if got.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("тип файла потерялся: %s", got.Header.Get("Content-Type"))
	}
}

// Ошибку хранилища нужно донести целиком: «отказало» без причины сделает
// разбор поломки гаданием.
func TestPutReportsRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code></Error>"))
	}))
	defer server.Close()

	storage := NewStorage(server.URL, "ru-1", "bucket", "key", "secret")
	err := storage.Put(context.Background(), "a.jpg", []byte("x"), "image/jpeg")
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("ожидали причину отказа, получили: %v", err)
	}
}

func TestUnconfiguredStorageStaysQuiet(t *testing.T) {
	storage := NewStorage("", "", "", "", "")
	if storage.Configured() {
		t.Fatal("пустая настройка считается рабочей")
	}
	if err := storage.Put(context.Background(), "a.jpg", []byte("x"), ""); err == nil {
		t.Fatal("ожидали отказ без настройки")
	}
}

func TestPublicURL(t *testing.T) {
	storage := NewStorage("https://s3.twcstorage.ru/", "ru-1", "ficusin-photos", "k", "s")
	want := "https://s3.twcstorage.ru/ficusin-photos/products/a.jpg"
	if got := storage.PublicURL("/products/a.jpg"); got != want {
		t.Fatalf("ожидали %s, получили %s", want, got)
	}
}
