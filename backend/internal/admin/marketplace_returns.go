package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type MarketplaceReturn struct {
	PublicComment      string          `json:"publicComment"`
	DeletedAt          *time.Time      `json:"deletedAt,omitempty"`
	Label              string          `json:"label"`
	Price              float64         `json:"price"`
	DiscountPercent    int             `json:"discountPercent"`
	DiscountedPrice    float64         `json:"discountedPrice"`
	Article            string          `json:"article"`
	IntakeID           string          `json:"intakeId,omitempty"`
	ID                 int64           `json:"id"`
	Channel            string          `json:"channel"`
	ChannelComment     string          `json:"channelComment,omitempty"`
	SourceReturnID     string          `json:"sourceReturnId"`
	SourceShipmentID   string          `json:"sourceShipmentId"`
	SourceUnitIndex    int             `json:"sourceUnitIndex"`
	SalesEventID       *int64          `json:"salesEventId,omitempty"`
	VariantID          int64           `json:"variantId"`
	ProductName        string          `json:"productName"`
	SKU                string          `json:"sku"`
	ReturnedAt         string          `json:"returnedAt"`
	Condition          string          `json:"condition"`
	Comment            string          `json:"comment"`
	UnitCost           *float64        `json:"unitCost,omitempty"`
	CostOutcome        string          `json:"costOutcome"`
	FinancialStatus    string          `json:"financialStatus"`
	ReceiptStatus      string          `json:"receiptStatus"`
	ReceiptExternalURL string          `json:"receiptExternalUrl,omitempty"`
	PhotoIDs           []int64         `json:"photoIds"`
	History            []ReturnHistory `json:"history"`
}

