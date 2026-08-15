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
	// Batch — сколько снимков берём за проход. Шестьдесят за пять минут —
	// это один запрос в пять секунд: полторы тысячи снимков разбираются за
	// пару часов, а чужой сервер этого даже не замечает.
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
		// A deployment must drain a normal catalogue in one pass. Downloads
		// stay deliberately sequential and Pause caps pressure on Saby/S3.
		Batch:   250,
		Every:   5 * time.Minute,
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
			// Причина идёт в сам текст сообщения, а не в поле: панель хостинга
			// показывает только текст и поля отбрасывает, а разбираться
			// вслепую в чужой неудаче невозможно.
			mirror.logger.Warn("снимок не перенесён: " + err.Error() + " (" + source + ")")
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

// One переносит один снимок.
func (mirror *Mirror) One(ctx context.Context, source string) error {
	raw, kind, err := mirror.download(ctx, source)
	if err != nil {
		return err
	}
	card, large, err := mirror.copies(ctx, source, raw, kind)
	if err != nil {
		return err
	}
	return mirror.store.Save(ctx, source, card, large)
}

// copies кладёт снимок в хранилище и возвращает ссылки на него.
//
// Уменьшить получается не всякий: СБИС отдаёт webp, а его стандартная
// библиотека не читает — нужна сторонняя, которую в этот проект не добавить.
// Такие снимки копируем как есть. Это не поражение: webp у СБИС весит около
// ста килобайт, то есть главное — снимок становится нашим и не исчезнет
// вместе с чужим сервером — мы получаем и без уменьшения.
func (mirror *Mirror) copies(
	ctx context.Context,
	source string,
	raw []byte,
	kind string,
) (string, string, error) {
	links := make(map[string]string, len(Sizes))
	for _, size := range Sizes {
		ready, prepareErr := Prepare(raw, size.MaxSide)
		if prepareErr != nil {
			if !strings.HasPrefix(kind, "image/") {
				// Пришло не изображение вовсе — вот это уже неудача.
				return "", "", fmt.Errorf("%w (пришло %s, %d байт)", prepareErr, kind, len(raw))
			}
			key := originalKey(source, kind)
			if putErr := mirror.storage.Put(ctx, key, raw, kind); putErr != nil {
				return "", "", putErr
			}
			link := mirror.storage.PublicURL(key)
			return link, link, nil
		}
		key := Key(source, size)
		if putErr := mirror.storage.Put(ctx, key, ready, "image/jpeg"); putErr != nil {
			return "", "", putErr
		}
		links[size.Name] = mirror.storage.PublicURL(key)
	}
	return links[SizeCard.Name], links[SizeLarge.Name], nil
}

func (mirror *Mirror) download(ctx context.Context, source string) ([]byte, string, error) {
	// Только https: адрес приходит из чужой системы, и ходить по нему куда
	// угодно мы не обязаны.
	if !strings.HasPrefix(source, "https://") {
		return nil, "", errors.New("ссылка не по https")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", err
	}
	// Браузеру покупателя СБИС снимок отдаёт, а безымянному запросу — нет.
	// Поэтому представляемся и говорим, откуда пришли: это те же сведения,
	// что шлёт любая страница магазина, и ничего сверх того.
	request.Header.Set("User-Agent", "FicusinBot/1.0 (+https://ficusin.ru)")
	request.Header.Set("Referer", "https://ficusin.ru/")
	request.Header.Set("Accept", "image/jpeg,image/png;q=0.9,*/*;q=0.1")

	response, err := mirror.client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("снимок не скачан: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("снимок отдан с кодом %d", response.StatusCode)
	}
	// Читаем на байт больше потолка: так видно, что файл его превысил, а не
	// ровно в него упёрся.
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("снимок не дочитан: %w", err)
	}
	if len(raw) > maxSourceBytes {
		return nil, "", fmt.Errorf("снимок тяжелее %d МБ", maxSourceBytes>>20)
	}
	if len(raw) == 0 {
		return nil, "", errors.New("пустой ответ")
	}
	// Заголовку верить нельзя, поэтому смотрим на сами байты.
	return raw, http.DetectContentType(raw), nil
}

// Key — имя файла в хранилище. Оно выводится из самой ссылки, поэтому
// повторный перенос кладёт снимок на то же место, а не плодит копии.
func Key(source string, size Size) string {
	return objectKey(source, size.Name, "jpg")
}

// originalKey — имя для снимка, который мы не смогли уменьшить и положили
// как есть.
func originalKey(source, kind string) string {
	return objectKey(source, "original", extension(kind))
}

func objectKey(source, name, extension string) string {
	sum := sha256.Sum256([]byte(source))
	return "products/" + hex.EncodeToString(sum[:])[:16] + "-" + name + "." + extension
}

func extension(kind string) string {
	switch {
	case strings.HasPrefix(kind, "image/webp"):
		return "webp"
	case strings.HasPrefix(kind, "image/avif"):
		return "avif"
	case strings.HasPrefix(kind, "image/png"):
		return "png"
	case strings.HasPrefix(kind, "image/gif"):
		return "gif"
	default:
		return "jpg"
	}
}
