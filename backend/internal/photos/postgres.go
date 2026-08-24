package photos

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore хранит перечень перенесённых снимков.
//
// Ключ — ссылка поставщика, а не строка product_media: обмен с СБИС удаляет
// и заново создаёт эти строки при каждой синхронизации, и привязка к ним
// заставляла бы качать одно и то же по кругу.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Pending возвращает ссылки, которые ещё не перенесены. Сюда же попадают
// прежние неудачи — но не раньше чем через шесть часов и не больше пяти раз:
// битая ссылка не должна занимать очередь вечно.
func (store *PostgresStore) Pending(ctx context.Context, limit int) ([]string, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT source.object_key
		FROM (SELECT DISTINCT object_key FROM product_media) source
		LEFT JOIN media_mirror mirror ON mirror.source_url = source.object_key
		WHERE source.object_key LIKE 'https://%'
			AND (
				mirror.source_url IS NULL
				OR (
					mirror.card_url IS NULL
					AND mirror.attempts < 5
					AND mirror.checked_at < CURRENT_TIMESTAMP - INTERVAL '6 hours'
				)
				OR (
					mirror.mirrored_at IS NULL
					AND mirror.card_url IS NOT NULL
					AND mirror.attempts < 5
					AND mirror.checked_at < CURRENT_TIMESTAMP - INTERVAL '6 hours'
				)
			)
		-- Собственные тяжёлые обложки уже лежат рядом с приложением и не
		-- нагружают поставщика. Обрабатываем их раньше внешней очереди, чтобы
		-- новая карточка не ждала следующего пяти-минутного прохода.
		ORDER BY CASE
			WHEN source.object_key LIKE 'https://s3.twcstorage.ru/%' THEN 0
			ELSE 1
		END, source.object_key
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("очередь фотографий: %w", err)
	}
	defer rows.Close()
	sources := make([]string, 0, limit)
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, fmt.Errorf("чтение очереди фотографий: %w", err)
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (store *PostgresStore) Save(ctx context.Context, source, card, large string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO media_mirror (
			source_url, card_url, large_url, attempts, failure, mirrored_at, checked_at
		)
		VALUES ($1, $2, $3, 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (source_url) DO UPDATE SET
			card_url = EXCLUDED.card_url,
			large_url = EXCLUDED.large_url,
			failure = '',
			mirrored_at = CURRENT_TIMESTAMP,
			checked_at = CURRENT_TIMESTAMP
	`, source, card, large)
	if err != nil {
		return fmt.Errorf("запись перенесённого снимка: %w", err)
	}
	return nil
}

func (store *PostgresStore) Fail(ctx context.Context, source, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO media_mirror (source_url, attempts, failure, checked_at)
		VALUES ($1, 1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (source_url) DO UPDATE SET
			attempts = media_mirror.attempts + 1,
			failure = EXCLUDED.failure,
			checked_at = CURRENT_TIMESTAMP
	`, source, reason)
	if err != nil {
		return fmt.Errorf("запись неудачи снимка: %w", err)
	}
	return nil
}
