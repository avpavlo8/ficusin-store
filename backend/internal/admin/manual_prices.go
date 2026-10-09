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
	BlockReason     string    `json:"blockReason"`
	CreatedAt       time.Time `json:"createdAt"`
}

var ErrPriceChannelUnavailable = fmt.Errorf("канал изменения цены не подключён")

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
		COALESCE(pc.wb_nm_id::text,directory.wb_nm_ids[1],''),COALESCE(NULLIF(pc.ozon_offer_id,''),directory.ozon_articles[1],''),
		COALESCE(v.height_cm,0),s.package_rub::float8,s.marketplace_logistics_per_cm::float8,
		s.return_loss_rate::float8,s.marketplace_cost_rate::float8,s.tax_rate::float8,s.reserve_rate::float8,s.marketplace_strike_markup::float8
		FROM product_variants v
		LEFT JOIN saby_nomenclature n ON n.saby_id=v.saby_id
		LEFT JOIN canonical_product_directory directory ON directory.variant_id=v.id
		LEFT JOIN procurement_product_channels pc ON pc.saby_id=v.saby_id
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
		COALESCE(variant.base_price_minor=batch.manual_site_price_minor,false)
		FROM procurement_action_items item JOIN procurement_action_batches batch ON batch.id=item.batch_id
		LEFT JOIN product_variants variant ON variant.id=batch.manual_variant_id
		WHERE batch.source='manual' AND batch.manual_variant_id=$1 ORDER BY item.created_at DESC,item.id DESC`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PriceProposal{}
	for rows.Next() {
		var item PriceProposal
		var currentPrice bool
		if err = rows.Scan(&item.ID, &item.Channel, &item.ExternalArticle, &item.OldValue, &item.NewValue, &item.CompareAtValue, &item.Status, &item.ErrorMessage, &item.CreatedAt, &currentPrice); err != nil {
			return nil, err
		}
		if item.Status == "draft" {
			switch {
			case item.ExternalArticle == "":
				item.BlockReason = "Нет связи с каналом"
			case !currentPrice:
				item.BlockReason = "Цена сайта изменилась; предложение устарело"
			case item.Channel != "saby_price" && !repository.priceChannelsConfigured[item.Channel]:
				item.BlockReason = "Интеграция не подключена"
			default:
				item.CanApprove = true
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
	var item PriceProposal
	err := repository.pool.QueryRow(ctx, `UPDATE procurement_action_items item SET
		status=CASE WHEN item.channel='saby_price' THEN 'approved' ELSE 'queued' END,
		next_attempt_at=now(),priority='interactive',approved_by=$2,updated_at=now()
		FROM procurement_action_batches batch WHERE batch.id=item.batch_id AND batch.source='manual'
		AND item.id=$1 AND item.status='draft' AND item.external_article<>''
		AND EXISTS (SELECT 1 FROM product_variants variant WHERE variant.id=batch.manual_variant_id
			AND variant.base_price_minor=batch.manual_site_price_minor)
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
