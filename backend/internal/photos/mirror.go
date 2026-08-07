package photos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Store помнит, какие снимки уже перенесены.
type Store interface {
	Pending(ctx context.Context, limit int) ([]string, error)
	Save(ctx context.Context, source, card, large string) error
	Fail(ctx context.Context, source, reason string) error
}

// Mirror переносит фотографии поставщика в наше хранилище.
//
// Работает в фоне и маленькими порциями. Перекачать все снимки разом — это
// на несколько минут занять и свою память, и чужой сервер; порциями по
// два десятка никто не заметит.
type Mirror struct {
	store   Store
	storage *Storage
	logger  *slog.Logger
	client  *http.Client
	// Batch — сколько снимков берём за проход.
	Batch int
	// Every — как часто заглядываем, не появилось ли новых.
	Every time.Duration
	// Pause — задержка между снимками. Мы в гостях у чужого сервера и не
	// имеем права превращать перенос в обстрел.
	Pause time.Duration
}

// maxSourceBytes — потолок на исходный файл. Без него один непомерный
// снимок положит магазин по памяти.
const maxSourceBytes = 25 << 20

func NewMirror(store Store, storage *Storage, logger *slog.Logger) *Mirror {
	return &Mirror{
		store:   store,
		storage: storage,
		logger:  logger,
		client:  &http.Client{Timeout: 60 * time.Second},
		Batch:   20,
		Every:   10 * time.Minute,
		Pause:   500 * time.Millisecond,
	}
}

// Run работает до остановки магазина.
func (mirror *Mirror) Run(ctx context.Context) {
	ticker := time.NewTicker(mirror.Every)
	defer ticker.Stop()
	for {
		moved, err := mirror.Pass(ctx)
		if err != nil && ctx.Err() == nil {
			mirror.logger.Error("очередь фотографий не разобрана", "error", err)
		}
		if moved > 0 {
			mirror.logger.Info("фотографии перенесены в своё хранилище", "count", moved)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Pass переносит одну порцию и возвращает число удавшихся снимков.
func (mirror *Mirror) Pass(ctx context.Context) (int, error) {
	sources, err := mirror.store.Pending(ctx, mirror.Batch)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, source := range sources {
		if ctx.Err() != nil {
			return moved, ctx.Err()
		}
		if err := mirror.One(ctx, source); err != nil {
			// Неудача одного снимка не повод бросать остальные: у поставщика
			// попадаются битые ссылки, и из-за одной очередь встанет навсегда.
			mirror.logger.Warn("снимок не перенесён", "source", source, "error", err)
			if failErr := mirror.store.Fail(ctx, source, err.Error()); failErr != nil {
				return moved, failErr
			}
			continue
		}
		moved++
		if mirror.Pause > 0 {
			select {
			case <-ctx.Done():
				return moved, ctx.Err()
			case <-time.After(mirror.Pause):
			}
		}
	}
	return moved, nil
}

// One переносит один снимок во всех размерах.
func (mirror *Mirror) One(ctx context.Context, source string) error {
	raw, err := mirror.download(ctx, source)
	if err != nil {
		return err
	}
	links := make(map[string]string, len(Sizes))
	for _, size := range Sizes {
		ready, prepareErr := Prepare(raw, size.MaxSide)
		if prepareErr != nil {
			return prepareErr
		}
		key := Key(source, size)
		if putErr := mirror.storage.Put(ctx, key, ready, "image/jpeg"); putErr != nil {
			return putErr
		}
		links[size.Name] = mirror.storage.PublicURL(key)
	}
	return mirror.store.Save(ctx, source, links[SizeCard.Name], links[SizeLarge.Name])
}

func (mirror *Mirror) download(ctx context.Context, source string) ([]byte, error) {
	// Только https и только чужая картинка: адрес приходит из чужой системы,
	// и ходить по нему куда угодно мы не обязаны.
	if !strings.HasPrefix(source, "https://") {
		return nil, errors.New("ссылка не по https")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	response, err := mirror.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("снимок не скачан: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("снимок отдан с кодом %d", response.StatusCode)
	}
	// Читаем на байт больше потолка: так видно, что файл его превысил, а не
	// ровно в него упёрся.
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("снимок не дочитан: %w", err)
	}
	if len(raw) > maxSourceBytes {
		return nil, fmt.Errorf("снимок тяжелее %d МБ", maxSourceBytes>>20)
	}
	if len(raw) == 0 {
		return nil, errors.New("пустой ответ")
	}
	return raw, nil
}

// Key — имя файла в хранилище. Оно выводится из самой ссылки, поэтому
// повторный перенос кладёт снимок на то же место, а не плодит копии.
func Key(source string, size Size) string {
	sum := sha256.Sum256([]byte(source))
	return "products/" + hex.EncodeToString(sum[:])[:16] + "-" + size.Name + ".jpg"
}
