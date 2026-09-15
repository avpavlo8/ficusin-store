package procurement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	integrationCurrentEvery = 5 * time.Minute
	integrationDeepEvery    = 7 * 24 * time.Hour
)

type IntegrationSyncCoordinator interface {
	RequestIntegrationSync(context.Context, string, string) (IntegrationSyncStatus, error)
	ClaimIntegrationSync(context.Context, string, string, string, time.Duration) (*SyncClaim, error)
	FinishIntegrationSync(context.Context, SyncClaim, int, time.Time, time.Time, *time.Time, time.Duration, error) (bool, error)
	ListIntegrationSync(context.Context) ([]IntegrationSyncStatus, error)
}

func validIntegrationLane(channel, resource string) bool {
	if channel != "saby" && channel != "wb" && channel != "ozon" {
		return false
	}
	return resource == "catalog" || resource == "sales"
}

// RequestIntegrationSync coalesces clicks. During a run it records exactly one
// generation after the active snapshot, so a newly created product is not lost.
func (store *PostgresStore) RequestIntegrationSync(ctx context.Context, channel, resource string) (IntegrationSyncStatus, error) {
	if !validIntegrationLane(channel, resource) {
		return IntegrationSyncStatus{}, ErrInvalidInput
	}
	var item IntegrationSyncStatus
	err := store.pool.QueryRow(ctx, `
		UPDATE procurement_integration_sync_state SET
			requested_generation = GREATEST(requested_generation, CASE
				WHEN status='running' THEN active_generation + 1
				ELSE completed_generation + 1 END),
			status = CASE WHEN status='running' THEN status ELSE 'queued' END,
			priority='interactive', next_attempt_at=CURRENT_TIMESTAMP,
			updated_at=CURRENT_TIMESTAMP
		WHERE channel=$1 AND resource=$2
		RETURNING channel,resource,status,priority,requested_generation,
			active_generation,completed_generation,last_attempt_at,last_success_at,
			next_attempt_at,next_deep_at,cooldown_until,COALESCE(period_from::TEXT,''),
			COALESCE(period_to::TEXT,''),latest_event_at,rows_synced,last_error
	`, channel, resource).Scan(&item.Channel, &item.Resource, &item.Status, &item.Priority,
		&item.RequestedGeneration, &item.ActiveGeneration, &item.CompletedGeneration,
		&item.LastAttemptAt, &item.LastSuccessAt, &item.NextAttemptAt, &item.NextDeepAt,
		&item.CooldownUntil, &item.PeriodFrom, &item.PeriodTo, &item.LatestEventAt,
		&item.RowsSynced, &item.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return IntegrationSyncStatus{}, ErrNotFound
	}
	if err != nil {
		return IntegrationSyncStatus{}, fmt.Errorf("queue integration synchronization: %w", err)
	}
	return item, nil
}

func (store *PostgresStore) ClaimIntegrationSync(ctx context.Context, channel, resource, owner string, lease time.Duration) (*SyncClaim, error) {
	if !validIntegrationLane(channel, resource) || owner == "" || lease <= 0 {
		return nil, ErrInvalidInput
	}
	var claim SyncClaim
	err := store.pool.QueryRow(ctx, `
		UPDATE procurement_integration_sync_state SET
			status='running', last_attempt_at=CURRENT_TIMESTAMP, last_error='',
			active_generation=GREATEST(requested_generation,completed_generation+1),
			requested_generation=GREATEST(requested_generation,completed_generation+1),
			lease_owner=$3, lease_token=lease_token+1,
			lease_until=CURRENT_TIMESTAMP+make_interval(secs=>$4::DOUBLE PRECISION),
			updated_at=CURRENT_TIMESTAMP
		WHERE channel=$1 AND resource=$2
			AND next_attempt_at<=CURRENT_TIMESTAMP
			AND (cooldown_until IS NULL OR cooldown_until<=CURRENT_TIMESTAMP)
			AND (status<>'running' OR lease_until IS NULL OR lease_until<CURRENT_TIMESTAMP)
		RETURNING channel,resource,CASE WHEN next_deep_at<=CURRENT_TIMESTAMP THEN 'deep' ELSE 'current' END,
			lease_owner,lease_token,active_generation
	`, channel, resource, owner, lease.Seconds()).Scan(&claim.Channel, &claim.Resource, &claim.Mode,
		&claim.Owner, &claim.Token, &claim.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim integration synchronization: %w", err)
	}
	return &claim, nil
}

