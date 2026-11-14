package admin

import (
	"context"
	"encoding/json"
	"fmt"
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) RetryEditorPublication(ctx context.Context, actor Actor, productID, variantID int64, channel string) error {
	if !Can(actor.Role, PermissionProductsManage) {
		return ErrForbidden
	}
	if channel != "wb" && channel != "ozon" && channel != "saby" && channel != "flowwow" {
		return ErrInvalidInput
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id int64
	if e = tx.QueryRow(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&id); e != nil {
		return ErrInvalidInput
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&id); e != nil {
		return ErrInvalidInput
	}
	if e = ce.GuardContentPriceConflict(ctx, tx, variantID, r.editorPublicationMode); e != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, e)
	}
	var status string
	var progress ce.Progress
	var payload []byte
	if e = tx.QueryRow(ctx, `SELECT status,progress,payload FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2 FOR UPDATE`, variantID, channel).Scan(&status, &progress, &payload); e == pgx.ErrNoRows {
		return ErrInvalidInput
	} else if e != nil {
		return e
	}
	if status != "failed" && status != "needs_attention" {
		return fmt.Errorf("%w: повторная проверка доступна после ошибки", ErrInvalidInput)
	}
	if progress.CreateAttempted && progress.IDs.ProductID == "" && progress.TaskID == 0 {
		progress.Stage = "reconcile"
	}
	if channel == "ozon" && status == "failed" && progress.TaskID > 0 && !progress.Ambiguous {
		var request ce.Request
		if json.Unmarshal(payload, &request) == nil {
			if retry, ok := ce.OzonCreationRetry(request, request, progress); ok {
				progress = retry
			}
		}
	}
	progress.Checks = 0
	raw, _ := json.Marshal(progress)
	if _, e = tx.Exec(ctx, `UPDATE catalog_editor_publications SET status='queued',progress=$3,attempts=0,next_attempt_at=NOW(),lease_until=NULL,message='Запрошена повторная проверка',updated_at=NOW() WHERE variant_id=$1 AND channel=$2`, variantID, channel, raw); e != nil {
		return e
	}
	if e = insertAudit(ctx, tx, actor, "catalog.editor.publication.retry", "product", fmt.Sprint(productID), nil, map[string]any{"variantId": variantID, "channel": channel}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
