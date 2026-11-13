package catalogeditor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
)

// RunPublisher never gets a client when publication is disabled. Mock clients
// use an in-memory HTTP transport, with no route to seller APIs.
func RunPublisher(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, mode string, logger *slog.Logger) {
	if publisher == nil || mode != "mock" && mode != "live" {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		logPriceStep(ctx, pool, publisher, mode, logger)
		if err := PublicationStep(ctx, pool, publisher, mode); err != nil && ctx.Err() == nil {
			logger.Error("catalog publication worker", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func PublicationStep(ctx context.Context, pool *pgxpool.Pool, publisher Publisher, mode string) error {
	if publisher == nil || mode != "mock" && mode != "live" {
		return nil
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var variant int64
	var channel, status string
	var raw, stateRaw []byte
	var attempts int
	e = tx.QueryRow(ctx, `SELECT variant_id,channel,payload,progress,status,attempts FROM catalog_editor_publications WHERE (channel IN ('wb','ozon') OR (channel='saby' AND $1='live')) AND execution_mode=$1 AND ((status IN ('prepared','queued') AND next_attempt_at<=NOW()) OR (status='processing' AND lease_until<NOW())) ORDER BY next_attempt_at,variant_id,channel FOR UPDATE SKIP LOCKED LIMIT 1`, mode).Scan(&variant, &channel, &raw, &stateRaw, &status, &attempts)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	var priceBusy bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_price_publications WHERE variant_id=$1 AND status IN('queued','processing','needs_attention'))`, variant).Scan(&priceBusy); e != nil {
		return e
	}
	if priceBusy {
		if _, e = tx.Exec(ctx, `UPDATE catalog_editor_publications SET next_attempt_at=NOW()+INTERVAL '10 seconds' WHERE variant_id=$1 AND channel=$2`, variant, channel); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	var request Request
	var progress Progress
	if e = json.Unmarshal(raw, &request); e != nil {
		return e
	}
	if e = json.Unmarshal(stateRaw, &progress); e != nil {
		return e
	}
	if progress.Stage == "" && request.Channel.Mode != "link" && request.Action != "resolve" {
		var ids RemoteIDs
		linkErr := tx.QueryRow(ctx, `SELECT remote_id,offer_id,sku,barcode FROM catalog_editor_remote_links WHERE variant_id=$1 AND channel=$2 AND execution_mode=$3`, variant, channel, mode).Scan(&ids.ProductID, &ids.OfferID, &ids.SKU, &ids.Barcode)
		if linkErr != nil && linkErr != pgx.ErrNoRows {
			return linkErr
		}
		if linkErr == nil {
			if channel == "saby" {
				var uuids []string
				if e = tx.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT external_id FROM product_external_ids WHERE variant_id=$1 AND provider='saby' AND id_type='alias' AND status='active' AND external_id~*'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')`, variant).Scan(&uuids); e != nil {
					return e
				}
				if len(uuids) == 1 {
					ids.UUID = uuids[0]
				}
			}
			progress.IDs = ids
			progress.CreateAttempted = true
			progress.Stage = "update"
			if channel == "saby" {
				progress.Stage = "hydrate"
			}
		}
	}
	if status == "processing" && progress.Stage == "create" {
		progress.Stage = "reconcile"
		progress.Ambiguous = true
	}
	if status == "processing" && channel == "saby" {
		// These leases may have died after the vendor accepted a write but
		// before its reply/progress committed. Never replay that POST blindly.
		switch progress.Stage {
		case "save", "send-save":
			progress.Stage = "verify"
			progress.Ambiguous = true
		case "move":
			progress.Stage = "move-verify"
			progress.Ambiguous = true
		case "media":
			progress.Stage = "media-unknown"
			progress.Ambiguous = true
		}
	}
	if progress.Stage == "create" {
		progress.CreateAttempted = true
	}
	stateRaw, _ = json.Marshal(progress)
	if _, e = tx.Exec(ctx, `UPDATE catalog_editor_publications SET status='processing',execution_mode=$3,progress=$4,lease_until=NOW()+INTERVAL '3 minutes',attempts=attempts+1 WHERE variant_id=$1 AND channel=$2`, variant, channel, mode, stateRaw); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	callCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	result, callErr := publisher.Advance(callCtx, channel, request, progress)
	if callErr != nil {
		if result.Progress.Stage != "" {
			progress = result.Progress
		}
		result = Result{Progress: progress, Status: "failed", Message: callErr.Error()}
		var remote interface{ HTTPStatus() int }
		if errors.As(callErr, &remote) {
			if remote.HTTPStatus() == 429 || remote.HTTPStatus() >= 500 {
				result.Status = "queued"
			}
		} else if errors.Is(callErr, context.DeadlineExceeded) {
			result.Status = "queued"
		}
		// A leased create may have reached the vendor. Reconcile instead of sending
		// it again after any unclassified transport error.
		if progress.Stage == "create" && progress.CreateAttempted {
			result.Progress.Stage = "reconcile"
			result.Progress.Ambiguous = true
			result.Status = "queued"
		}
	}
	if attempts > 250 && result.Status == "queued" {
		result.Status = "needs_attention"
		result.Message = "Площадка долго не подтверждает операцию. Отправка повторной карточки не выполнялась."
	}
	if mode == "mock" {
		result.Message = "Локальная проверка: " + result.Message + " Реальная площадка не изменялась."
	}
	if result.Status == "" {
		return errors.New("publisher returned no status")
	}
	stateRaw, e = json.Marshal(result.Progress)
	if e != nil {
		return e
	}
	tx, e = pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Saby completion updates the product as well as its variant. Match the
	// editor's product -> variant -> publication order before taking either
	// dependent row lock, otherwise a concurrent Save can deadlock completion.
	var product int64
	if channel == "saby" {
		if e = tx.QueryRow(ctx, `SELECT product_id FROM product_variants WHERE id=$1`, variant).Scan(&product); e != nil {
			return e
		}
		var lockedProduct int64
		if e = tx.QueryRow(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, product).Scan(&lockedProduct); e != nil {
			return e
		}
		var currentProduct int64
		if e = tx.QueryRow(ctx, `SELECT product_id FROM product_variants WHERE id=$1 FOR UPDATE`, variant).Scan(&currentProduct); e != nil {
			return e
		}
		if currentProduct != product {
			// A concurrent merge moved the variant while we waited. Roll back;
			// lease recovery reconciles the durable native receipt without replay.
			return errors.New("publication variant changed product during completion")
		}
	} else {
		if e = tx.QueryRow(ctx, `SELECT product_id FROM product_variants WHERE id=$1 FOR UPDATE`, variant).Scan(&product); e != nil {
			return e
		}
	}
	linkConflict := false
	if result.Progress.IDs.ProductID != "" && result.Progress.IDs.ProductID != "0" {
		occupied, err := remoteCardOccupied(ctx, tx, variant, channel, mode, result.Progress.IDs)
		if err != nil {
			return err
		}
		if occupied {
			result.Status = "failed"
			result.Message = "Этот товар площадки уже связан с другим вариантом. Прежняя связь сохранена."
			if channel == "saby" {
				// Keep the known remote draft/card receipt. Dropping its ID could
				// turn a later save into another irreversible Create.
				result.Status = "needs_attention"
				result.Progress.Stage = "conflict"
				result.Progress.CreateAttempted = true
				linkConflict = true
			} else {
				result.Progress = Progress{Stage: "conflict"}
			}
			stateRaw, _ = json.Marshal(result.Progress)
		}
	}
	if !linkConflict && result.Progress.IDs.ProductID != "" && result.Progress.IDs.ProductID != "0" && (channel != "saby" || result.Progress.IDs.OfferID != "") {
		ids := result.Progress.IDs
		if _, e = tx.Exec(ctx, "SAVEPOINT remote_link"); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO catalog_editor_remote_links(variant_id,channel,execution_mode,remote_id,offer_id,sku,barcode) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(variant_id,channel,execution_mode) DO UPDATE SET remote_id=EXCLUDED.remote_id,offer_id=EXCLUDED.offer_id,sku=EXCLUDED.sku,barcode=EXCLUDED.barcode,verified_at=NOW()`, variant, channel, mode, ids.ProductID, ids.OfferID, ids.SKU, ids.Barcode)
		if e != nil {
			var pgErr *pgconn.PgError
			if !errors.As(e, &pgErr) || pgErr.Code != "23505" {
				return e
			}
			if _, e = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT remote_link"); e != nil {
				return e
			}
			result.Status = "failed"
			result.Message = "Этот товар площадки уже связан с другим вариантом. Проверьте связь."
			if channel == "saby" {
				result.Status = "needs_attention"
				result.Progress.Stage = "conflict"
			} else {
				result.Progress.IDs = RemoteIDs{}
				result.Progress.Stage = "reconcile"
			}
			result.Progress.CreateAttempted = true
			stateRaw, _ = json.Marshal(result.Progress)
		} else if mode == "live" && (channel != "saby" || result.Status == "created" || result.Status == "synced" || result.Status == "linked") {
			// Mock IDs never reach canonical mappings or procurement.
			if e = saveLiveRemoteLink(ctx, tx, product, variant, channel, ids); e != nil {
				return e
			}

		}
	}
	delay := 20 * time.Second
	if mode == "mock" {
		delay = 0
	}
	if _, e = tx.Exec(ctx, `UPDATE catalog_editor_publications SET status=$3,message=$4,progress=$5,lease_until=NULL,next_attempt_at=NOW()+$6::interval,updated_at=NOW() WHERE variant_id=$1 AND channel=$2`, variant, channel, result.Status, result.Message, stateRaw, delay.String()); e != nil {
		return e
	}
	if result.Status == "created" || result.Status == "synced" || result.Status == "linked" || result.Status == "failed" && result.Progress.IDs.ProductID != "" {
		if _, e = tx.Exec(ctx, `UPDATE catalog_editor_publications SET payload=pending_payload,fingerprint=pending_fingerprint,progress='{}'::jsonb,status='prepared',message='Сохранены новые изменения, ожидают отправки',pending_payload=NULL,pending_fingerprint=NULL,next_attempt_at=NOW() WHERE variant_id=$1 AND channel=$2 AND pending_payload IS NOT NULL`, variant, channel); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

func remoteCardOccupied(ctx context.Context, tx pgx.Tx, variant int64, channel, mode string, ids RemoteIDs) (bool, error) {
	if channel == "saby" {
		var occupied bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants WHERE saby_id=$2 AND id<>$1) OR EXISTS(SELECT 1 FROM products WHERE saby_id=$2 AND id<>(SELECT product_id FROM product_variants WHERE id=$1)) OR EXISTS(SELECT 1 FROM product_external_ids WHERE provider='saby' AND status='active' AND (product_id<>(SELECT product_id FROM product_variants WHERE id=$1) OR (variant_id IS NOT NULL AND variant_id<>$1)) AND ((id_type='id' AND external_id=$2) OR (id_type='code' AND external_id=$3) OR (id_type='alias' AND external_id=$5 AND $5<>''))) OR EXISTS(SELECT 1 FROM catalog_editor_remote_links WHERE channel='saby' AND execution_mode=$4 AND variant_id<>$1 AND (remote_id=$2 OR offer_id=$3))`, variant, ids.ProductID, ids.OfferID, mode, ids.UUID).Scan(&occupied)
		return occupied, err
	}
	var occupied bool
	provider := "wildberries"
	if channel == "ozon" {
		provider = "ozon"
	}
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_remote_links WHERE channel=$2 AND execution_mode=$3 AND variant_id<>$1 AND (remote_id=$4 OR offer_id=$5)) OR EXISTS(SELECT 1 FROM product_external_ids WHERE provider=$6 AND variant_id IS DISTINCT FROM $1 AND ((id_type IN ('sku','nm_id') AND external_id=$4 AND $2='wb') OR (id_type IN ('vendor_code','offer_id') AND external_id=$5)))`, variant, channel, mode, ids.ProductID, ids.OfferID, provider).Scan(&occupied)
	return occupied, e
}
func saveLiveRemoteLink(ctx context.Context, tx pgx.Tx, product, variant int64, channel string, ids RemoteIDs) error {
	if channel == "saby" {
		return saveLiveSabyLink(ctx, tx, product, variant, ids)
	}
	// Keep the old procurement mirror consistent before changing canonical IDs.
	var sabyID string
	e := tx.QueryRow(ctx, `SELECT COALESCE(saby_id,'') FROM canonical_product_directory WHERE variant_id=$1`, variant).Scan(&sabyID)
	if e != nil {
		return e
	}
	if sabyID != "" {
		query := `UPDATE procurement_product_channels SET wb_nm_id=$2::bigint,wb_vendor_code=$3,wb_article=$2,updated_at=NOW() WHERE saby_id=$1 AND (SELECT COUNT(*) FROM canonical_product_directory WHERE saby_id=$1)=1`
		if channel == "ozon" {
			query = `UPDATE procurement_product_channels SET ozon_offer_id=$3,ozon_article=$3,updated_at=NOW() WHERE saby_id=$1 AND $2<>'' AND (SELECT COUNT(*) FROM canonical_product_directory WHERE saby_id=$1)=1`
		}
		if _, e = tx.Exec(ctx, query, sabyID, ids.ProductID, ids.OfferID); e != nil {
			return e
		}
	}
	provider := "wildberries"
	identities := map[string]string{"sku": ids.ProductID, "vendor_code": ids.OfferID}
	if channel == "ozon" {
		provider = "ozon"
		identities = map[string]string{"offer_id": ids.OfferID}
	}
	for typ, external := range identities {
		if _, e = tx.Exec(ctx, `UPDATE product_external_ids SET status='legacy',is_primary=FALSE,updated_at=NOW() WHERE variant_id=$1 AND provider=$2 AND (id_type=$3 OR ($3='sku' AND id_type='nm_id')) AND external_id<>$4`, variant, provider, typ, external); e != nil {
			return e
		}
		tag, err := tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,source,status,is_primary) VALUES($1,$2,$3,$4,$5,'catalog-editor','active',TRUE) ON CONFLICT(provider,id_type,external_id) DO UPDATE SET status='active',is_primary=TRUE,source='catalog-editor',updated_at=NOW() WHERE product_external_ids.variant_id=EXCLUDED.variant_id`, product, variant, provider, typ, external)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("товар площадки уже связан с другим вариантом")
		}
	}
	return nil
}
