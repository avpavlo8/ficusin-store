// Пакет photos переносит фотографии товаров в наше хранилище и уменьшает их
// до разумного размера.
//
// Сейчас снимки лежат на диске СБИС в оригинале — до трёх тысяч пикселей по
// стороне. Покупателю с телефона это стоит долгого ожидания, а магазину —
// зависимости: закроется доступ, и витрина останется без единой картинки.
package photos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Storage кладёт файлы в хранилище, совместимое с S3.
//
// Подпись запросов написана здесь целиком, без сторонней библиотеки: проект
// собирается без сети до реестра модулей, и добавить зависимость нельзя. Сам
// алгоритм AWS SigV4 — это четыре HMAC подряд, и он умещается в сотню строк.
type Storage struct {
	endpoint  string
	region    string
	bucket    string
	accessKey string
	secretKey string
	client    *http.Client
}

func NewStorage(endpoint, region, bucket, accessKey, secretKey string) *Storage {
	return &Storage{
		endpoint:  strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		region:    strings.TrimSpace(region),
		bucket:    strings.TrimSpace(bucket),
		accessKey: strings.TrimSpace(accessKey),
		secretKey: strings.TrimSpace(secretKey),
		client:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Configured отвечает на вопрос «настроено ли хранилище». Пустой ключ — это
// не ошибка, а выключенная возможность: магазин работает и на чужих ссылках.
func (storage *Storage) Configured() bool {
	return storage != nil && storage.endpoint != "" && storage.bucket != "" &&
		storage.accessKey != "" && storage.secretKey != ""
}

// PublicURL — адрес, по которому файл увидит покупатель. Путь строится в
// «path-style»: он работает у любого хранилища, тогда как имя бакета в домене
// поддерживают не все.
func (storage *Storage) PublicURL(key string) string {
	return storage.endpoint + "/" + storage.bucket + "/" + strings.TrimPrefix(key, "/")
}

func (storage *Storage) Put(ctx context.Context, key string, body []byte, contentType string) error {
	if !storage.Configured() {
		return fmt.Errorf("хранилище не настроено")
	}
	target := storage.PublicURL(key)
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("неверный адрес файла: %w", err)
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	payloadHash := hashHex(body)

	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	request.Header.Set("X-Amz-Date", amzDate)

	// Подписываем только три заголовка. Остальные хранилище примет как есть:
	// в подпись обязаны входить лишь те, что перечислены в SignedHeaders.
	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	values := map[string]string{
		"host":                 parsed.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	sort.Strings(signed)
	canonicalHeaders := strings.Builder{}
	for _, name := range signed {
		canonicalHeaders.WriteString(name + ":" + values[name] + "\n")
	}

	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		escapePath(parsed.Path),
		"",
		canonicalHeaders.String(),
		strings.Join(signed, ";"),
		payloadHash,
	}, "\n")

	scope := shortDate + "/" + storage.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hashHex([]byte(canonicalRequest)),
	}, "\n")

	key256 := sign([]byte("AWS4"+storage.secretKey), shortDate)
	key256 = sign(key256, storage.region)
	key256 = sign(key256, "s3")
	key256 = sign(key256, "aws4_request")
	signature := hex.EncodeToString(sign(key256, stringToSign))

	request.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		storage.accessKey, scope, strings.Join(signed, ";"), signature,
	))

	response, err := storage.client.Do(request)
	if err != nil {
		return fmt.Errorf("хранилище недоступно: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		details, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("хранилище отказало (%d): %s", response.StatusCode, strings.TrimSpace(string(details)))
	}
	return nil
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sign(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

// Косая черта разделяет части пути и экранированию не подлежит, всё
// остальное экранируется по правилам подписи.
func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		parts[index] = strings.ReplaceAll(url.QueryEscape(part), "+", "%20")
	}
	return strings.Join(parts, "/")
}
