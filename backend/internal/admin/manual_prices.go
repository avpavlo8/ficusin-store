package admin

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5"
)

type PriceProposal struct {
	ID              int64     `json:"id"`
	Channel         string    `json:"channel"`
	ExternalArticle string    `json:"externalArticle"`
	OldValue        *float64  `json:"oldValue"`
	NewValue        float64   `json:"newValue"`
	CompareAtValue  *float64  `json:"compareAtValue"`
	Status          string    `json:"status"`
	ErrorMessage    string    `json:"errorMessage"`
	CanApprove      bool      `json:"canApprove"`
	CanRetry        bool      `json:"canRetry"`
	BlockReason     string    `json:"blockReason"`
	CreatedAt       time.Time `json:"createdAt"`
}

var ErrPriceChannelUnavailable = fmt.Errorf("канал изменения цены не подключён")

// currentAvitoProposal verifies the exact listing, every linked active size's
// fresh Saby stock, and the cheapest available site price before requeueing.
func currentAvitoProposal(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, variantID int64, article string, price float64) (string, error) {
	var linked, stockReady bool
	var currentMin *int64
	err := query.QueryRow(ctx, `WITH listed AS (
		SELECT v.id,v.product_id,v.saby_id,v.base_price_minor
		FROM avito_listing_products lp JOIN product_variants v ON v.product_id=lp.product_id
		WHERE lp.item_id=$1 AND v.is_active<>0 AND v.archived_at IS NULL
	), fresh AS (
		SELECT listed.id,listed.base_price_minor,GREATEST(i.available_qty-i.reserved_qty,0) qty
		FROM listed JOIN products p ON p.id=listed.product_id AND 'stock'=ANY(p.saby_fields)
		JOIN saby_nomenclature n ON n.saby_id=listed.saby_id AND n.missing_since IS NULL
		JOIN inventory i ON i.variant_id=listed.id AND i.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours'
		JOIN warehouses w ON w.id=i.warehouse_id AND w.saby_id='saby-ryazan-main' AND w.is_active=1
	)
	SELECT EXISTS(SELECT 1 FROM listed WHERE id=$2)
		AND EXISTS(SELECT 1 FROM avito_listings WHERE item_id=$1 AND LOWER(status) NOT IN ('removed','blocked','rejected')),
		EXISTS(SELECT 1 FROM procurement_integration_sync_state
			WHERE channel='saby' AND resource='catalog' AND status<>'error'
				AND last_success_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours')
		AND EXISTS(SELECT 1 FROM fresh WHERE qty>0)
		AND (SELECT COUNT(*) FROM listed)=(SELECT COUNT(DISTINCT id) FROM fresh),
		(SELECT MIN(base_price_minor) FROM fresh WHERE qty>0)`, article, variantID).Scan(&linked, &stockReady, &currentMin)
	if err != nil {
		return "", err
	}
	if !linked {
		return "Объявление больше не связано с товаром", nil
	}
	if !stockReady {
		return "Нужен свежий остаток СБИС для всех размеров", nil
	}
	if currentMin == nil || *currentMin != int64(math.Round(price*100)) {
		return "Минимальная доступная цена сайта изменилась", nil
	}
	return "", nil
}

