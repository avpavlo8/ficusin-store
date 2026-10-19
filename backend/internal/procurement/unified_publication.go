package procurement

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"
)

type unifiedPriceItem struct {
	VariantID   int64        `json:"variantId"`
	LineID      int64        `json:"lineId"`
	Article     string       `json:"article"`
	Scope       string       `json:"scope"`
	Quote       ChannelQuote `json:"quote"`
	BlockReason string       `json:"blockReason,omitempty"`
}
type unifiedPriceSnapshot struct {
	Version int                `json:"version"`
	Channel string             `json:"channel"`
	Items   []unifiedPriceItem `json:"items"`
}

func unifiedPriceSnapshotFor(ctx context.Context, db pricingReader, s PricingSettings, orderID int64, channel string) (unifiedPriceSnapshot, error) {
	result := unifiedPriceSnapshot{Version: s.Version, Channel: channel, Items: []unifiedPriceItem{}}
	if !oneOf(channel, "wb", "ozon") {
		return result, ErrInvalidInput
	}
	preview, err := loadUnifiedOrderPreview(ctx, db, s, orderID)
	if err != nil {
		return result, err
	}
	if preview == nil || preview.Units == 0 || preview.PricedUnits != preview.Units {
		return result, &UserFacingError{Message: "Сначала завершите расчёт всех товаров: проверьте связи, себестоимость и упаковку."}
	}
	for _, line := range preview.Lines {
		item := unifiedPriceItem{VariantID: line.VariantID, LineID: line.SourceLineID, Quote: line.Quotes[channel]}
		err = db.QueryRow(ctx, `SELECT CASE WHEN $2='wb' THEN COALESCE(d.wb_nm_ids[1],'') ELSE COALESCE(d.ozon_articles[1],'') END,v.marketplace_scope
   FROM canonical_product_directory d JOIN product_variants v ON v.id=d.variant_id WHERE d.variant_id=$1 AND d.active`, line.VariantID, channel).Scan(&item.Article, &item.Scope)
		if err != nil {
			return result, err
		}
		if item.Scope == "site_only" {
			item.BlockReason = "Маркетплейсы отключены для этого размера"
		} else if item.Article == "" {
			item.BlockReason = "Нет связи с карточкой маркетплейса"
		}
		result.Items = append(result.Items, item)
	}
	// One marketplace card cannot receive competing prices from different SKUs.
	counts := map[string]int{}
	for _, item := range result.Items {
		if item.Article != "" {
			counts[item.Article]++
		}
	}
	for i := range result.Items {
		if counts[result.Items[i].Article] > 1 {
			result.Items[i].BlockReason = "Одна карточка маркетплейса связана с несколькими размерами: уточните связь"
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].VariantID < result.Items[j].VariantID })
	return result, nil
}

func (store *PostgresStore) batchByID(ctx context.Context, orderID, batchID int64) (ActionBatch, error) {
	batches, err := store.listBatches(ctx, orderID)
	if err != nil {
		return ActionBatch{}, err
	}
	for _, batch := range batches {
		if batch.ID == batchID {
			return batch, nil
		}
	}
	return ActionBatch{}, ErrNotFound
}

// The caller holds the order lock. Only reviewable MP proposals are written;
// received purchase costs, stock, site prices and receipt documents stay intact.
func prepareUnifiedPriceBatch(ctx context.Context, tx pgx.Tx, actor Actor, orderID int64, s PricingSettings, channel string) (int64, error) {
	snapshot, err := unifiedPriceSnapshotFor(ctx, tx, s, orderID, channel)
	if err != nil {
		return 0, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return 0, err
	}
	var busy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM procurement_action_batches WHERE procurement_order_id=$1 AND kind='prices' AND (channel=$2 OR channel='') AND status IN ('approved','processing'))`, orderID, channel).Scan(&busy); err != nil {
		return 0, err
	}
	if busy {
		return 0, &UserFacingError{Message: "По этой площадке уже выполняется отправка цен. Дождитесь её результата."}
	}
	var existing int64
	err = tx.QueryRow(ctx, `SELECT id FROM procurement_action_batches WHERE procurement_order_id=$1 AND kind='prices' AND channel=$2 AND status IN ('draft','completed') AND unified_price_snapshot=$3::jsonb ORDER BY id DESC LIMIT 1`, orderID, channel, encoded).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != pgx.ErrNoRows {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE procurement_action_batches SET status='cancelled',updated_at=now() WHERE procurement_order_id=$1 AND kind='prices' AND channel=$2 AND status='draft'`, orderID, channel); err != nil {
		return 0, err
	}
	var batchID int64
	err = tx.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,channel,created_by,calculation_version,calculated_at,unified_price_snapshot) VALUES($1,'prices',$2,$3,$4,now(),$5) RETURNING id`, orderID, channel, actor.CustomerID, s.Version, encoded).Scan(&batchID)
	if err != nil {
		return 0, err
	}
	for _, item := range snapshot.Items {
		_, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,old_value,new_value,error_message,pricing_variant_id)
   VALUES($1,$2,$3,$4,(SELECT current_price FROM procurement_channel_products WHERE channel=$3 AND external_id=$4),$5,$6,$7)`, batchID, item.LineID, channel, item.Article, *item.Quote.PriceRUB, item.BlockReason, item.VariantID)
		if err != nil {
			return 0, err
		}
	}
	err = audit(ctx, tx, actor, "procurement.unified_prices.prepare", "procurement_action_batch", batchID, map[string]any{"orderId": orderID, "channel": channel, "version": s.Version, "purchaseCostsPreserved": true})
	return batchID, err
}

