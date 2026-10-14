package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ReturnIntakeRow struct {
	DiscountPercent int    `json:"discountPercent"`
	Key             string `json:"key"`
	VariantID       int64  `json:"variantId"`
	Name            string `json:"name"`
	SKU             string `json:"sku"`
	Search          string `json:"search"`
	Quantity        int    `json:"quantity"`
	Condition       string `json:"condition"`
	Comment         string `json:"comment"`
}
type ReturnIntakeData struct {
	Channel        string            `json:"channel"`
	ChannelComment string            `json:"channelComment"`
	Rows           []ReturnIntakeRow `json:"rows"`
}
type ReturnIntake struct {
	DocumentNumber     int64               `json:"documentNumber"`
	CreatedAt          time.Time           `json:"createdAt"`
	ID                 string              `json:"id"`
	Revision           int64               `json:"revision"`
	Data               ReturnIntakeData    `json:"data"`
	Status             string              `json:"status"`
	ReceiptStatus      string              `json:"receiptStatus"`
	ReceiptExternalURL string              `json:"receiptExternalUrl,omitempty"`
	ReceiptError       string              `json:"receiptError,omitempty"`
	Items              []MarketplaceReturn `json:"items"`
}
type ReturnIntakeSave struct {
	Revision int64            `json:"revision"`
	Data     ReturnIntakeData `json:"data"`
}

func validIntakeID(id string) bool {
	var value pgtype.UUID
	return len(id) == 36 && value.Scan(id) == nil && value.Valid
}

func validateIntake(data ReturnIntakeData, complete bool) error {
	if data.Channel != "ozon" && data.Channel != "wb" && data.Channel != "other" {
		return errors.New("выберите канал возврата")
	}
	if len(data.Rows) > 100 || len(data.ChannelComment) > 4000 {
		return errors.New("слишком большой возврат")
	}
	if complete && len(data.Rows) == 0 {
		return errors.New("добавьте растения в возврат")
	}
	keys := map[string]bool{}
	total := 0
	for _, row := range data.Rows {
		if row.DiscountPercent < 0 || row.DiscountPercent > 100 || row.Key == "" || keys[row.Key] || row.VariantID < 0 || row.Quantity < 0 || row.Quantity > 100 || len(row.Comment) > 4000 || len(row.Name) > 1000 || len(row.Search) > 1000 || len(row.SKU) > 200 || !validReturnCondition(row.Condition) || row.Condition == "inspection" {
			return errors.New("проверьте строки возврата")
		}
		keys[row.Key] = true
		total += row.Quantity
		if complete && (row.VariantID == 0 || row.Quantity < 1) {
			return errors.New("выберите растение из каталога и укажите количество в каждой строке")
		}
	}
	if total > 500 {
		return errors.New("в одном возврате можно принять не более 500 растений")
	}
	return nil
}

func (repository *PostgresRepository) ReturnIntakeDraft(ctx context.Context, actor Actor) (*ReturnIntake, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return nil, ErrForbidden
	}
	var id string
	err := repository.pool.QueryRow(ctx, `SELECT id::TEXT FROM return_intakes WHERE created_by=$1 AND deleted_at IS NULL AND status='draft' ORDER BY updated_at DESC LIMIT 1`, actor.CustomerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return repository.ReturnIntake(ctx, actor, id)
}

