package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// ReturnDocuments lists accepted documents shared with the team.
func (r *PostgresRepository) ReturnDocuments(ctx context.Context, actor Actor) ([]ReturnIntake, error) {
	if !Can(actor.Role, PermissionReturnsRead) {
		return nil, ErrForbidden
	}
	return r.returnDocuments(ctx, false)
}

func (r *PostgresRepository) returnDocuments(ctx context.Context, archived bool) ([]ReturnIntake, error) {
	rows, err := r.pool.Query(ctx, `SELECT i.id::TEXT,i.revision,i.data,i.status,i.document_number,i.created_at,COALESCE((SELECT jsonb_agg(jsonb_build_object('id',r.id,'condition',r.condition,'receiptStatus',r.receipt_status,'receiptExternalUrl',r.receipt_external_url) ORDER BY r.id) FROM marketplace_returns r WHERE r.intake_id=i.id AND (r.deleted_at IS NOT NULL)=$1),'[]'::JSONB) FROM return_intakes i WHERE i.status='saved' AND (i.deleted_at IS NOT NULL)=$1 ORDER BY i.created_at DESC,i.id DESC`, archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReturnIntake{}
	for rows.Next() {
		var doc ReturnIntake
		var data, items []byte
		if err = rows.Scan(&doc.ID, &doc.Revision, &data, &doc.Status, &doc.DocumentNumber, &doc.CreatedAt, &items); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &doc.Data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(items, &doc.Items); err != nil {
			return nil, err
		}
		doc.ReceiptStatus = "none"
		result = append(result, doc)
	}
	return result, rows.Err()
}

func normalizedReturnIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, errors.New("выберите от 1 до 500 растений")
	}
	seen := map[int64]bool{}
	result := []int64{}
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("некорректное растение")
		}
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