// FinishIntegrationSync is fenced by owner and monotonically increasing token.
// A worker whose lease expired cannot overwrite the result of its successor.
func (store *PostgresStore) FinishIntegrationSync(ctx context.Context, claim SyncClaim, rows int, from, to time.Time, latest *time.Time, retryAfter time.Duration, syncErr error) (bool, error) {
	if !validIntegrationLane(claim.Channel, claim.Resource) || claim.Owner == "" || claim.Token <= 0 || rows < 0 {
		return false, ErrInvalidInput
	}
	status, message := "ok", ""
	next := integrationCurrentEvery
	if claim.Resource == "catalog" {
		next = time.Hour
	}
	if syncErr != nil {
		status, message = "error", safeError(syncErr.Error())
		next = 15 * time.Minute
		if retryAfter > 0 {
			next = retryAfter
		}
	}
	var applied bool
	err := store.pool.QueryRow(ctx, `
		UPDATE procurement_integration_sync_state SET
			status=CASE WHEN $8='ok' AND requested_generation>active_generation THEN 'queued' ELSE $8 END,
			priority=CASE WHEN requested_generation>active_generation THEN 'interactive' ELSE 'background' END,
			completed_generation=CASE WHEN $8='ok' THEN active_generation ELSE completed_generation END,
			last_success_at=CASE WHEN $8='ok' THEN CURRENT_TIMESTAMP ELSE last_success_at END,
			next_attempt_at=CASE WHEN $8='ok' AND requested_generation>active_generation THEN CURRENT_TIMESTAMP
				ELSE CURRENT_TIMESTAMP+make_interval(secs=>$9::DOUBLE PRECISION) END,
			next_deep_at=CASE WHEN $8='ok' AND $7='deep' THEN CURRENT_TIMESTAMP+INTERVAL '7 days' ELSE next_deep_at END,
			cooldown_until=CASE WHEN $8='error' AND $10::DOUBLE PRECISION>0
				THEN CURRENT_TIMESTAMP+make_interval(secs=>$10::DOUBLE PRECISION) ELSE NULL END,
			period_from=CASE WHEN $8='ok' THEN $4::DATE ELSE period_from END,
			period_to=CASE WHEN $8='ok' THEN $5::DATE ELSE period_to END,
			latest_event_at=CASE WHEN $8='ok' THEN $6 ELSE latest_event_at END,
			current_cursor=CASE WHEN $8='ok' THEN jsonb_build_object('through',$5::DATE,'mode',$7) ELSE current_cursor END,
			history_cursor=CASE WHEN $8='ok' AND $7='deep' THEN jsonb_build_object('through',$5::DATE,'complete',true) ELSE history_cursor END,
			rows_synced=CASE WHEN $8='ok' THEN $3 ELSE rows_synced END,last_error=$11,
			lease_owner='',lease_until=NULL,updated_at=CURRENT_TIMESTAMP
		WHERE channel=$1 AND resource=$2 AND lease_owner=$12 AND lease_token=$13
		RETURNING TRUE
	`, claim.Channel, claim.Resource, rows, from, to, latest, claim.Mode, status,
		next.Seconds(), retryAfter.Seconds(), message, claim.Owner, claim.Token).Scan(&applied)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("finish integration synchronization: %w", err)
	}
	return applied, nil
}

