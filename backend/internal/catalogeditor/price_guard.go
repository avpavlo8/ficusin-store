package catalogeditor

import (
	"context"
	"fmt"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5"
)

type PriceQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PriceSnapshotCurrent validates before approval and immediately before the
// first write. After a write has started, workers reconcile its saved receipt
// even if the local price or link has since changed.
func PriceSnapshotCurrent(ctx context.Context, q PriceQuery, r Request, channel, mode, origin string) (string, error) {
	if r.Price == nil || r.Price.TargetID == "" || r.Price.PriceMinor <= 0 {
		return "Нет точной цены или идентификатора канала", nil
	}
	if channel == "wb" {
		price := float64(r.Price.PriceMinor) / 100
		base, discount := int64(price), int64(0)
		if r.Price.CompareAtMinor != nil && *r.Price.CompareAtMinor > r.Price.PriceMinor {
			base = *r.Price.CompareAtMinor / 100
			if base > 0 {
				discount = int64((1 - price/float64(base)) * 100)
			}
		}
		if base <= 0 || discount < 0 || discount > 99 {
			return "Цена или скидка Wildberries не поддерживается площадкой", nil
		}
	}
	var current int64
	var sabyID, code, scope string
	var exists bool
	err := q.QueryRow(ctx, `SELECT base_price_minor,COALESCE(v.saby_id,''),COALESCE(n.code,''),marketplace_scope,v.archived_at IS NULL FROM product_variants v LEFT JOIN saby_nomenclature n ON n.saby_id=v.saby_id WHERE v.id=$1 AND v.product_id=$2`, r.VariantID, r.ProductID).Scan(&current, &sabyID, &code, &scope, &exists)
	if err == pgx.ErrNoRows {
		return "Вариант удалён или перенесён", nil
	}
	if err != nil {
		return "", err
	}
	if !exists || current != r.PriceMinor {
		return "Цена сайта или состояние варианта изменились", nil
	}
	switch channel {
	case "wb", "ozon":
		if scope == "site_only" {
			return "Канал отключён для варианта", nil
		}
		var mapped bool
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM canonical_product_directory d WHERE d.variant_id=$1 AND $2=ANY(CASE WHEN $3='wb' THEN d.wb_nm_ids ELSE d.ozon_articles END)) OR EXISTS(SELECT 1 FROM catalog_editor_remote_links l WHERE l.variant_id=$1 AND l.channel=$3 AND l.execution_mode=$4 AND $2=CASE WHEN $3='wb' THEN l.remote_id ELSE l.offer_id END)`, r.VariantID, r.Price.TargetID, channel, mode).Scan(&mapped)
		if err != nil {
			return "", err
		}
		if !mapped {
			return "Связь площадки изменилась", nil
		}
		if origin == "formula" {
			wanted, compare, available, e := PriceFormula(ctx, q, r.VariantID, channel, current)
			if e != nil {
				return "", e
			}
			if !available {
				return "Формула больше не даёт цену канала", nil
			}
			if wanted != r.Price.PriceMinor || r.Price.CompareAtMinor == nil || *r.Price.CompareAtMinor != compare {
				return "Формула, скидка или себестоимость изменилась", nil
			}
		}
	case "saby":
		if sabyID == "" || code != r.Price.TargetID || r.SabyID != "" && r.SabyID != sabyID {
			return "Связь СБИС изменилась", nil
		}
	case "avito":
		return avitoPriceCurrent(ctx, q, r.VariantID, r.Price.TargetID, r.Price.PriceMinor)
	default:
		return "Неизвестный канал", nil
	}
	return "", nil
}

func avitoPriceCurrent(ctx context.Context, q PriceQuery, variant int64, target string, minor int64) (string, error) {
	var linked, fresh bool
	var minimum *int64
	err := q.QueryRow(ctx, `WITH listed AS(SELECT v.id,v.product_id,v.saby_id,v.base_price_minor FROM avito_listing_products lp JOIN product_variants v ON v.product_id=lp.product_id WHERE lp.item_id=$1 AND v.is_active<>0 AND v.archived_at IS NULL),fresh AS(SELECT listed.id,listed.base_price_minor,GREATEST(i.available_qty-i.reserved_qty,0) qty FROM listed JOIN products p ON p.id=listed.product_id AND 'stock'=ANY(p.saby_fields) JOIN saby_nomenclature n ON n.saby_id=listed.saby_id AND n.missing_since IS NULL JOIN inventory i ON i.variant_id=listed.id AND i.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours' JOIN warehouses w ON w.id=i.warehouse_id AND w.saby_id='saby-ryazan-main' AND w.is_active=1) SELECT EXISTS(SELECT 1 FROM listed WHERE id=$2) AND EXISTS(SELECT 1 FROM avito_listings WHERE item_id=$1 AND LOWER(status) NOT IN('removed','blocked','rejected')),EXISTS(SELECT 1 FROM procurement_integration_sync_state WHERE channel='saby' AND resource='catalog' AND status<>'error' AND last_success_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours') AND EXISTS(SELECT 1 FROM fresh WHERE qty>0) AND (SELECT COUNT(*) FROM listed)=(SELECT COUNT(DISTINCT id) FROM fresh),(SELECT MIN(base_price_minor) FROM fresh WHERE qty>0)`, target, variant).Scan(&linked, &fresh, &minimum)
	if err != nil {
		return "", err
	}
	if !linked {
		return "Объявление больше не связано с товаром", nil
	}
	if !fresh {
		return "Нужен свежий остаток СБИС для всех размеров", nil
	}
	if minimum == nil || *minimum != minor {
		return "Минимальная доступная цена сайта изменилась", nil
	}
	return "", nil
}

func PriceContentBusy(ctx context.Context, q PriceQuery, variant int64, channel, mode string) (bool, error) {
	var busy bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2 AND execution_mode=$3 AND (status IN('prepared','queued','processing','needs_attention') OR (status NOT IN('synced','created','linked','cancelled') AND (progress->>'ambiguous'='true' OR progress->>'createAttempted'='true'))))`, variant, channel, mode).Scan(&busy)
	return busy, err
}

