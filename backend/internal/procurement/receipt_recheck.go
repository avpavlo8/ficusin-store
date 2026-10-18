package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RecheckReceipt only queues a read of the document already recorded on the
// source action. It cannot prepare a new external document or replay a write.
func (service *Service) RecheckReceipt(ctx context.Context, actor Actor, batchID int64) (ActionBatch, error) {
	if batchID <= 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	if service.executor == nil || !service.executor.Configured("saby_receipt") {
		return ActionBatch{}, &UserFacingError{Message: "СБИС не подключён"}
	}
	store, ok := service.store.(interface {
		RecheckReceipt(context.Context, Actor, int64) (ActionBatch, error)
	})
	if !ok {
		return ActionBatch{}, ErrInvalidInput
	}
	return store.RecheckReceipt(ctx, actor, batchID)
}

func (store *PostgresStore) RecheckReceipt(ctx context.Context, actor Actor, sourceBatchID int64) (ActionBatch, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ActionBatch{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var orderID int64
	if err = tx.QueryRow(ctx, `SELECT procurement_order_id FROM procurement_action_batches WHERE id=$1 AND kind='receipt'`, sourceBatchID).Scan(&orderID); errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, ErrNotFound
	} else if err != nil {
		return ActionBatch{}, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&status); err != nil {
		return ActionBatch{}, err
	}
	if status != "ready_to_receive" {
		return ActionBatch{}, ErrInvalidInput
	}
	if err = checkReceivingComplete(ctx, tx, orderID, true); err != nil {
		return ActionBatch{}, err
	}
	var sourceID, lineID int64
	var externalID, externalURL string
	var payload json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT id,procurement_order_line_id,external_operation_id,external_url,payload
		FROM procurement_action_items WHERE batch_id=$1 AND channel='saby_receipt'
		AND external_operation_id ~ '^[0-9]+$' ORDER BY id DESC LIMIT 1 FOR UPDATE`, sourceBatchID).
		Scan(&sourceID, &lineID, &externalID, &externalURL, &payload); errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, &UserFacingError{Message: "Нет сохранённого номера документа СБИС. Повторная проверка не создаёт новое поступление."}
	} else if err != nil {
		return ActionBatch{}, err
	}
	if n, e := strconv.ParseInt(externalID, 10, 64); e != nil || n <= 0 || strings.TrimSpace(externalURL) == "" {
		return ActionBatch{}, ErrInvalidInput
	}
	var source struct {
		OrderID int64 `json:"orderId"`
		Lines   []struct {
			SabyID   string `json:"sabyId"`
			Quantity int    `json:"quantity"`
		} `json:"lines"`
	}
	if json.Unmarshal(payload, &source) != nil || source.OrderID != orderID || len(source.Lines) == 0 {
		return ActionBatch{}, ErrInvalidInput
	}
	expected := map[string]int{}
	for _, line := range source.Lines {
		if line.SabyID == "" || line.Quantity <= 0 {
			return ActionBatch{}, ErrInvalidInput
		}
		expected[line.SabyID] += line.Quantity
	}
	rows, err := tx.Query(ctx, `SELECT saby_id,SUM(COALESCE(invoiced_qty,ordered_qty))::INTEGER
		FROM procurement_order_lines WHERE procurement_order_id=$1 AND match_status='confirmed'
		AND saby_id IS NOT NULL AND NOT invoice_excluded AND reconciliation_status<>'superseded'
		GROUP BY saby_id`, orderID)
	if err != nil {
		return ActionBatch{}, err
	}
	current := map[string]int{}
	for rows.Next() {
		var id string
		var quantity int
		if err = rows.Scan(&id, &quantity); err != nil {
			rows.Close()
			return ActionBatch{}, err
		}
		current[id] = quantity
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return ActionBatch{}, err
	}
	if !reflect.DeepEqual(expected, current) {
		return ActionBatch{}, &UserFacingError{Message: "Состав закупки изменился после создания документа СБИС. Сверьте товары и количества; новый документ не создавался."}
	}
	var activeID int64
	var verifyOnly bool
	err = tx.QueryRow(ctx, `SELECT b.id,COALESCE((i.payload->>'verifyOnly')::BOOLEAN,FALSE)
		FROM procurement_action_batches b JOIN procurement_action_items i ON i.batch_id=b.id
		WHERE b.procurement_order_id=$1 AND b.kind='receipt' AND b.status NOT IN ('cancelled','completed')
		AND i.status IN ('queued','processing') ORDER BY b.id DESC LIMIT 1`, orderID).Scan(&activeID, &verifyOnly)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ActionBatch{}, err
	}
	if activeID > 0 && !verifyOnly {
		return ActionBatch{}, &UserFacingError{Message: "Проверка поступления уже выполняется"}
	}
	batchID := activeID
	if batchID == 0 {
		if _, err = tx.Exec(ctx, `UPDATE procurement_action_batches SET status='cancelled',updated_at=CURRENT_TIMESTAMP
			WHERE procurement_order_id=$1 AND kind='receipt' AND status NOT IN ('completed','cancelled','processing','approved')`, orderID); err != nil {
			return ActionBatch{}, err
		}
		var data map[string]any
		if err = json.Unmarshal(payload, &data); err != nil {
			return ActionBatch{}, err
		}
		data["verifyOnly"], data["sourceReceiptActionId"] = true, sourceID
		encoded, err := json.Marshal(data)
		if err != nil {
			return ActionBatch{}, err
		}
		if err = tx.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status,created_by,approved_by,approved_at)
			VALUES($1,'receipt','processing',$2,$2,CURRENT_TIMESTAMP) RETURNING id`, orderID, actor.CustomerID).Scan(&batchID); err != nil {
			return ActionBatch{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,quantity,status,payload,external_operation_id,external_url)
			SELECT $1,$2,'saby_receipt',$3,0,quantity,'queued',$4,$5,$6 FROM procurement_action_items WHERE id=$7`, batchID, lineID, fmt.Sprint(orderID), encoded, externalID, externalURL, sourceID); err != nil {
			return ActionBatch{}, err
		}
		if err = audit(ctx, tx, actor, "procurement.receipt.recheck", "procurement_action_batch", batchID, map[string]any{"sourceActionId": sourceID, "externalDocumentId": externalID, "readOnly": true}); err != nil {
			return ActionBatch{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ActionBatch{}, err
	}
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

// The order may have changed while a read was in flight. The finisher holds
// the order lock and checks the same immutable composition before closing it.
func receiptRecheckStillCurrent(ctx context.Context, tx pgx.Tx, batchID int64) (bool, error) {
	var status string
	var orderID int64
	if err := tx.QueryRow(ctx, `SELECT o.id,o.status FROM procurement_orders o
		JOIN procurement_action_batches b ON b.procurement_order_id=o.id WHERE b.id=$1 FOR UPDATE OF o`, batchID).Scan(&orderID, &status); err != nil {
		return false, err
	}
	if !oneOf(status, "ready_to_receive", "received") {
		return false, nil
	}
	var matches bool
	err := tx.QueryRow(ctx, `WITH expected AS (
		SELECT part->>'sabyId' id,SUM((part->>'quantity')::INTEGER) qty
		FROM procurement_action_items i CROSS JOIN LATERAL jsonb_array_elements(i.payload->'lines') part
		WHERE i.batch_id=$1 AND i.channel='saby_receipt' GROUP BY part->>'sabyId'
	), current AS (
		SELECT saby_id id,SUM(COALESCE(invoiced_qty,ordered_qty)) qty FROM procurement_order_lines
		WHERE procurement_order_id=$2 AND match_status='confirmed' AND saby_id IS NOT NULL
		AND NOT invoice_excluded AND reconciliation_status<>'superseded' GROUP BY saby_id
	) SELECT NOT EXISTS(SELECT 1 FROM expected e FULL JOIN current c USING(id) WHERE e.qty IS DISTINCT FROM c.qty)`, batchID, orderID).Scan(&matches)
	return matches, err
}