// createManualPriceProposals is called inside the same transaction as a site
// price edit. A new edit supersedes unapproved proposals for this SKU.
func createManualPriceProposals(ctx context.Context, tx pgx.Tx, actor Actor, variantID, oldMinor, newMinor int64) error {
	if _, err := tx.Exec(ctx, `UPDATE procurement_action_items item SET status='skipped',error_message='Заменено новой ручной ценой',updated_at=now()
		FROM procurement_action_batches batch WHERE batch.id=item.batch_id AND batch.source='manual'
		AND batch.manual_variant_id=$1 AND item.status IN ('draft','approved','queued','failed','not_configured')`, variantID); err != nil {
		return err
	}
	var batchID int64
	if err := tx.QueryRow(ctx, `INSERT INTO procurement_action_batches(kind,source,manual_variant_id,manual_site_price_minor,created_by)
		VALUES('prices','manual',$1,$2,$3) RETURNING id`, variantID, newMinor, actor.CustomerID).Scan(&batchID); err != nil {
		return err
	}
	var productID int64
	var sabyID, sabyCode, wbID, ozonID string
	var height float64
	var packageRUB, logisticsRate, returnRate, marketplaceRate, taxRate, reserveRate, strikeRate float64
	err := tx.QueryRow(ctx, `SELECT v.product_id,COALESCE(v.saby_id,''),COALESCE(n.code,''),
		COALESCE(directory.wb_nm_ids[1],''),COALESCE(directory.ozon_articles[1],''),
		COALESCE(v.height_cm,0),s.package_rub::float8,s.marketplace_logistics_per_cm::float8,
		s.return_loss_rate::float8,s.marketplace_cost_rate::float8,s.tax_rate::float8,s.reserve_rate::float8,s.marketplace_strike_markup::float8
		FROM product_variants v
		LEFT JOIN saby_nomenclature n ON n.saby_id=v.saby_id
		LEFT JOIN canonical_product_directory directory ON directory.variant_id=v.id
		CROSS JOIN procurement_pricing_settings s
		WHERE v.id=$1 AND s.id=1`, variantID).Scan(&productID, &sabyID, &sabyCode, &wbID, &ozonID, &height,
		&packageRUB, &logisticsRate, &returnRate, &marketplaceRate, &taxRate, &reserveRate, &strikeRate)
	if err != nil {
		return fmt.Errorf("load manual price channels: %w", err)
	}
	// Serialize price proposals for sibling variants of one product. They can
	// share an Avito listing, so both edits must not leave a draft for it.
	var lockedProductID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&lockedProductID); err != nil {
		return err
	}
	// Avito prices belong to a listing, which can contain several sizes of the
	// same product. An edit to another size invalidates the previous listing
	// proposal even though it belongs to a different manual variant batch.
	if _, err = tx.Exec(ctx, `UPDATE procurement_action_items item
		SET status='skipped',error_message='Заменено новой ценой размера товара',updated_at=now()
		FROM procurement_action_batches previous
		WHERE previous.id=item.batch_id AND previous.source='manual' AND item.channel='avito'
			AND item.status IN ('draft','queued','failed','not_configured')
			AND item.external_article IN (
				SELECT listing.item_id FROM avito_listing_products listing WHERE listing.product_id=$1
			)`, productID); err != nil {
		return err
	}
	retail := float64(newMinor) / 100
	market := math.Floor((retail + packageRUB + math.Max(height, 0)*math.Max(logisticsRate, 0)) * (1 + returnRate + marketplaceRate + taxRate + reserveRate))
	strike := math.Floor(market * (1 + strikeRate))
	for _, channel := range []struct {
		name, article string
		price         float64
		compare       *float64
	}{
		{"wb", wbID, market, &strike}, {"ozon", ozonID, market, &strike},
	} {
		var old *float64
		message := ""
		if channel.article == "" {
			message = "Не найден внешний артикул канала"
		} else {
			_ = tx.QueryRow(ctx, `SELECT current_price::float8 FROM procurement_channel_products WHERE channel=$1 AND external_id=$2`, channel.name, channel.article).Scan(&old)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,channel,external_article,old_value,new_value,compare_at_value,error_message)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, batchID, channel.name, channel.article, old, channel.price, channel.compare, message); err != nil {
			return err
		}
	}
	if sabyID != "" && sabyCode != "" {
		var old *float64
		_ = tx.QueryRow(ctx, `SELECT price_minor::float8/100 FROM saby_nomenclature WHERE saby_id=$1`, sabyID).Scan(&old)
		if _, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,channel,external_article,old_value,new_value)
			VALUES($1,'saby_price',$2,$3,$4)`, batchID, sabyCode, old, retail); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `WITH fresh_stock AS (
			SELECT variant.id variant_id,GREATEST(stock.available_qty-stock.reserved_qty,0) qty
			FROM product_variants variant
			JOIN products product ON product.id=variant.product_id AND 'stock'=ANY(product.saby_fields)
			JOIN saby_nomenclature source ON source.saby_id=variant.saby_id AND source.missing_since IS NULL
			JOIN inventory stock ON stock.variant_id=variant.id AND stock.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours'
			JOIN warehouses warehouse ON warehouse.id=stock.warehouse_id
				AND warehouse.saby_id='saby-ryazan-main' AND warehouse.is_active=1
			WHERE variant.is_active<>0 AND variant.archived_at IS NULL
		), prices AS (
			SELECT listing.item_id,listing.remote_price_minor,
				MIN(CASE WHEN stock.qty>0 THEN
					CASE WHEN variant.id=$4 THEN $3::numeric/100 ELSE variant.base_price_minor::numeric/100 END
				END) new_price
		FROM avito_listing_products target
		JOIN avito_listings listing ON listing.item_id=target.item_id
		JOIN avito_listing_products lp ON lp.item_id=listing.item_id
		JOIN product_variants variant ON variant.product_id=lp.product_id AND variant.is_active<>0
			AND variant.archived_at IS NULL
		LEFT JOIN fresh_stock stock ON stock.variant_id=variant.id
		WHERE target.product_id=$2
		GROUP BY listing.item_id,listing.remote_price_minor
		HAVING COUNT(*)=COUNT(stock.variant_id)
		)
		INSERT INTO procurement_action_items(batch_id,channel,external_article,old_value,new_value)
		SELECT $1,'avito',item_id,remote_price_minor::numeric/100,new_price
		FROM prices WHERE new_price IS NOT NULL`, batchID, productID, newMinor, variantID)
	return err
}

func (repository *PostgresRepository) ListManualPriceProposals(ctx context.Context, variantID int64) ([]PriceProposal, error) {
	rows, err := repository.pool.Query(ctx, `SELECT item.id,item.channel,item.external_article,item.old_value::float8,item.new_value::float8,
		item.compare_at_value::float8,item.status,item.error_message,item.created_at,
		COALESCE(variant.base_price_minor=batch.manual_site_price_minor,false),
		CASE item.channel
			WHEN 'wb' THEN COALESCE(directory.wb_nm_ids[1],'')
			WHEN 'ozon' THEN COALESCE(directory.ozon_articles[1],'')
			ELSE item.external_article END
		FROM procurement_action_items item JOIN procurement_action_batches batch ON batch.id=item.batch_id
		LEFT JOIN product_variants variant ON variant.id=batch.manual_variant_id
		LEFT JOIN canonical_product_directory directory ON directory.variant_id=variant.id
		WHERE batch.source='manual' AND batch.manual_variant_id=$1 ORDER BY item.created_at DESC,item.id DESC`, variantID)
	if err != nil {
		return nil, err
	}
	items := []PriceProposal{}
	type currentState struct {
		price   bool
		article string
	}
	states := []currentState{}
	for rows.Next() {
		var item PriceProposal
		var currentPrice bool
		var currentArticle string
		if err = rows.Scan(&item.ID, &item.Channel, &item.ExternalArticle, &item.OldValue, &item.NewValue, &item.CompareAtValue, &item.Status, &item.ErrorMessage, &item.CreatedAt, &currentPrice, &currentArticle); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
		states = append(states, currentState{price: currentPrice, article: currentArticle})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for index := range items {
		item := &items[index]
		currentPrice, currentArticle := states[index].price, states[index].article
		if item.Status == "draft" {
			switch {
			case item.ExternalArticle == "":
				item.BlockReason = "Нет связи с каналом"
			case !currentPrice:
				item.BlockReason = "Цена сайта изменилась; предложение устарело"
			case (item.Channel == "wb" || item.Channel == "ozon") && item.ExternalArticle != currentArticle:
				item.BlockReason = "Связь с каналом изменилась"
			case item.Channel != "saby_price" && !repository.priceChannelsConfigured[item.Channel]:
				item.BlockReason = "Интеграция не подключена"
			case item.Channel == "avito":
				item.BlockReason, err = currentAvitoProposal(ctx, repository.pool, variantID, item.ExternalArticle, item.NewValue)
				if err != nil {
					return nil, err
				}
				item.CanApprove = item.BlockReason == ""
			default:
				item.CanApprove = true
			}
		}
		if item.Status == "failed" || item.Status == "not_configured" {
			switch {
			case item.Channel != "wb" && item.Channel != "ozon" && item.Channel != "avito":
				item.BlockReason = "Повтор этой отправки недоступен"
			case !currentPrice:
				item.BlockReason = "Цена сайта изменилась; предложение устарело"
			case item.ExternalArticle == "" || (item.Channel != "avito" && item.ExternalArticle != currentArticle):
				item.BlockReason = "Связь с каналом изменилась"
			case !repository.priceChannelsConfigured[item.Channel]:
				item.BlockReason = "Интеграция не подключена"
			case item.Channel == "avito":
				item.BlockReason, err = currentAvitoProposal(ctx, repository.pool, variantID, item.ExternalArticle, item.NewValue)
				if err != nil {
					return nil, err
				}
				item.CanRetry = item.BlockReason == ""
			default:
				item.CanRetry = true
			}
		}
	}
	return items, nil
}

// RetryManualPriceProposal requeues only the exact failed marketplace price
// that still matches the site's price and the product's current channel link.
func (repository *PostgresRepository) RetryManualPriceProposal(ctx context.Context, actor Actor, proposalID int64) error {
	if !Can(actor.Role, PermissionProductsManage) {
		return ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var channel string
	err = tx.QueryRow(ctx, `SELECT item.channel FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE item.id=$1 AND batch.source='manual' AND item.status IN ('failed','not_configured')
		FOR UPDATE OF item`, proposalID).Scan(&channel)
	if err != nil {
		return err
	}
	if channel != "wb" && channel != "ozon" && channel != "avito" {
		return ErrPriceChannelUnavailable
	}
	if !repository.priceChannelsConfigured[channel] {
		return ErrPriceChannelUnavailable
	}
	if channel == "avito" {
		var variantID int64
		var article string
		var price float64
		var sitePriceCurrent bool
		err = tx.QueryRow(ctx, `SELECT batch.manual_variant_id,item.external_article,item.new_value::float8,
			variant.base_price_minor=batch.manual_site_price_minor
			FROM procurement_action_items item JOIN procurement_action_batches batch ON batch.id=item.batch_id
			JOIN product_variants variant ON variant.id=batch.manual_variant_id
			WHERE item.id=$1 AND batch.source='manual'`, proposalID).Scan(&variantID, &article, &price, &sitePriceCurrent)
		if err != nil {
			return err
		}
		if !sitePriceCurrent {
			return pgx.ErrNoRows
		}
		reason, err := currentAvitoProposal(ctx, tx, variantID, article, price)
		if err != nil {
			return err
		}
		if reason != "" {
			return pgx.ErrNoRows
		}
		command, err := tx.Exec(ctx, `UPDATE procurement_action_items SET
			status='queued',attempts=0,error_message='',next_attempt_at=now(),
			locked_until=NULL,lock_owner='',priority='interactive',updated_at=now()
			WHERE id=$1 AND status IN ('failed','not_configured')`, proposalID)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if err = insertAudit(ctx, tx, actor, "manual_price.retry", "price_proposal", fmt.Sprint(proposalID),
			map[string]any{"status": "failed"}, map[string]any{"status": "queued", "channel": channel}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	command, err := tx.Exec(ctx, `UPDATE procurement_action_items item SET
		status='queued',attempts=0,error_message='',next_attempt_at=now(),
		locked_until=NULL,lock_owner='',priority='interactive',updated_at=now()
		FROM procurement_action_batches batch
		JOIN product_variants variant ON variant.id=batch.manual_variant_id
		LEFT JOIN canonical_product_directory directory ON directory.variant_id=variant.id
		WHERE item.id=$1 AND item.batch_id=batch.id AND batch.source='manual'
			AND item.status IN ('failed','not_configured') AND item.channel IN ('wb','ozon')
			AND item.external_article<>'' AND variant.base_price_minor=batch.manual_site_price_minor
			AND item.external_article=CASE item.channel
				WHEN 'wb' THEN COALESCE(directory.wb_nm_ids[1],'')
				ELSE COALESCE(directory.ozon_articles[1],'') END`, proposalID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err = insertAudit(ctx, tx, actor, "manual_price.retry", "price_proposal", fmt.Sprint(proposalID),
		map[string]any{"status": "failed"}, map[string]any{"status": "queued", "channel": channel}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *PostgresRepository) ApproveManualPriceProposal(ctx context.Context, actor Actor, proposalID int64) (PriceProposal, error) {
	if !Can(actor.Role, PermissionProductsManage) {
		return PriceProposal{}, ErrForbidden
	}
	var channel string
	if err := repository.pool.QueryRow(ctx, `SELECT item.channel FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE item.id=$1 AND batch.source='manual' AND item.status='draft'`, proposalID).Scan(&channel); err != nil {
		return PriceProposal{}, err
	}
	if channel != "saby_price" && !repository.priceChannelsConfigured[channel] {
		return PriceProposal{}, ErrPriceChannelUnavailable
	}
	if channel == "avito" {
		var variantID int64
		var article string
		var price float64
		err := repository.pool.QueryRow(ctx, `SELECT batch.manual_variant_id,item.external_article,item.new_value::float8
			FROM procurement_action_items item JOIN procurement_action_batches batch ON batch.id=item.batch_id
			WHERE item.id=$1 AND batch.source='manual' AND item.status='draft'`, proposalID).Scan(&variantID, &article, &price)
		if err != nil {
			return PriceProposal{}, err
		}
		reason, err := currentAvitoProposal(ctx, repository.pool, variantID, article, price)
		if err != nil {
			return PriceProposal{}, err
		}
		if reason != "" {
			return PriceProposal{}, pgx.ErrNoRows
		}
	}
	var item PriceProposal
	err := repository.pool.QueryRow(ctx, `UPDATE procurement_action_items item SET
		status=CASE WHEN item.channel='saby_price' THEN 'approved' ELSE 'queued' END,
		next_attempt_at=now(),priority='interactive',approved_by=$2,updated_at=now()
		FROM procurement_action_batches batch WHERE batch.id=item.batch_id AND batch.source='manual'
		AND item.id=$1 AND item.status='draft' AND item.external_article<>''
		AND EXISTS (SELECT 1 FROM product_variants variant WHERE variant.id=batch.manual_variant_id
			AND variant.base_price_minor=batch.manual_site_price_minor)
		AND (item.channel NOT IN ('wb','ozon') OR EXISTS (
			SELECT 1 FROM product_variants variant
			LEFT JOIN canonical_product_directory directory ON directory.variant_id=variant.id
			WHERE variant.id=batch.manual_variant_id AND item.external_article=CASE item.channel
				WHEN 'wb' THEN COALESCE(directory.wb_nm_ids[1],'')
				ELSE COALESCE(directory.ozon_articles[1],'') END))
		RETURNING item.id,item.channel,item.external_article,item.old_value::float8,item.new_value::float8,
		item.compare_at_value::float8,item.status,item.error_message,item.created_at`, proposalID, actor.CustomerID).Scan(
		&item.ID, &item.Channel, &item.ExternalArticle, &item.OldValue, &item.NewValue, &item.CompareAtValue, &item.Status, &item.ErrorMessage, &item.CreatedAt)
	return item, err
}

func (repository *PostgresRepository) ManualSabyPriceXLSX(ctx context.Context, proposalID int64) ([]byte, string, error) {
	var variantID int64
	var sabyID, code string
	var price int64
	err := repository.pool.QueryRow(ctx, `SELECT batch.manual_variant_id,COALESCE(variant.saby_id,''),item.external_article,
		ROUND(item.new_value)::bigint FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		JOIN product_variants variant ON variant.id=batch.manual_variant_id
		WHERE item.id=$1 AND batch.source='manual' AND item.channel='saby_price' AND item.status='approved'
			AND variant.base_price_minor=batch.manual_site_price_minor
			AND EXISTS (SELECT 1 FROM saby_nomenclature current_saby
				WHERE current_saby.saby_id=variant.saby_id AND current_saby.code=item.external_article
					AND current_saby.missing_since IS NULL)`, proposalID).Scan(&variantID, &sabyID, &code, &price)
	if err != nil {
		return nil, "", err
	}
	line := procurement.OrderLine{CanonicalVariantID: &variantID, SabyID: sabyID, SabyCode: code,
		MatchStatus: "confirmed", ProposedRetailRUB: &price, PriceChangeNeeded: true}
	content, _, err := procurement.BuildSabyPriceXLSX([]procurement.OrderLine{line})
	if err != nil {
		return nil, "", err
	}
	return content, fmt.Sprintf("saby-manual-price-%d.xlsx", proposalID), nil
}