type ReturnHistory struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"createdAt"`
}

type ReturnProduct struct {
	Price     float64 `json:"price"`
	VariantID int64   `json:"variantId"`
	Name      string  `json:"name"`
	SKU       string  `json:"sku"`
	Label     string  `json:"label"`
}

type ReturnCreate struct {
	Channel          string   `json:"channel"`
	SourceReturnID   string   `json:"sourceReturnId"`
	SourceShipmentID string   `json:"sourceShipmentId"`
	SalesEventID     *int64   `json:"salesEventId"`
	VariantID        int64    `json:"variantId"`
	Quantity         int      `json:"quantity"`
	ReturnedAt       string   `json:"returnedAt"`
	Conditions       []string `json:"conditions"`
	Comment          string   `json:"comment"`
}

type ReturnUpdate struct {
	PublicComment   *string `json:"publicComment,omitempty"`
	DiscountPercent *int    `json:"discountPercent,omitempty"`
	Condition       string  `json:"condition"`
	Comment         string  `json:"comment"`
}

func validReturnCondition(value string) bool {
	return value == "inspection" || value == "ready" || value == "restoring" || value == "dead"
}

func validReturnChannel(value string) bool {
	return value == "wb" || value == "ozon" || value == "avito" || value == "saby" || value == "other"
}

func (repository *PostgresRepository) CreateMarketplaceReturns(ctx context.Context, actor Actor, input ReturnCreate) ([]MarketplaceReturn, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return nil, ErrForbidden
	}
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	input.SourceReturnID = strings.TrimSpace(input.SourceReturnID)
	if !validReturnChannel(input.Channel) || input.VariantID <= 0 || input.Quantity < 1 || input.Quantity > 100 {
		return nil, errors.New("проверьте канал, товар и количество")
	}
	returnedAt, err := time.Parse("2006-01-02", input.ReturnedAt)
	if err != nil {
		return nil, errors.New("укажите дату возврата")
	}
	if returnedAt.After(time.Now().AddDate(0, 0, 1)) {
		return nil, errors.New("дата возврата ещё не наступила")
	}
	if len(input.Conditions) > 0 && len(input.Conditions) != input.Quantity {
		return nil, errors.New("укажите состояние каждого растения")
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cost sql.NullFloat64
	if err := tx.QueryRow(ctx, `SELECT current_unit_cost_rub::DOUBLE PRECISION FROM product_variants WHERE id=$1`, input.VariantID).Scan(&cost); err != nil {
		return nil, errors.New("товар не найден")
	}
	financial := "incomplete"
	if input.SalesEventID != nil {
		var eventVariant *int64
		var eventType string
		var saleCost sql.NullFloat64
		if err := tx.QueryRow(ctx, `SELECT canonical_variant_id,event_type,unit_cost_rub_snapshot::DOUBLE PRECISION FROM sales_events WHERE id=$1 AND channel=$2`, *input.SalesEventID, input.Channel).Scan(&eventVariant, &eventType, &saleCost); err != nil || eventVariant == nil || *eventVariant != input.VariantID || eventType != "sale" {
			return nil, errors.New("исходная продажа не соответствует возврату")
		}
		// A linked return must restore or lose the cost recognized by the
		// original sale. The current product cost may already belong to a
		// later receipt and must never rewrite this historical outcome.
		cost = saleCost
		financial = "linked"
	}
	if input.SourceReturnID == "" {
		input.SourceReturnID = fmt.Sprintf("manual:%d:%d", time.Now().UnixNano(), actor.CustomerID)
	}
	var costValue any
	if cost.Valid {
		costValue = cost.Float64
	}
	created := make([]int64, 0, input.Quantity)
	for index := 1; index <= input.Quantity; index++ {
		condition := "inspection"
		if len(input.Conditions) > 0 {
			condition = input.Conditions[index-1]
		}
		if !validReturnCondition(condition) {
			return nil, errors.New("неизвестное состояние растения")
		}
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO marketplace_returns(channel,source_return_id,source_shipment_id,source_unit_index,sales_event_id,canonical_variant_id,returned_at,condition,comment,unit_cost_rub_snapshot,cost_outcome,financial_status,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,CASE WHEN $8='ready' THEN 'restored' WHEN $8='dead' THEN 'lost' ELSE 'unknown' END,$11,$12,$12)
			ON CONFLICT(channel,source_return_id,source_unit_index) DO UPDATE SET updated_at=marketplace_returns.updated_at
			RETURNING id
		`, input.Channel, input.SourceReturnID, strings.TrimSpace(input.SourceShipmentID), index, input.SalesEventID, input.VariantID, returnedAt, condition, strings.TrimSpace(input.Comment), costValue, financial, actor.CustomerID).Scan(&id)
		if err != nil {
			return nil, err
		}
		created = append(created, id)
		if _, err := tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,to_condition,comment,created_by) SELECT $1,$2,$3,$4 WHERE NOT EXISTS(SELECT 1 FROM marketplace_return_history WHERE marketplace_return_id=$1)`, id, condition, input.Comment, actor.CustomerID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]MarketplaceReturn, 0, len(created))
	for _, id := range created {
		item, err := repository.MarketplaceReturn(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (repository *PostgresRepository) UpdateMarketplaceReturn(ctx context.Context, actor Actor, id int64, input ReturnUpdate) (MarketplaceReturn, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return MarketplaceReturn{}, ErrForbidden
	}
	if utf8.RuneCountInString(input.Comment) > 4000 || input.PublicComment != nil && utf8.RuneCountInString(*input.PublicComment) > 1000 {
		return MarketplaceReturn{}, errors.New("комментарий слишком длинный")
	}
	if !validReturnCondition(input.Condition) {
		return MarketplaceReturn{}, errors.New("неизвестное состояние")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return MarketplaceReturn{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before, receipt string
	if err := tx.QueryRow(ctx, `SELECT condition,receipt_status FROM marketplace_returns WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&before, &receipt); err != nil {
		return MarketplaceReturn{}, err
	}
	if input.DiscountPercent != nil && (*input.DiscountPercent < 0 || *input.DiscountPercent > 100) {
		return MarketplaceReturn{}, errors.New("скидка должна быть от 0 до 100%")
	}
	if _, err := tx.Exec(ctx, `UPDATE marketplace_returns SET condition=$2,comment=$3,discount_percent=COALESCE($5,discount_percent),public_comment=COALESCE($6,public_comment),receipt_status=CASE WHEN condition<>$2 AND receipt_status<>'none' THEN 'correction_required' ELSE receipt_status END,cost_outcome=CASE WHEN $2='ready' THEN 'restored' WHEN $2='dead' THEN 'lost' ELSE 'unknown' END,updated_by=$4,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, input.Condition, strings.TrimSpace(input.Comment), actor.CustomerID, input.DiscountPercent, input.PublicComment); err != nil {
		return MarketplaceReturn{}, err
	}
	if before != input.Condition {
		if _, err := tx.Exec(ctx, `INSERT INTO marketplace_return_history(marketplace_return_id,from_condition,to_condition,comment,created_by) VALUES($1,$2,$3,$4,$5)`, id, before, input.Condition, input.Comment, actor.CustomerID); err != nil {
			return MarketplaceReturn{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return MarketplaceReturn{}, err
	}
	return repository.MarketplaceReturn(ctx, id)
}

func (repository *PostgresRepository) CreateReturnReceipt(ctx context.Context, actor Actor, id int64) (MarketplaceReturn, error) {
	_, err := repository.CreateReturnReceiptBatch(ctx, actor, []int64{id}, false)
	if err != nil {
		return MarketplaceReturn{}, err
	}
	return repository.MarketplaceReturn(ctx, id)
}

func (repository *PostgresRepository) AddReturnPhoto(ctx context.Context, actor Actor, id int64, contentType, data string) (MarketplaceReturn, error) {
	return repository.writeReturnPhoto(ctx, actor, id, 0, contentType, data)
}
func (repository *PostgresRepository) ReplaceReturnPhoto(ctx context.Context, actor Actor, id, photoID int64, contentType, data string) (MarketplaceReturn, error) {
	if photoID <= 0 {
		return MarketplaceReturn{}, errors.New("некорректное фото")
	}
	return repository.writeReturnPhoto(ctx, actor, id, photoID, contentType, data)
}
func (repository *PostgresRepository) writeReturnPhoto(ctx context.Context, actor Actor, id, replacePhotoID int64, contentType, data string) (MarketplaceReturn, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return MarketplaceReturn{}, ErrForbidden
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
	if err != nil || len(raw) < 4 || len(raw) > 5<<20 {
		return MarketplaceReturn{}, errors.New("некорректное фото")
	}
	contentType = http.DetectContentType(raw)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return MarketplaceReturn{}, errors.New("выберите фото JPEG, PNG или WebP")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 80000000 {
		return MarketplaceReturn{}, errors.New("не удалось прочитать изображение")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return MarketplaceReturn{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if err = tx.QueryRow(ctx, `SELECT id FROM marketplace_returns WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&locked); err != nil {
		return MarketplaceReturn{}, err
	}
	if replacePhotoID > 0 {
		command, err := tx.Exec(ctx, `UPDATE marketplace_return_photos SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1 AND marketplace_return_id=$2 AND deleted_at IS NULL`, replacePhotoID, id)
		if err != nil {
			return MarketplaceReturn{}, err
		}
		if command.RowsAffected() != 1 {
			return MarketplaceReturn{}, errors.New("фото не найдено в этом возврате")
		}
	}
	command, err := tx.Exec(ctx, `INSERT INTO marketplace_return_photos(marketplace_return_id,content_type,image,created_by) SELECT $1,$2,$3,$4 WHERE (SELECT COUNT(*) FROM marketplace_return_photos WHERE marketplace_return_id=$1 AND deleted_at IS NULL)<6`, id, contentType, raw, actor.CustomerID)
	if err != nil {
		return MarketplaceReturn{}, err
	}
	if command.RowsAffected() == 0 {
		return MarketplaceReturn{}, errors.New("к одному возврату можно приложить не более 6 фото")
	}
	if err = tx.Commit(ctx); err != nil {
		return MarketplaceReturn{}, err
	}
	return repository.MarketplaceReturn(ctx, id)
}

