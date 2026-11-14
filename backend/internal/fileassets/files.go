// Package fileassets stores private file bytes outside the relational database.
// Access control belongs to each existing API endpoint, never to a public URL.
package fileassets

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Storage interface {
	Configured() bool
	Put(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

var ErrUnavailable = errors.New("приватное хранилище файлов не настроено")

// Save verifies persisted bytes before the caller commits its database reference.
func Save(ctx context.Context, storage Storage, prefix, contentType string, data []byte) (string, error) {
	if storage == nil || !storage.Configured() {
		return "", ErrUnavailable
	}
	if prefix == "" || strings.Contains(prefix, "..") || strings.HasPrefix(prefix, "/") || len(data) == 0 || len(data) > 64<<20 {
		return "", errors.New("некорректный файл")
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	key := strings.TrimSuffix(prefix, "/") + "/" + hex.EncodeToString(token)
	if err := storage.Put(ctx, key, data, contentType); err != nil {
		Discard(storage, key)
		return "", fmt.Errorf("загрузить файл: %w", err)
	}
	saved, err := storage.Get(ctx, key)
	if err != nil || sha256.Sum256(saved) != sha256.Sum256(data) {
		Discard(storage, key)
		if err != nil {
			return "", fmt.Errorf("проверить загруженный файл: %w", err)
		}
		return "", errors.New("контрольная сумма загруженного файла не совпадает")
	}
	return key, nil
}

// Read uses only the verified private object reference. Retired database bytes
// are archived outside the runtime path. Empty keys represent no attachment.
func Read(ctx context.Context, storage Storage, key string) ([]byte, error) {
	if key == "" {
		return nil, nil
	}
	if storage == nil || !storage.Configured() {
		return nil, ErrUnavailable
	}
	return storage.Get(ctx, key)
}

// Discard only cleans an uncommitted newly uploaded object. Migration never
// deletes source bytes or existing objects automatically.
func Discard(storage Storage, key string) {
	if storage == nil || !storage.Configured() || key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = storage.Delete(ctx, key)
}