func (repository *PostgresRepository) ReturnIntake(ctx context.Context, actor Actor, id string) (*ReturnIntake, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return nil, ErrForbidden
	}
	if !validIntakeID(id) {
		return nil, errors.New("некорректный номер черновика")
	}
	result := &ReturnIntake{ID: id, Items: []MarketplaceReturn{}, ReceiptStatus: "none"}
	var raw []byte
	var actionID *int64
	err := repository.pool.QueryRow(ctx, `SELECT revision,data,status,receipt_action_id,document_number,created_at FROM return_intakes WHERE id=$1 AND deleted_at IS NULL AND (created_by=$2 OR status='saved')`, id, actor.CustomerID).Scan(&result.Revision, &raw, &result.Status, &actionID, &result.DocumentNumber, &result.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &result.Data); err != nil {
		return nil, err
	}
	if actionID != nil {
		if err = repository.pool.QueryRow(ctx, `SELECT CASE WHEN status='completed' THEN COALESCE((SELECT receipt_status FROM marketplace_returns WHERE receipt_action_id=procurement_action_items.id ORDER BY id LIMIT 1),'draft_created') WHEN status IN ('failed','not_configured','skipped') THEN 'failed' WHEN external_operation_id<>'' THEN 'draft_created' WHEN status='queued' THEN 'queued' ELSE 'checking' END,external_url,error_message FROM procurement_action_items WHERE id=$1`, *actionID).Scan(&result.ReceiptStatus, &result.ReceiptExternalURL, &result.ReceiptError); err != nil {
			return nil, err
		}
	}
	rows, err := repository.pool.Query(ctx, `SELECT id FROM marketplace_returns WHERE intake_id=$1 AND deleted_at IS NULL ORDER BY source_unit_index`, id)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var itemID int64
		if err = rows.Scan(&itemID); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, itemID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, itemID := range ids {
		item, err := repository.MarketplaceReturn(ctx, itemID)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (repository *PostgresRepository) SaveReturnIntakeDraft(ctx context.Context, actor Actor, id string, input ReturnIntakeSave) (*ReturnIntake, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return nil, ErrForbidden
	}
	if !validIntakeID(id) || input.Revision < 0 {
		return nil, errors.New("некорректный номер или версия черновика")
	}
	if err := validateIntake(input.Data, false); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(input.Data)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Insert only on the first revision. The ID is a durable idempotency key.
	if input.Revision == 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO return_intakes(id,created_by,data) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, id, actor.CustomerID, raw); err != nil {
			return nil, err
		}
	}
	var current int64
	var stored []byte
	var status string
	if err = tx.QueryRow(ctx, `SELECT revision,data,status FROM return_intakes WHERE id=$1 AND created_by=$2 AND deleted_at IS NULL FOR UPDATE`, id, actor.CustomerID).Scan(&current, &stored, &status); err != nil {
		return nil, err
	}
	// JSONB changes key order, so compare semantic data for a lost-response retry.
	var previous ReturnIntakeData
	if err = json.Unmarshal(stored, &previous); err != nil {
		return nil, err
	}
	previousRaw, _ := json.Marshal(previous)
	same := bytes.Equal(previousRaw, raw)
	if status != "draft" {
		return nil, errors.New("возврат уже сохранён в журнале")
	}
	if current != input.Revision && !(same && current == input.Revision+1) {
		return nil, errors.New("черновик изменён в другом окне — закройте форму и откройте её снова")
	}
	if current == input.Revision {
		if _, err = tx.Exec(ctx, `UPDATE return_intakes SET data=$2,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, raw); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return repository.ReturnIntake(ctx, actor, id)
}

// Accept and receipt are serialized against autosave and each other. A failed
// receipt validation rolls back the ledger too, leaving the editable draft.
func (repository *PostgresRepository) AcceptReturnIntake(ctx context.Context, actor Actor, id string, revision int64, receipt bool) (*ReturnIntake, error) {
	if !Can(actor.Role, PermissionReturnsEdit) || receipt && !Can(actor.Role, PermissionReturnsReceipt) {
		return nil, ErrForbidden
	}
	if !validIntakeID(id) {
		return nil, errors.New("некорректный номер черновика")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current int64
	var raw []byte
	var status string
	var actionID *int64
	if err = tx.QueryRow(ctx, `SELECT revision,data,status,receipt_action_id FROM return_intakes WHERE id=$1 AND deleted_at IS NULL AND (created_by=$2 OR status='saved') FOR UPDATE`, id, actor.CustomerID).Scan(&current, &raw, &status, &actionID); err != nil {
		return nil, err
	}
	if status == "saved" {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		doc, err := repository.ReturnIntake(ctx, actor, id)
		if err != nil {
			return nil, err
		}
		if receipt {
			ids := []int64{}
			for _, item := range doc.Items {
				if item.Condition == "ready" && item.ReceiptStatus == "none" {
					ids = append(ids, item.ID)
				}
			}
			if len(ids) > 0 {
				if _, err = repository.CreateReturnReceiptBatch(ctx, actor, ids, false); err != nil {
					return nil, err
				}
				return repository.ReturnIntake(ctx, actor, id)
			}
		}
		return doc, nil
	} else if current != revision {
		return nil, errors.New("версия черновика изменилась — дождитесь автосохранения")
	}
	var data ReturnIntakeData
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if err = validateIntake(data, true); err != nil {
		return nil, err
	}
	if status == "draft" {
		channelComment := ""
		if data.Channel == "other" {
			channelComment = strings.TrimSpace(data.ChannelComment)
		}
		unit := 0
		for _, row := range data.Rows {
			var cost sql.NullFloat64
			var price int64
			if err = tx.QueryRow(ctx, `SELECT v.current_unit_cost_rub::DOUBLE PRECISION,v.base_price_minor FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1 AND p.catalog_section='plants'`, row.VariantID).Scan(&cost, &price); err != nil {
				return nil, fmt.Errorf("растение %q не найдено в каталоге", row.Name)
			}
			var costValue any
			if cost.Valid {
				costValue = cost.Float64
			}
			for index := 0; index < row.Quantity; index++ {
				unit++
				var itemID int64
				if err = tx.QueryRow(ctx, `INSERT INTO marketplace_returns(channel,channel_comment,source_return_id,source_unit_index,canonical_variant_id,returned_at,condition,comment,unit_cost_rub_snapshot,cost_outcome,created_by,updated_by,intake_id,retail_price_minor_snapshot,discount_percent) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,CASE WHEN $7='ready' THEN 'restored' WHEN $7='dead' THEN 'lost' ELSE 'unknown' END,$10,$10,$11,$12,$13) RETURNING id`, data.Channel, channelComment, "intake:"+id, unit, row.VariantID, time.Now().In(time.FixedZone("MSK", 3*60*60)).Format("2006-01-02"), row.Condition, strings.TrimSpace(row.Comment), costValue, actor.CustomerID, id, price, row.DiscountPercent).Scan(&itemID); err != nil {
					return nil, err
				}
				if _, err = tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,to_condition,comment,created_by) VALUES($1,$2,$3,$4)`, itemID, row.Condition, row.Comment, actor.CustomerID); err != nil {
					return nil, err
				}
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE return_intakes SET status='saved',revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id); err != nil {
			return nil, err
		}
	}
	if receipt {
		rows, err := tx.Query(ctx, `SELECT MIN(r.id),d.saby_id,COALESCE(NULLIF(d.master_code,''),d.display_sku),d.name,COUNT(*)::INTEGER FROM marketplace_returns r JOIN canonical_product_directory d ON d.variant_id=r.canonical_variant_id WHERE r.intake_id=$1 AND r.condition='ready' AND r.receipt_status='none' AND r.deleted_at IS NULL GROUP BY d.saby_id,d.master_code,d.display_sku,d.name ORDER BY MIN(r.id)`, id)
		if err != nil {
			return nil, err
		}
		lines := []map[string]any{}
		var anchor int64
		total := 0
		for rows.Next() {
			var itemID int64
			var sabyID, code, name string
			var quantity int
			if err = rows.Scan(&itemID, &sabyID, &code, &name, &quantity); err != nil {
				rows.Close()
				return nil, err
			}
			numericID, parseErr := strconv.ParseInt(sabyID, 10, 64)
			if parseErr != nil || numericID <= 0 {
				rows.Close()
				return nil, fmt.Errorf("у растения «%s» нет связи с СБИС", name)
			}
			if anchor == 0 {
				anchor = itemID
			}
			total += quantity
			lines = append(lines, map[string]any{"sabyId": sabyID, "code": code, "name": name, "quantity": quantity})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(lines) == 0 {
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
			return repository.ReturnIntake(ctx, actor, id)
		}
		payload, err := json.Marshal(map[string]any{"returnId": anchor, "orderId": anchor, "orderNumber": fmt.Sprintf("RET-%d", anchor), "lines": lines})
		if err != nil {
			return nil, err
		}
		var newActionID int64
		if err = tx.QueryRow(ctx, `INSERT INTO procurement_action_items(marketplace_return_id,channel,external_article,new_value,quantity,status,payload,priority,next_attempt_at) VALUES($1,'saby_receipt',$2,0,$3,'queued',$4,'interactive',CURRENT_TIMESTAMP) RETURNING id`, anchor, "RET-"+id, total, payload).Scan(&newActionID); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE marketplace_returns SET receipt_status='queued',receipt_action_id=$2,updated_at=CURRENT_TIMESTAMP WHERE intake_id=$1 AND condition='ready' AND receipt_status='none' AND deleted_at IS NULL`, id, newActionID); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE return_intakes SET receipt_action_id=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, newActionID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return repository.ReturnIntake(ctx, actor, id)
}
