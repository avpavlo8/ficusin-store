package procurement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClaimWBSync acquires one persistent mirror lane. The state lives in
// PostgreSQL, so a deployment or a second application instance cannot start
// another full WB export while the first one is still running.
func (store *PostgresStore) ClaimWBSync(ctx context.Context, resource string, lease time.Duration) (bool, error) {
	if !validWBResource(resource) || lease <= 0 {
		return false, ErrInvalidInput
	}
	var claimed bool
	err := store.pool.QueryRow(ctx, `
		UPDATE procurement_wb_sync_state SET
			status = 'running', last_attempt_at = CURRENT_TIMESTAMP,
			locked_until = CURRENT_TIMESTAMP + make_interval(secs => $2::DOUBLE PRECISION),
			last_error = '', updated_at = CURRENT_TIMESTAMP
		WHERE resource = $1 AND next_attempt_at <= CURRENT_TIMESTAMP
			AND (status <> 'running' OR locked_until IS NULL OR locked_until < CURRENT_TIMESTAMP)
		RETURNING TRUE
	`, resource, lease.Seconds()).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim Wildberries %s synchronization: %w", resource, err)
	}
	return claimed, nil
}

// FinishWBSync releases a mirror lane. A successful lane is due in one hour;
// a 429 uses WB's exact X-RateLimit-Retry value instead of an arbitrary retry
// loop. Other failures are retried after a quiet fifteen-minute window.
func (store *PostgresStore) FinishWBSync(
	ctx context.Context,
	resource string,
	rows int,
	next time.Duration,
	syncErr error,
) error {
	if !validWBResource(resource) || rows < 0 || next <= 0 {
		return ErrInvalidInput
	}
	status, message := "ok", ""
	if syncErr != nil {
		status, message = "error", safeError(syncErr.Error())
	}
	_, err := store.pool.Exec(ctx, `
		UPDATE procurement_wb_sync_state SET status = $2,
			last_success_at = CASE WHEN $2 = 'ok' THEN CURRENT_TIMESTAMP ELSE last_success_at END,
			next_attempt_at = CURRENT_TIMESTAMP + make_interval(secs => $3::DOUBLE PRECISION),
			locked_until = NULL,
			rows_synced = CASE WHEN $2 = 'ok' THEN $4 ELSE rows_synced END,
			last_error = $5, updated_at = CURRENT_TIMESTAMP
		WHERE resource = $1
	`, resource, status, next.Seconds(), rows, message)
	if err != nil {
		return fmt.Errorf("finish Wildberries %s synchronization: %w", resource, err)
	}
	// The integrations card reflects the real mirror, not only a lightweight
	// /ping. Since catalogue runs before sales, the final state of an hourly
	// cycle is the sales result that operators actually care about.
	_, err = store.pool.Exec(ctx, `
		INSERT INTO procurement_integration_health (
			channel, last_checked_at, last_success_at, last_error
		) VALUES ('wb', CURRENT_TIMESTAMP,
			CASE WHEN $1 = 'ok' THEN CURRENT_TIMESTAMP ELSE NULL END, $2)
		ON CONFLICT (channel) DO UPDATE SET
			last_checked_at = CURRENT_TIMESTAMP,
			last_success_at = CASE WHEN $1 = 'ok' THEN CURRENT_TIMESTAMP
				ELSE procurement_integration_health.last_success_at END,
			last_error = $2
	`, status, message)
	if err != nil {
		return fmt.Errorf("record Wildberries mirror health: %w", err)
	}
	return nil
}

func validWBResource(value string) bool {
	return value == "catalog" || value == "sales"
}

// ReserveWBRequest reserves the next request slot in a named WB API bucket
// and returns how long this process must wait before using it. The atomic
// upsert serializes every app instance that shares the seller token.
func (store *PostgresStore) ReserveWBRequest(ctx context.Context, bucket string, pace time.Duration) (time.Duration, error) {
	if bucket == "" || pace < 0 {
		return 0, ErrInvalidInput
	}
	if pace == 0 {
		return 0, nil
	}
	var seconds float64
	err := store.pool.QueryRow(ctx, `
		WITH reservation AS (
			INSERT INTO procurement_wb_rate_limits (bucket, next_request_at)
			VALUES ($1, CURRENT_TIMESTAMP + make_interval(secs => $2::DOUBLE PRECISION))
			ON CONFLICT (bucket) DO UPDATE SET
				next_request_at = GREATEST(procurement_wb_rate_limits.next_request_at, CURRENT_TIMESTAMP)
					+ make_interval(secs => $2::DOUBLE PRECISION),
				updated_at = CURRENT_TIMESTAMP
			RETURNING next_request_at - make_interval(secs => $2::DOUBLE PRECISION) AS reserved_at
		)
		SELECT GREATEST(EXTRACT(EPOCH FROM (reserved_at - CURRENT_TIMESTAMP)), 0)
		FROM reservation
	`, bucket, pace.Seconds()).Scan(&seconds)
	if err != nil {
		return 0, fmt.Errorf("reserve Wildberries request slot: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// DeferWBRequests publishes the server-provided retry window to every app
// instance before the current caller returns the 429 to its durable worker.
func (store *PostgresStore) DeferWBRequests(ctx context.Context, bucket string, delay time.Duration) error {
	if bucket == "" || delay <= 0 {
		return nil
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO procurement_wb_rate_limits (bucket, next_request_at)
		VALUES ($1, CURRENT_TIMESTAMP + make_interval(secs => $2::DOUBLE PRECISION))
		ON CONFLICT (bucket) DO UPDATE SET
			next_request_at = GREATEST(procurement_wb_rate_limits.next_request_at,
				CURRENT_TIMESTAMP + make_interval(secs => $2::DOUBLE PRECISION)),
			updated_at = CURRENT_TIMESTAMP
	`, bucket, delay.Seconds())
	if err != nil {
		return fmt.Errorf("defer Wildberries request bucket: %w", err)
	}
	return nil
}

// CachedChannelProducts is the only catalogue source used by interactive WB
// screens. Network reads are owned by WBMirrorWorker.
func (store *PostgresStore) CachedChannelProducts(ctx context.Context, channel string) ([]ChannelProduct, error) {
	if channel != "wb" && channel != "ozon" {
		return nil, ErrInvalidInput
	}
	rows, err := store.pool.Query(ctx, `
		SELECT external_id, article, name, barcodes,
			current_price::DOUBLE PRECISION, current_base_price::DOUBLE PRECISION
		FROM procurement_channel_products
		WHERE channel = $1 ORDER BY external_id
	`, channel)
	if err != nil {
		return nil, fmt.Errorf("query cached %s catalogue: %w", channel, err)
	}
	defer rows.Close()
	items := make([]ChannelProduct, 0, 256)
	for rows.Next() {
		var item ChannelProduct
		if err := rows.Scan(&item.ExternalID, &item.Article, &item.Name, &item.Barcodes,
			&item.CurrentPrice, &item.CurrentBasePrice); err != nil {
			return nil, fmt.Errorf("scan cached %s catalogue: %w", channel, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read cached %s catalogue: %w", channel, err)
	}
	return items, nil
}