func (store *PostgresStore) unifiedBatchCurrent(ctx context.Context, tx pgx.Tx, batchID int64) (bool, error) {
	var orderID int64
	var channel, status string
	if err := tx.QueryRow(ctx, `SELECT b.procurement_order_id,b.channel,o.status FROM procurement_action_batches b JOIN procurement_orders o ON o.id=b.procurement_order_id WHERE b.id=$1 FOR UPDATE OF o`, batchID).Scan(&orderID, &channel, &status); err != nil {
		return false, err
	}
	if status != "received" {
		return false, nil
	}
	// Serialize with PIM edits while taking the approval/upload snapshot.
	rows, err := tx.Query(ctx, `SELECT v.id FROM product_variants v WHERE v.id IN (SELECT (x->>'variantId')::bigint FROM procurement_action_batches b CROSS JOIN LATERAL jsonb_array_elements(b.unified_price_snapshot->'items') x WHERE b.id=$1) ORDER BY v.id FOR SHARE OF v`, batchID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	s, err := loadPricingSettings(ctx, tx)
	if err != nil {
		return false, err
	}
	snapshot, err := unifiedPriceSnapshotFor(ctx, tx, s, orderID, channel)
	if _, invalid := err.(*UserFacingError); invalid {
		return false, nil
	}
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT unified_price_snapshot=$2::jsonb FROM procurement_action_batches WHERE id=$1`, batchID, encoded).Scan(&same); err != nil || !same {
		return false, err
	}
	// Validate the actual worker payload as well as the batch's input snapshot.
	var mismatches int
	err = tx.QueryRow(ctx, `WITH expected AS (SELECT jsonb_array_elements($2::jsonb->'items') x)
 SELECT COUNT(*) FROM procurement_action_items i LEFT JOIN expected e ON (e.x->>'variantId')::bigint=i.pricing_variant_id
 WHERE i.batch_id=$1 AND (e.x IS NULL OR i.external_article IS DISTINCT FROM e.x->>'article'
 OR i.channel IS DISTINCT FROM $3 OR i.new_value IS DISTINCT FROM (e.x->'quote'->>'priceRub')::numeric OR i.compare_at_value IS NOT NULL)
 `, batchID, encoded, channel).Scan(&mismatches)
	if err != nil {
		return false, err
	}
	var count, unique int
	err = tx.QueryRow(ctx, `SELECT COUNT(*),COUNT(DISTINCT pricing_variant_id) FROM procurement_action_items WHERE batch_id=$1`, batchID).Scan(&count, &unique)
	return mismatches == 0 && count == len(snapshot.Items) && unique == count, err
}

func (store *PostgresStore) approveUnifiedPriceBatch(ctx context.Context, tx pgx.Tx, actor Actor, batchID int64, configured map[string]bool) error {
	current, err := store.unifiedBatchCurrent(ctx, tx, batchID)
	if err != nil {
		return err
	}
	if !current {
		return &UserFacingError{Message: "Формула, упаковка или связь с площадкой изменилась. Подготовьте цены заново."}
	}
	var status, channel string
	if err = tx.QueryRow(ctx, `SELECT status,channel FROM procurement_action_batches WHERE id=$1 FOR UPDATE`, batchID).Scan(&status, &channel); err != nil {
		return err
	}
	if status != "draft" {
		return ErrInvalidInput
	}
	if !configured[channel] {
		return &UserFacingError{Message: "API площадки не подключён. Отправка недоступна."}
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_action_items SET status=CASE WHEN error_message<>'' THEN 'skipped' ELSE 'queued' END,
 approved_by=$2,next_attempt_at=now(),priority='interactive',updated_at=now() WHERE batch_id=$1`, batchID, actor.CustomerID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_action_batches SET status=CASE WHEN EXISTS(SELECT 1 FROM procurement_action_items WHERE batch_id=$1 AND status='queued') THEN 'processing' ELSE 'partially_completed' END,approved_by=$2,approved_at=now(),updated_at=now() WHERE id=$1`, batchID, actor.CustomerID)
	if err != nil {
		return err
	}
	return audit(ctx, tx, actor, "procurement.unified_prices.approve", "procurement_action_batch", batchID, map[string]any{"channel": channel})
}

func (store *PostgresStore) guardUnifiedPriceActions(ctx context.Context, item ActionItem) (bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var batchID int64
	var status string
	if err = tx.QueryRow(ctx, `SELECT b.id,b.status FROM procurement_action_batches b JOIN procurement_action_items i ON i.batch_id=b.id WHERE i.id=$1`, item.ID).Scan(&batchID, &status); err != nil {
		return false, err
	}
	current, err := store.unifiedBatchCurrent(ctx, tx, batchID)
	if err != nil {
		return false, err
	}
	if current && status == "processing" {
		return true, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_action_items SET status='skipped',error_message='Расчёт или связь с площадкой изменились. Подготовьте цены заново.',locked_until=NULL,lock_owner='',lock_token=lock_token+1,updated_at=now() WHERE batch_id=$1 AND status IN ('queued','processing') AND COALESCE(external_operation_id,'')=''`, batchID)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE procurement_action_batches SET status='partially_completed',updated_at=now() WHERE id=$1 AND status NOT IN ('cancelled','completed')`, batchID)
	if err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