func GuardContentPriceConflict(ctx context.Context, q PriceQuery, variant int64) error {
	var busy bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_price_publications WHERE variant_id=$1 AND status IN('queued','processing','needs_attention'))`, variant).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("сначала дождитесь завершения подтверждённого изменения цены")
	}
	return nil
}

// PriceFormula is shared by editor saves and final pre-write guards so an
// automatic price cannot silently become a manually approved stale value.
func PriceFormula(ctx context.Context, q PriceQuery, variant int64, channel string, site int64) (int64, int64, bool, error) {
	quotes, enabled, err := procurement.UnifiedVariantPriceQuotes(ctx, q, variant)
	if err != nil {
		return 0, 0, false, err
	}
	var height, pack, logistics, returns, market, tax, reserve, strike float64
	err = q.QueryRow(ctx, `SELECT COALESCE(v.height_cm,0),s.package_rub::float8,s.marketplace_logistics_per_cm::float8,s.return_loss_rate::float8,s.marketplace_cost_rate::float8,s.tax_rate::float8,s.reserve_rate::float8,s.marketplace_strike_markup::float8 FROM product_variants v CROSS JOIN procurement_pricing_settings s WHERE v.id=$1 AND s.id=1`, variant).Scan(&height, &pack, &logistics, &returns, &market, &tax, &reserve, &strike)
	if err != nil {
		return 0, 0, false, err
	}
	wanted, compare := procurement.LegacyMarketplacePrices(site, pack, height, logistics, returns, market, tax, reserve, strike)
	if enabled {
		if quotes[channel].PriceRUB == nil {
			return 0, 0, false, nil
		}
		wanted = *quotes[channel].PriceRUB
		compare = procurement.CompareAtPrice(wanted, strike)
	}
	return wanted * 100, compare * 100, true, nil
}