func (repository *PostgresRepository) ReturnProducts(ctx context.Context, query string) ([]ReturnProduct, error) {
	rows, err := repository.pool.Query(ctx, `SELECT d.variant_id,d.name,COALESCE(d.display_sku,''),COALESCE(d.label,''),v.base_price_minor::DOUBLE PRECISION/100 FROM canonical_product_directory d JOIN products p ON p.id=d.product_id JOIN product_variants v ON v.id=d.variant_id WHERE p.catalog_section='plants' AND ($1='' OR d.name ILIKE '%'||$1||'%' OR d.display_sku ILIKE '%'||$1||'%') ORDER BY d.name,d.display_sku,d.variant_id`, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReturnProduct{}
	for rows.Next() {
		var item ReturnProduct
		if err := rows.Scan(&item.VariantID, &item.Name, &item.SKU, &item.Label, &item.Price); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) MarketplaceReturns(ctx context.Context, status, channel, query string) ([]MarketplaceReturn, error) {
	rows, err := repository.pool.Query(ctx, `SELECT r.id FROM marketplace_returns r JOIN product_variants v ON v.id=r.canonical_variant_id JOIN products p ON p.id=v.product_id WHERE r.deleted_at IS NULL AND ($1='' OR r.condition=$1) AND ($2='' OR r.channel=$2) AND ($3='' OR p.name ILIKE '%'||$3||'%' OR r.source_shipment_id ILIKE '%'||$3||'%' OR r.source_return_id ILIKE '%'||$3||'%') ORDER BY r.returned_at DESC,r.id DESC LIMIT 500`, status, channel, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	result := make([]MarketplaceReturn, 0, len(ids))
	for _, id := range ids {
		item, err := repository.MarketplaceReturn(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (repository *PostgresRepository) MarketplaceReturn(ctx context.Context, id int64) (MarketplaceReturn, error) {
	return repository.marketplaceReturn(ctx, id, false)
}

func (repository *PostgresRepository) marketplaceReturn(ctx context.Context, id int64, archived bool) (MarketplaceReturn, error) {
	item := MarketplaceReturn{PhotoIDs: []int64{}, History: []ReturnHistory{}}
	var unitCost sql.NullFloat64
	err := repository.pool.QueryRow(ctx, `SELECT r.id,r.channel,r.source_return_id,r.source_shipment_id,r.source_unit_index,r.sales_event_id,r.canonical_variant_id,p.name,COALESCE(v.sku,''),r.returned_at::TEXT,r.condition,r.comment,r.unit_cost_rub_snapshot::DOUBLE PRECISION,r.cost_outcome,r.financial_status,r.receipt_status,r.receipt_external_url,r.channel_comment,COALESCE(v.label,''),COALESCE(r.retail_price_minor_snapshot,v.base_price_minor)::DOUBLE PRECISION/100,r.discount_percent,COALESCE(r.intake_id::TEXT,''),r.public_comment,r.deleted_at FROM marketplace_returns r JOIN product_variants v ON v.id=r.canonical_variant_id JOIN products p ON p.id=v.product_id WHERE r.id=$1 AND (r.deleted_at IS NULL OR $2)`, id, archived).Scan(&item.ID, &item.Channel, &item.SourceReturnID, &item.SourceShipmentID, &item.SourceUnitIndex, &item.SalesEventID, &item.VariantID, &item.ProductName, &item.SKU, &item.ReturnedAt, &item.Condition, &item.Comment, &unitCost, &item.CostOutcome, &item.FinancialStatus, &item.ReceiptStatus, &item.ReceiptExternalURL, &item.ChannelComment, &item.Label, &item.Price, &item.DiscountPercent, &item.IntakeID, &item.PublicComment, &item.DeletedAt)
	if err != nil {
		return item, err
	}
	item.Article = fmt.Sprintf("RET-%06d", item.ID)
	item.DiscountedPrice = float64((int64(item.Price*100+0.5)*int64(100-item.DiscountPercent)+50)/100) / 100
	if unitCost.Valid {
		item.UnitCost = &unitCost.Float64
	}
	photoRows, err := repository.pool.Query(ctx, `SELECT id FROM marketplace_return_photos WHERE marketplace_return_id=$1 AND deleted_at IS NULL ORDER BY id`, id)
	if err != nil {
		return item, err
	}
	for photoRows.Next() {
		var photoID int64
		if err := photoRows.Scan(&photoID); err != nil {
			photoRows.Close()
			return item, err
		}
		item.PhotoIDs = append(item.PhotoIDs, photoID)
	}
	photoRows.Close()
	historyRows, err := repository.pool.Query(ctx, `SELECT from_condition,to_condition,comment,created_at FROM marketplace_return_history WHERE marketplace_return_id=$1 ORDER BY id`, id)
	if err != nil {
		return item, err
	}
	for historyRows.Next() {
		var history ReturnHistory
		if err := historyRows.Scan(&history.From, &history.To, &history.Comment, &history.CreatedAt); err != nil {
			historyRows.Close()
			return item, err
		}
		item.History = append(item.History, history)
	}
	historyRows.Close()
	return item, nil
}

func (repository *PostgresRepository) ReturnPhoto(ctx context.Context, id int64) (string, []byte, error) {
	var contentType string
	var data []byte
	err := repository.pool.QueryRow(ctx, `SELECT f.content_type,f.image FROM marketplace_return_photos f JOIN marketplace_returns r ON r.id=f.marketplace_return_id WHERE f.id=$1 AND f.deleted_at IS NULL AND r.deleted_at IS NULL`, id).Scan(&contentType, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	return contentType, data, err
}
