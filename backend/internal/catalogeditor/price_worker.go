package catalogeditor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PricePublicationStep shares adapters and mode gates with card publication,
// but owns immutable, approved commands. It never creates/upserts card links.
func PricePublicationStep(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, mode string) error {
	if publisher == nil || mode != "live" && mode != "mock" {
		return nil
	}
	var id, product, variant int64
	err := pool.QueryRow(ctx, `SELECT job.id,v.product_id,job.variant_id FROM catalog_editor_price_publications job JOIN product_variants v ON v.id=job.variant_id WHERE execution_mode=$1 AND approved_at IS NOT NULL AND ((status='queued' AND next_attempt_at<=NOW()) OR(status='processing' AND lease_until<NOW())) ORDER BY next_attempt_at,job.id LIMIT 1`, mode).Scan(&id, &product, &variant)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked int64
	if err = tx.QueryRow(ctx, "SELECT id FROM products WHERE id=$1 FOR UPDATE", product).Scan(&locked); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM product_variants WHERE id=$1 FOR UPDATE", variant).Scan(&locked); err != nil {
		return err
	}
	var channel, status, target, origin, token, fingerprint, currentHash string
	var raw, state []byte
	var attempts int
	err = tx.QueryRow(ctx, `SELECT channel,status,target_id,price_origin,payload,progress,attempts,fingerprint,encode(sha256(convert_to(payload::text,'UTF8')),'hex') FROM catalog_editor_price_publications WHERE id=$1 AND execution_mode=$2 AND approved_at IS NOT NULL AND ((status='queued' AND next_attempt_at<=NOW()) OR(status='processing' AND lease_until<NOW())) FOR UPDATE SKIP LOCKED`, id, mode).Scan(&channel, &status, &target, &origin, &raw, &state, &attempts, &fingerprint, &currentHash)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var req Request
	var progress Progress
	if err = json.Unmarshal(raw, &req); err != nil {
		return err
	}
	if err = json.Unmarshal(state, &progress); err != nil {
		return err
	}
	if fingerprint != currentHash || req.VariantID != variant || req.Action != "price" || req.Price == nil || req.Price.TargetID != target {
		return fmt.Errorf("invalid frozen price command %d", id)
	}
	if status == "processing" && (progress.Stage == "price-send" || progress.Stage == "price-native-send") {
		progress.Stage = "price-verify"
		progress.Ambiguous = true
	}
	beforeWrite := progress.Stage == "" || progress.Stage == "price-send" || progress.Stage == "price-native-send"
	if beforeWrite {
		reason, e := PriceSnapshotCurrent(ctx, tx, req, channel, mode, origin)
		if e != nil {
			return e
		}
		if reason != "" {
			_, e = tx.Exec(ctx, "UPDATE catalog_editor_price_publications SET status='superseded',message=$2,lease_until=NULL,claim_token='',updated_at=NOW() WHERE id=$1", id, reason)
			if e != nil {
				return e
			}
			return tx.Commit(ctx)
		}
	}
	if beforeWrite {
		busy, err := PriceContentBusy(ctx, tx, variant, channel, mode)
		if err != nil {
			return err
		}
		if busy {
			_, err = tx.Exec(ctx, "UPDATE catalog_editor_price_publications SET next_attempt_at=NOW()+INTERVAL '10 seconds' WHERE id=$1", id)
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
	}
	state, err = json.Marshal(progress)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `UPDATE catalog_editor_price_publications SET status='processing',progress=$2,lease_until=NOW()+INTERVAL '3 minutes',claim_token=gen_random_uuid()::text,attempts=attempts+1,updated_at=NOW() WHERE id=$1 RETURNING claim_token`, id, state).Scan(&token)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	result, callErr := publisher.Advance(callCtx, channel, req, progress)
	if callErr != nil {
		if result.Progress.Stage != "" {
			progress = result.Progress
		}
		result.Progress = progress
		result.Message = callErr.Error()
		if progress.Stage == "price-send" || progress.Stage == "price-native-send" {
			result.Progress.Stage = "price-verify"
			result.Progress.Ambiguous = true
			result.Status = "queued"
		} else if progress.Stage == "price-verify" || progress.Ambiguous {
			result.Status = "queued"
		} else {
			result.Status = "failed"
		}
	}
	if result.Status == "" {
		return fmt.Errorf("price adapter returned no status")
	}
	if attempts >= 250 && result.Status == "queued" {
		result.Status = "needs_attention"
		result.Message = "Цена долго не подтверждается; повторная отправка не выполнялась"
	}
	state, err = json.Marshal(result.Progress)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `UPDATE catalog_editor_price_publications SET status=$3,message=$4,progress=$5,lease_until=NULL,claim_token='',next_attempt_at=NOW()+INTERVAL '5 seconds',updated_at=NOW() WHERE id=$1 AND claim_token=$2 AND status='processing'`, id, token, result.Status, result.Message, state)
	return err
}

func logPriceStep(ctx context.Context, pool *pgxpool.Pool, p Publisher, mode string, logger *slog.Logger) {
	if err := PricePublicationStep(ctx, pool, p, mode); err != nil && ctx.Err() == nil {
		logger.Error("catalog editor price publication", "error", err)
	}
}
