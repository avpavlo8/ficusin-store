package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	fw "github.com/avpavlo8/ficusin-store/backend/internal/flowwow"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
)

type FlowwowLinkInput struct {
	VariantID   int64  `json:"variantId"`
	RemoteID    int64  `json:"remoteId"`
	Fingerprint string `json:"fingerprint"`
}
type FlowwowLinkView struct {
	Stock         int                        `json:"stock"`
	Request       ce.Request                 `json:"site"`
	Remote        integration.FlowwowProduct `json:"remote"`
	ShopID        string                     `json:"shopId"`
	Fingerprint   string                     `json:"fingerprint"`
	ExecutionMode string                     `json:"executionMode"`
	Queued        bool                       `json:"queued"`
}

// Preview and confirm read the same saved data under the product/variant locks.
// No marketplace writes occur in either request: only the durable publisher sends.
func (r *PostgresRepository) FlowwowLink(ctx context.Context, actor Actor, input FlowwowLinkInput, remote integration.FlowwowProduct, shop string, apply bool) (FlowwowLinkView, error) {
	out := FlowwowLinkView{Remote: remote, ShopID: shop, ExecutionMode: r.editorPublicationMode}
	if !Can(actor.Role, PermissionIntegrationsEdit) || !Can(actor.Role, PermissionProductsManage) {
		return out, ErrForbidden
	}
	if input.VariantID <= 0 || input.RemoteID <= 0 || input.RemoteID != remote.ProductID || remote.Type != 1 || remote.SubCategoryID <= 0 || shop == "" || (r.editorPublicationMode != "mock" && r.editorPublicationMode != "live") {
		return out, fmt.Errorf("%w: выберите обычную карточку Flowwow и вариант сайта в подключённом режиме", ErrInvalidInput)
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var product int64
	if e = tx.QueryRow(ctx, `SELECT product_id FROM product_variants WHERE id=$1`, input.VariantID).Scan(&product); e != nil {
		return out, fmt.Errorf("%w: вариант сайта не найден", ErrInvalidInput)
	}
	req := ce.Request{ProductID: product, VariantID: input.VariantID, Action: "link"}
	if e = tx.QueryRow(ctx, `SELECT name,description FROM products WHERE id=$1 FOR UPDATE`, product).Scan(&req.Name, &req.Description); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT sku,base_price_minor,COALESCE(generated_barcode,'') FROM product_variants WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, input.VariantID).Scan(&req.SKU, &req.PriceMinor, &req.Barcode); e != nil {
		return out, fmt.Errorf("%w: архивный вариант нельзя связать", ErrInvalidInput)
	}
	offer := remote.OfferID
	if offer == "" {
		offer = req.SKU
	}
	req.Channel = ce.Channel{Enabled: true, Mode: "link", ExistingID: strconv.FormatInt(remote.ProductID, 10), Article: &offer, Category: ce.Category{ID: strconv.FormatInt(remote.SubCategoryID, 10)}}
	raw, _ := json.Marshal(req)
	raw, e = editorSyncContent(ctx, tx, raw, product, input.VariantID, "flowwow", offer, req.Name, req.Description, map[string]any{})
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(raw, &req); e != nil {
		return out, e
	}
	req.Action = "link"
	req.ExistingID = ""
	if req.PriceMinor <= 0 || len(req.Images) == 0 {
		return out, fmt.Errorf("%w: добавьте цену и фотографии в карточку сайта", ErrInvalidInput)
	}
	// Serialize reservations by both provider identities before checking pending work.
	for _, identity := range []string{"id:" + req.Channel.ExistingID, "offer:" + offer} {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "flowwow:"+r.editorPublicationMode+":"+identity); e != nil {
			return out, e
		}
	}
	var conflict bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_remote_links WHERE channel='flowwow' AND execution_mode=$1 AND ((variant_id=$2 AND (remote_id<>$3 OR offer_id<>$4)) OR (variant_id<>$2 AND (remote_id=$3 OR offer_id=$4)))) OR EXISTS(SELECT 1 FROM catalog_editor_publications WHERE channel='flowwow' AND execution_mode=$1 AND variant_id<>$2 AND status<>'cancelled' AND (payload->'channel'->>'existingId'=$3 OR payload->'channel'->>'article'=$4 OR pending_payload->'channel'->>'existingId'=$3 OR pending_payload->'channel'->>'article'=$4))`, r.editorPublicationMode, input.VariantID, req.Channel.ExistingID, offer).Scan(&conflict)
	if e != nil {
		return out, e
	}
	if conflict {
		return out, fmt.Errorf("%w: карточка или вариант уже участвуют в другой связи", ErrInvalidInput)
	}
	var status, priorMode string
	var priorProgress ce.Progress
	e = tx.QueryRow(ctx, `SELECT status,execution_mode,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel='flowwow' FOR UPDATE`, input.VariantID).Scan(&status, &priorMode, &priorProgress)
	if e != nil && e != pgx.ErrNoRows {
		return out, e
	}
	if priorProgress.Ambiguous || status == "failed" && priorProgress.CreateAttempted {
		return out, fmt.Errorf("%w: сначала проверьте результат предыдущей отправки Flowwow", ErrInvalidInput)
	}
	if priorMode != "" && priorMode != r.editorPublicationMode {
		return out, fmt.Errorf("%w: связь принадлежит другому режиму обмена", ErrInvalidInput)
	}
	if status == "processing" || status == "queued" || status == "needs_attention" || status == "prepared" {
		return out, fmt.Errorf("%w: дождитесь завершения предыдущей отправки Flowwow", ErrInvalidInput)
	}
	if e = tx.QueryRow(ctx, `SELECT `+fw.SiteStockSQL+` FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, input.VariantID).Scan(&out.Stock); e != nil {
		return out, e
	}
	out.Request = req
	proof, _ := json.Marshal(out)
	hash := sha256.Sum256(proof)
	out.Fingerprint = hex.EncodeToString(hash[:])
	if !apply {
		return out, nil
	}
	if input.Fingerprint != out.Fingerprint {
		return out, fmt.Errorf("%w: данные изменились; проверьте привязку ещё раз", ErrInvalidInput)
	}
	raw, _ = json.Marshal(req)
	_, e = tx.Exec(ctx, `INSERT INTO catalog_editor_publications(variant_id,channel,fingerprint,payload,status,message,execution_mode) VALUES($1,'flowwow',$2,$3,'prepared','Связь подтверждена, ожидает отправки',$4) ON CONFLICT(variant_id,channel) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,payload=EXCLUDED.payload,status='prepared',message=EXCLUDED.message,execution_mode=EXCLUDED.execution_mode,progress='{}'::jsonb,pending_payload=NULL,pending_fingerprint=NULL,next_attempt_at=NOW(),updated_at=NOW()`, input.VariantID, editorPublicationFingerprint("flowwow", raw), raw, r.editorPublicationMode)
	if e != nil {
		return out, e
	}
	var plan EditorPlan
	e = tx.QueryRow(ctx, `SELECT config FROM catalog_editor_plans WHERE product_id=$1 FOR UPDATE`, product).Scan(&plan)
	if e != nil && e != pgx.ErrNoRows {
		return out, e
	}
	found := false
	for i := range plan.Variants {
		if plan.Variants[i].VariantID == input.VariantID {
			plan.Variants[i].Flowwow = req.Channel
			found = true
		}
	}
	if !found {
		plan.Variants = append(plan.Variants, EditorVariantPlan{VariantID: input.VariantID, Flowwow: req.Channel})
	}
	config, _ := json.Marshal(plan)
	if _, e = tx.Exec(ctx, `INSERT INTO catalog_editor_plans(product_id,config) VALUES($1,catalog_editor_canonical_plan($2)) ON CONFLICT(product_id) DO UPDATE SET config=EXCLUDED.config,updated_at=NOW()`, product, config); e != nil {
		return out, e
	}
	if e = insertAudit(ctx, tx, actor, "flowwow.link", "variant", fmt.Sprint(input.VariantID), nil, map[string]any{"remoteId": remote.ProductID, "offerId": offer, "shopId": shop, "executionMode": r.editorPublicationMode}); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	out.Queued = true
	return out, nil
}

// Saved site content is reconciled even when edited outside the full editor or
// when a browser closes after saving media. Only existing, mode-scoped links qualify.
func (r *PostgresRepository) FlowwowContentStep(ctx context.Context) error {
	if r.editorPublicationMode != "live" && r.editorPublicationMode != "mock" {
		return nil
	}
	rows, e := r.pool.Query(ctx, `SELECT DISTINCT v.product_id FROM catalog_editor_remote_links l JOIN product_variants v ON v.id=l.variant_id WHERE l.channel='flowwow' AND l.execution_mode=$1 ORDER BY v.product_id`, r.editorPublicationMode)
	if e != nil {
		return e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if _, e = r.syncExistingEditorPlan(ctx, Actor{Role: RoleOwner}, id, []string{"flowwow"}, false); e != nil {
			return e
		}
	}
	return nil
}
func (r *PostgresRepository) RunFlowwowContent(ctx context.Context, logger *slog.Logger) {
	if r.editorPublicationMode != "live" && r.editorPublicationMode != "mock" {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e := r.FlowwowContentStep(ctx); e != nil && ctx.Err() == nil {
			logger.Error("Flowwow saved content sync", "error", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
