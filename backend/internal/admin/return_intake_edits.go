package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ReturnIntakeEdit struct {
	Revision     int64             `json:"revision"`
	Data         ReturnIntakeData  `json:"data"`
	ItemVersions map[string]string `json:"itemVersions"`
}

// Document edits preserve physical IDs, photographs and external receipts.
// The document revision and every active unit version must still match the
// editor's snapshot; a concurrent assessment can never be silently overwritten.
func (r *PostgresRepository) UpdateReturnIntake(ctx context.Context, actor Actor, id string, input ReturnIntakeEdit) (*ReturnIntake, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return nil, ErrForbidden
	}
	if !validIntakeID(id) {
		return nil, errors.New("некорректный документ")
	}
	validation := input.Data
	validation.Rows = append([]ReturnIntakeRow(nil), input.Data.Rows...)
	for index := range validation.Rows {
		if validation.Rows[index].Condition == "inspection" {
			validation.Rows[index].Condition = "ready"
		}
	}
	if err := validateIntakeRows(validation, true, 500); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var revision int64
	var original []byte
	if err = tx.QueryRow(ctx, `SELECT revision,data FROM return_intakes WHERE id=$1 AND status='saved' AND deleted_at IS NULL FOR UPDATE`, id).Scan(&revision, &original); err != nil {
		return nil, err
	}
	requestJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if revision != input.Revision {
		var applied bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM return_intake_edits WHERE intake_id=$1 AND revision=$2 AND request_data=$3::JSONB)`, id, input.Revision+1, requestJSON).Scan(&applied); err != nil {
			return nil, err
		}
		if applied {
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
			return r.ReturnIntake(ctx, actor, id)
		}
		return nil, errors.New("документ изменился — закройте и откройте его заново перед редактированием")
	}
	type unit struct {
		id, variant                 int64
		version, condition, receipt string
		linked                      bool
	}
	units := map[int64]unit{}
	rows, err := tx.Query(ctx, `SELECT id,canonical_variant_id,updated_at::TEXT,condition,receipt_status,receipt_action_id IS NOT NULL OR receipt_external_id<>'' OR receipt_external_url<>'' FROM marketplace_returns WHERE intake_id=$1 AND deleted_at IS NULL ORDER BY id FOR UPDATE`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u unit
		if err = rows.Scan(&u.id, &u.variant, &u.version, &u.condition, &u.receipt, &u.linked); err != nil {
			rows.Close()
			return nil, err
		}
		units[u.id] = u
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(units) != len(input.ItemVersions) {
		return nil, errors.New("состав возврата изменился — откройте документ заново")
	}
	for uid, u := range units {
		if input.ItemVersions[strconv.FormatInt(uid, 10)] != u.version {
			return nil, errors.New("оценка растения изменилась — откройте документ заново, чтобы не потерять изменения коллеги")
		}
	}
	var beforeItems []byte
	if err = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM marketplace_returns r WHERE intake_id=$1`, id).Scan(&beforeItems); err != nil {
		return nil, err
	}
	used := map[int64]bool{}
	channelComment := ""
	if input.Data.Channel == "other" {
		channelComment = strings.TrimSpace(input.Data.ChannelComment)
	}
	var nextIndex int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(source_unit_index),0) FROM marketplace_returns WHERE intake_id=$1`, id).Scan(&nextIndex); err != nil {
		return nil, err
	}
	input.Data.Rows = append([]ReturnIntakeRow(nil), input.Data.Rows...)
	for rowIndex := range input.Data.Rows {
		row := &input.Data.Rows[rowIndex]
		// Extra IDs represent reductions of an existing grouped row. They are
		// archived below, under the same permission as journal deletion.
		keep := len(row.ItemIDs)
		if keep > row.Quantity {
			keep = row.Quantity
		}
		rowIDs := append([]int64(nil), row.ItemIDs[:keep]...)
		for _, uid := range rowIDs {
			u, ok := units[uid]
			if !ok || used[uid] {
				return nil, errors.New("экземпляр не найден в документе или указан дважды")
			}
			used[uid] = true
			if (u.linked || u.receipt != "none") && u.variant != row.VariantID {
				return nil, errors.New("у растения уже есть поступление в СБИС: замену товара оформите после корректировки поступления")
			}
			if _, err = tx.Exec(ctx, `UPDATE marketplace_returns SET channel=$2,channel_comment=$3,canonical_variant_id=$4,condition=$5,comment=$6,discount_percent=$7,unit_cost_rub_snapshot=CASE WHEN canonical_variant_id<>$4 THEN (SELECT current_unit_cost_rub FROM product_variants WHERE id=$4) ELSE unit_cost_rub_snapshot END,retail_price_minor_snapshot=CASE WHEN canonical_variant_id<>$4 THEN (SELECT base_price_minor FROM product_variants WHERE id=$4) ELSE retail_price_minor_snapshot END,receipt_status=CASE WHEN condition<>$5 AND receipt_status<>'none' THEN 'correction_required' ELSE receipt_status END,cost_outcome=CASE WHEN $5='ready' THEN 'restored' WHEN $5='dead' THEN 'lost' ELSE 'unknown' END,updated_by=$8,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, uid, input.Data.Channel, channelComment, row.VariantID, row.Condition, strings.TrimSpace(row.Comment), row.DiscountPercent, actor.CustomerID); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,from_condition,to_condition,comment,created_by) VALUES($1,$2,$3,$4,$5)`, uid, u.condition, row.Condition, "Редактирование документа приёмки. "+strings.TrimSpace(row.Comment), actor.CustomerID); err != nil {
				return nil, err
			}
		}
		var cost sql.NullFloat64
		var price int64
		if err = tx.QueryRow(ctx, `SELECT v.current_unit_cost_rub::DOUBLE PRECISION,v.base_price_minor FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1 AND p.catalog_section='plants'`, row.VariantID).Scan(&cost, &price); err != nil {
			return nil, fmt.Errorf("растение %q не найдено в каталоге", row.Name)
		}
		var costValue any
		if cost.Valid {
			costValue = cost.Float64
		}
		for len(rowIDs) < row.Quantity {
			nextIndex++
			var uid int64
			if err = tx.QueryRow(ctx, `INSERT INTO marketplace_returns(channel,channel_comment,source_return_id,source_unit_index,canonical_variant_id,returned_at,condition,comment,unit_cost_rub_snapshot,cost_outcome,created_by,updated_by,intake_id,retail_price_minor_snapshot,discount_percent) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,CASE WHEN $7='ready' THEN 'restored' WHEN $7='dead' THEN 'lost' ELSE 'unknown' END,$10,$10,$11,$12,$13) RETURNING id`, input.Data.Channel, channelComment, "intake:"+id, nextIndex, row.VariantID, time.Now().In(time.FixedZone("MSK", 10800)).Format("2006-01-02"), row.Condition, strings.TrimSpace(row.Comment), costValue, actor.CustomerID, id, price, row.DiscountPercent).Scan(&uid); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,to_condition,comment,created_by) VALUES($1,$2,$3,$4)`, uid, row.Condition, "Добавлено при редактировании приёмки. "+row.Comment, actor.CustomerID); err != nil {
				return nil, err
			}
			rowIDs = append(rowIDs, uid)
		}
		row.ItemIDs = rowIDs
	}
	for uid, u := range units {
		if !used[uid] {
			if !Can(actor.Role, PermissionReturnsDelete) {
				return nil, ErrForbidden
			}
			if u.linked || u.receipt != "none" {
				return nil, errors.New("нельзя убрать растение с поступлением в СБИС — сначала исправьте связанный документ")
			}
			if _, err = tx.Exec(ctx, `UPDATE marketplace_returns SET deleted_at=CURRENT_TIMESTAMP,deleted_by=$2,updated_by=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, uid, actor.CustomerID); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,from_condition,to_condition,comment,created_by) VALUES($1,$2,$2,'Убрано при редактировании приёмки; сохранено в удалённых',$3)`, uid, u.condition, actor.CustomerID); err != nil {
				return nil, err
			}
		}
	}
	after, err := json.Marshal(input.Data)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE return_intakes SET data=$2,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, after); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO return_intake_edits(intake_id,revision,before_data,after_data,before_items,after_items,created_by,request_data) SELECT $1,$2,$3,$4,$5,COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'),$6,$7 FROM marketplace_returns r WHERE intake_id=$1`, id, revision+1, original, after, beforeItems, actor.CustomerID, requestJSON); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ReturnIntake(ctx, actor, id)
}