func (store *PostgresStore) ListIntegrationSync(ctx context.Context) ([]IntegrationSyncStatus, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT channel,resource,status,priority,requested_generation,active_generation,
			completed_generation,last_attempt_at,last_success_at,next_attempt_at,next_deep_at,
			cooldown_until,COALESCE(period_from::TEXT,''),COALESCE(period_to::TEXT,''),
			latest_event_at,rows_synced,last_error
		FROM procurement_integration_sync_state
		ORDER BY CASE channel WHEN 'saby' THEN 0 WHEN 'ozon' THEN 1 ELSE 2 END,resource
	`)
	if err != nil {
		return nil, fmt.Errorf("query integration synchronization: %w", err)
	}
	defer rows.Close()
	items := make([]IntegrationSyncStatus, 0, 6)
	for rows.Next() {
		var item IntegrationSyncStatus
		if err := rows.Scan(&item.Channel, &item.Resource, &item.Status, &item.Priority,
			&item.RequestedGeneration, &item.ActiveGeneration, &item.CompletedGeneration,
			&item.LastAttemptAt, &item.LastSuccessAt, &item.NextAttemptAt, &item.NextDeepAt,
			&item.CooldownUntil, &item.PeriodFrom, &item.PeriodTo, &item.LatestEventAt,
			&item.RowsSynced, &item.LastError); err != nil {
			return nil, fmt.Errorf("scan integration synchronization: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *PostgresStore) ReserveIntegrationRequest(ctx context.Context, channel, bucket string, pace time.Duration) (time.Duration, error) {
	if !validIntegrationLane(channel, "sales") || bucket == "" || pace < 0 {
		return 0, ErrInvalidInput
	}
	if pace == 0 {
		return 0, nil
	}
	var seconds float64
	err := store.pool.QueryRow(ctx, `
		WITH gate AS (
			SELECT GREATEST(COALESCE(MAX(GREATEST(next_request_at,COALESCE(cooldown_until,CURRENT_TIMESTAMP))),CURRENT_TIMESTAMP),CURRENT_TIMESTAMP) opens_at
			FROM procurement_integration_rate_limits WHERE channel=$1 AND bucket IN ($2,'__account__')
		), reservation AS (
			INSERT INTO procurement_integration_rate_limits(channel,bucket,next_request_at)
			SELECT $1,name,opens_at+make_interval(secs=>$3::DOUBLE PRECISION)
			FROM gate CROSS JOIN UNNEST(ARRAY[$2::TEXT,'__account__']) name
			ON CONFLICT(channel,bucket) DO UPDATE SET next_request_at=GREATEST(
				procurement_integration_rate_limits.next_request_at,(SELECT opens_at FROM gate),CURRENT_TIMESTAMP)
				+make_interval(secs=>$3::DOUBLE PRECISION),updated_at=CURRENT_TIMESTAMP
			RETURNING bucket,next_request_at-make_interval(secs=>$3::DOUBLE PRECISION) reserved_at
		)
		SELECT GREATEST(EXTRACT(EPOCH FROM(reserved_at-CURRENT_TIMESTAMP)),0) FROM reservation WHERE bucket=$2
	`, channel, bucket, pace.Seconds()).Scan(&seconds)
	if err != nil {
		return 0, fmt.Errorf("reserve integration request: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func (store *PostgresStore) DeferIntegrationRequests(ctx context.Context, channel, bucket string, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if !validIntegrationLane(channel, "sales") || bucket == "" {
		return ErrInvalidInput
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO procurement_integration_rate_limits(channel,bucket,next_request_at,cooldown_until)
		SELECT $1,name,CURRENT_TIMESTAMP+make_interval(secs=>$3::DOUBLE PRECISION),CURRENT_TIMESTAMP+make_interval(secs=>$3::DOUBLE PRECISION)
		FROM UNNEST(ARRAY[$2::TEXT,'__account__']) name
		ON CONFLICT(channel,bucket) DO UPDATE SET
			next_request_at=GREATEST(procurement_integration_rate_limits.next_request_at,EXCLUDED.next_request_at),
			cooldown_until=GREATEST(procurement_integration_rate_limits.cooldown_until,EXCLUDED.cooldown_until),updated_at=CURRENT_TIMESTAMP
	`, channel, bucket, delay.Seconds())
	if err != nil {
		return fmt.Errorf("defer integration requests: %w", err)
	}
	return nil
}

func (store *PostgresStore) IntegrationRequestDelay(ctx context.Context, channel, bucket string) (time.Duration, error) {
	if !validIntegrationLane(channel, "sales") || bucket == "" {
		return 0, ErrInvalidInput
	}
	var seconds float64
	err := store.pool.QueryRow(ctx, `
		SELECT GREATEST(COALESCE(MAX(EXTRACT(EPOCH FROM(GREATEST(next_request_at,COALESCE(cooldown_until,CURRENT_TIMESTAMP))-CURRENT_TIMESTAMP))),0),0)
		FROM procurement_integration_rate_limits WHERE channel=$1 AND bucket IN ($2,'__account__')
	`, channel, bucket).Scan(&seconds)
	if err != nil {
		return 0, fmt.Errorf("read integration request delay: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