// All selected units are locked in stable order. The durable association with an
// action is the idempotency key, including after a lost response or worker error.
func (r *PostgresRepository) CreateReturnReceiptBatch(ctx context.Context, actor Actor, ids []int64, sell bool) ([]MarketplaceReturn, error) {
	if !Can(actor.Role, PermissionReturnsReceipt) || sell && !Can(actor.Role, PermissionReturnsEdit) {
		return nil, ErrForbidden
	}
	ids, err := normalizedReturnIDs(ids)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	type line struct {
		SabyID   string `json:"sabyId"`
		Code     string `json:"code"`
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
	}
	lines := []line{}
	bySaby := map[string]int{}
	fresh := []int64{}
	for _, id := range ids {
		var condition, status, saby, code, name string
		var action *int64
		err = tx.QueryRow(ctx, `SELECT r.condition,r.receipt_status,COALESCE(d.saby_id,''),COALESCE(NULLIF(d.master_code,''),d.display_sku),d.name,r.receipt_action_id FROM marketplace_returns r JOIN canonical_product_directory d ON d.variant_id=r.canonical_variant_id WHERE r.id=$1 AND r.deleted_at IS NULL FOR UPDATE OF r`, id).Scan(&condition, &status, &saby, &code, &name, &action)
		if err != nil {
			return nil, err
		}
		if condition != "ready" && !(sell && condition == "restoring") {
			return nil, errors.New("для поступления выберите растения «В продажу»")
		}
		if action != nil || status != "none" {
			continue
		}
		numeric, parseErr := strconv.ParseInt(saby, 10, 64)
		if parseErr != nil || numeric <= 0 {
			return nil, fmt.Errorf("у растения «%s» нет связи с СБИС", name)
		}
		fresh = append(fresh, id)
		if index, ok := bySaby[saby]; ok {
			lines[index].Quantity++
		} else {
			bySaby[saby] = len(lines)
			lines = append(lines, line{saby, code, name, 1})
		}
	}
	if len(fresh) > 0 {
		anchor := fresh[0]
		payload, err := json.Marshal(map[string]any{"returnId": anchor, "orderId": anchor, "orderNumber": fmt.Sprintf("RET-%d", anchor), "lines": lines})
		if err != nil {
			return nil, err
		}
		var actionID int64
		err = tx.QueryRow(ctx, `INSERT INTO procurement_action_items(marketplace_return_id,channel,external_article,new_value,quantity,status,payload,priority,next_attempt_at) VALUES($1,'saby_receipt',$2,0,$3,'queued',$4,'interactive',CURRENT_TIMESTAMP) RETURNING id`, anchor, fmt.Sprintf("RET-%d", anchor), len(fresh), payload).Scan(&actionID)
		if err != nil {
			return nil, err
		}
		if sell {
			_, err = tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,from_condition,to_condition,comment,created_by) SELECT id,condition,'ready','Продажа экземпляра со скидкой; скидка в кассе вручную',$2 FROM marketplace_returns WHERE id=ANY($1) AND condition='restoring'`, fresh, actor.CustomerID)
			if err != nil {
				return nil, err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE marketplace_returns SET condition='ready',cost_outcome='restored',receipt_status='queued',receipt_action_id=$2,updated_by=$3,updated_at=CURRENT_TIMESTAMP WHERE id=ANY($1)`, fresh, actionID, actor.CustomerID)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `UPDATE return_intakes SET receipt_action_id=$2,updated_at=CURRENT_TIMESTAMP WHERE id IN (SELECT intake_id FROM marketplace_returns WHERE id=ANY($1))`, fresh, actionID)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.returnItems(ctx, ids)
}
func (r *PostgresRepository) returnItems(ctx context.Context, ids []int64) ([]MarketplaceReturn, error) {
	result := []MarketplaceReturn{}
	for _, id := range ids {
		item, err := r.MarketplaceReturn(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// Deletion hides journal records, retaining cost/audit evidence and external
// document identities. It never deletes or recreates a Saby receipt.
func (r *PostgresRepository) DeleteReturns(ctx context.Context, actor Actor, ids []int64, documentID string, all bool) error {
	if !Can(actor.Role, PermissionReturnsDelete) {
		return ErrForbidden
	}
	if !all && documentID == "" {
		var err error
		ids, err = normalizedReturnIDs(ids)
		if err != nil {
			return err
		}
	}
	if documentID != "" && !validIntakeID(documentID) {
		return errors.New("некорректный документ")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE marketplace_returns SET deleted_at=CURRENT_TIMESTAMP,deleted_by=$4 WHERE deleted_at IS NULL AND ($1 OR id=ANY($2::BIGINT[]) OR intake_id::TEXT=$3)`, all, ids, documentID, actor.CustomerID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE return_intakes SET deleted_at=CURRENT_TIMESTAMP,deleted_by=$3 WHERE deleted_at IS NULL AND ($1 OR id::TEXT=$2 OR (status='saved' AND NOT EXISTS(SELECT 1 FROM marketplace_returns r WHERE r.intake_id=return_intakes.id AND r.deleted_at IS NULL)))`, all, documentID, actor.CustomerID)
	if err != nil {
		return err
	}
	// Cancel only work that has never been claimed or sent to Saby.
	_, err = tx.Exec(ctx, `UPDATE procurement_action_items a SET status='skipped',error_message='Записи возврата удалены',updated_at=CURRENT_TIMESTAMP WHERE channel='saby_receipt' AND status='queued' AND attempts=0 AND external_operation_id='' AND NOT EXISTS(SELECT 1 FROM marketplace_returns r WHERE (r.receipt_action_id=a.id OR r.id=a.marketplace_return_id) AND r.deleted_at IS NULL)`)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) ReturnLabelItems(ctx context.Context, actor Actor, ids []int64) ([]MarketplaceReturn, error) {
	if !Can(actor.Role, PermissionReturnsRead) {
		return nil, ErrForbidden
	}
	ids, err := normalizedReturnIDs(ids)
	if err != nil {
		return nil, err
	}
	items, err := r.returnItems(ctx, ids)
	if err != nil {
		return nil, err
	}
	// Eligibility is checked on the current server state, including mixed selections.
	eligible := items[:0]
	for _, item := range items {
		if item.Condition == "restoring" {
			eligible = append(eligible, item)
		}
	}
	if len(eligible) == 0 {
		return nil, errors.New("этикетки печатаются только для растений на восстановлении")
	}
	for index := range eligible {
		item := &eligible[index]
		// Saby owns barcode identities; website snapshots still own the price.
		err = r.pool.QueryRow(ctx, `SELECT COALESCE((SELECT b.value FROM saby_nomenclature s CROSS JOIN LATERAL unnest(s.barcodes || ARRAY[s.barcode]) AS b(value) WHERE s.saby_id=d.saby_id AND b.value ~ '^[0-9]{13}$' ORDER BY b.value LIMIT 1),'') FROM canonical_product_directory d WHERE d.variant_id=$1`, item.VariantID).Scan(&item.Barcode)
		if err != nil {
			return nil, err
		}
	}
	return eligible, nil
}
