package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5"
)

var ErrPriceChannelUnavailable = fmt.Errorf("канал изменения цены не подключён")

func stageEditorPriceProposals(ctx context.Context, tx pgx.Tx, actor Actor, variantID, newMinor int64) error {
	var productID int64
	var sabyID, sabyCode, wbID, ozonID string
	var marketplaceScope string
	var height float64
	var packageRUB, logisticsRate, returnRate, marketplaceRate, taxRate, reserveRate, strikeRate float64
	err := tx.QueryRow(ctx, `SELECT v.product_id,COALESCE(v.saby_id,''),COALESCE(n.code,''),v.marketplace_scope,
		COALESCE(directory.wb_nm_ids[1],''),COALESCE(directory.ozon_articles[1],''),
		COALESCE(v.height_cm,0),s.package_rub::float8,s.marketplace_logistics_per_cm::float8,
		s.return_loss_rate::float8,s.marketplace_cost_rate::float8,s.tax_rate::float8,s.reserve_rate::float8,s.marketplace_strike_markup::float8
		FROM product_variants v
		LEFT JOIN saby_nomenclature n ON n.saby_id=v.saby_id
		LEFT JOIN canonical_product_directory directory ON directory.variant_id=v.id
		CROSS JOIN procurement_pricing_settings s
		WHERE v.id=$1 AND s.id=1`, variantID).Scan(&productID, &sabyID, &sabyCode, &marketplaceScope, &wbID, &ozonID, &height,
		&packageRUB, &logisticsRate, &returnRate, &marketplaceRate, &taxRate, &reserveRate, &strikeRate)
	if err != nil {
		return fmt.Errorf("load editor price channels: %w", err)
	}
	marketPrice, strikePrice := procurement.LegacyMarketplacePrices(newMinor, packageRUB, height, logisticsRate, returnRate, marketplaceRate, taxRate, reserveRate, strikeRate)
	market, strike := float64(marketPrice), float64(strikePrice)
	unifiedQuotes, unified, err := procurement.UnifiedVariantPriceQuotes(ctx, tx, variantID)
	if err != nil {
		return err
	}
	for _, channel := range []struct {
		name, article string
		price         float64
		compare       *float64
	}{
		{"wb", wbID, market, &strike}, {"ozon", ozonID, market, &strike},
	} {
		if marketplaceScope == "site_only" {
			continue
		}
		if unified {
			q := unifiedQuotes[channel.name]
			if q.PriceRUB == nil {
				continue
			}
			channel.price = float64(*q.PriceRUB)
			compare := float64(procurement.CompareAtPrice(*q.PriceRUB, strikeRate))
			channel.compare = &compare
		}
		if channel.article != "" {
			var compare *int64
			if channel.compare != nil {
				value := money.Kopecks(*channel.compare)
				compare = &value
			}
			if err = stageEditorPrice(ctx, tx, actor, productID, variantID, channel.name, channel.article, newMinor, money.Kopecks(channel.price), compare, "formula"); err != nil {
				return err
			}
		}
	}
	if sabyID != "" && sabyCode != "" {
		if err = stageEditorPrice(ctx, tx, actor, productID, variantID, "saby", sabyCode, newMinor, newMinor, nil, "explicit"); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, `WITH fresh_stock AS (
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
					CASE WHEN variant.id=$3 THEN $2::numeric/100 ELSE variant.base_price_minor::numeric/100 END
				END) new_price
		FROM avito_listing_products target
		JOIN avito_listings listing ON listing.item_id=target.item_id
		JOIN avito_listing_products lp ON lp.item_id=listing.item_id
		JOIN product_variants variant ON variant.product_id=lp.product_id AND variant.is_active<>0
			AND variant.archived_at IS NULL
		LEFT JOIN fresh_stock stock ON stock.variant_id=variant.id
		WHERE target.product_id=$1
		GROUP BY listing.item_id,listing.remote_price_minor
		HAVING COUNT(*)=COUNT(stock.variant_id)
		)
 SELECT item_id,new_price FROM prices WHERE new_price IS NOT NULL`, productID, newMinor, variantID)
	if err != nil {
		return err
	}
	type listingPrice struct {
		id    string
		price float64
	}
	prices := []listingPrice{}
	for rows.Next() {
		var p listingPrice
		if err = rows.Scan(&p.id, &p.price); err != nil {
			rows.Close()
			return err
		}
		prices = append(prices, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range prices {
		if err = stageEditorPrice(ctx, tx, actor, productID, variantID, "avito", p.id, newMinor, money.Kopecks(p.price), nil, "explicit"); err != nil {
			return err
		}
	}
	return nil
}

// Draft creation never approves or sends a price, even when live publishing is enabled.
func stageEditorPrice(ctx context.Context, tx pgx.Tx, actor Actor, product, variant int64, channel, target string, site, price int64, compare *int64, origin string) error {
	if target == "" || price <= 0 {
		return nil
	}
	var sabyID string
	if channel == "saby" {
		if err := tx.QueryRow(ctx, "SELECT COALESCE(saby_id,'') FROM product_variants WHERE id=$1", variant).Scan(&sabyID); err != nil {
			return err
		}
	}
	r := ce.Request{Action: "price", ProductID: product, VariantID: variant, PriceMinor: site, SabyID: sabyID, Price: &ce.PriceRequest{TargetID: target, PriceMinor: price, CompareAtMinor: compare}}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	// Never overwrite a submitted command or native content receipt. Repeated
	// saves only replace unapproved intentions for this remote target.
	_, err = tx.Exec(ctx, `UPDATE catalog_editor_price_publications SET status='superseded',message='Заменено новым сохранением',updated_at=NOW() WHERE channel=$1 AND target_id=$2 AND status='draft' AND (fingerprint<>encode(sha256(convert_to($3::jsonb::text,'UTF8')),'hex') OR price_origin<>$4)`, channel, target, raw, origin)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO catalog_editor_price_publications(product_id,variant_id,channel,target_id,expected_site_price_minor,price_minor,compare_at_minor,payload,fingerprint,price_origin,created_by) SELECT $1,$2,$3,$4,$5,$6,$7,$8,encode(sha256(convert_to($8::jsonb::text,'UTF8')),'hex'),$9,$10 WHERE NOT EXISTS(SELECT 1 FROM catalog_editor_price_publications WHERE variant_id=$2 AND channel=$3 AND target_id=$4 AND fingerprint=encode(sha256(convert_to($8::jsonb::text,'UTF8')),'hex') AND price_origin=$9 AND status NOT IN('superseded','failed'))`, product, variant, channel, target, site, price, compare, raw, origin, actor.CustomerID)
	return err
}

type EditorPricePublication struct {
	ID                     int64     `json:"id"`
	VariantID              int64     `json:"variantId"`
	Channel                string    `json:"channel"`
	TargetID               string    `json:"targetId"`
	PriceMinor             int64     `json:"priceMinor"`
	CompareAtMinor         *int64    `json:"compareAtMinor"`
	ExpectedSitePriceMinor int64     `json:"expectedSitePriceMinor"`
	Status                 string    `json:"status"`
	Message                string    `json:"message"`
	Fingerprint            string    `json:"fingerprint"`
	ExecutionMode          string    `json:"executionMode"`
	CanApprove             bool      `json:"canApprove"`
	CanRetry               bool      `json:"canRetry"`
	BlockReason            string    `json:"blockReason"`
	CreatedAt              time.Time `json:"createdAt"`
}

func (r *PostgresRepository) EditorPrices(ctx context.Context, product int64) ([]EditorPricePublication, error) {
	return r.editorPriceRows(ctx, product, 0)
}

func (r *PostgresRepository) editorPriceRows(ctx context.Context, product, id int64) ([]EditorPricePublication, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,variant_id,channel,target_id,price_minor,compare_at_minor,expected_site_price_minor,status,message,fingerprint,execution_mode,created_at,payload,price_origin,progress FROM catalog_editor_price_publications WHERE variant_id IN(SELECT id FROM product_variants WHERE product_id=$1) AND status<>'superseded' AND ($2::bigint=0 OR id=$2) ORDER BY created_at DESC,id DESC`, product, id)
	if err != nil {
		return nil, err
	}
	type row struct {
		p        EditorPricePublication
		raw      []byte
		origin   string
		progress ce.Progress
	}
	result := []row{}
	for rows.Next() {
		var x row
		if err = rows.Scan(&x.p.ID, &x.p.VariantID, &x.p.Channel, &x.p.TargetID, &x.p.PriceMinor, &x.p.CompareAtMinor, &x.p.ExpectedSitePriceMinor, &x.p.Status, &x.p.Message, &x.p.Fingerprint, &x.p.ExecutionMode, &x.p.CreatedAt, &x.raw, &x.origin, &x.progress); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []EditorPricePublication{}
	for _, x := range result {
		var req ce.Request
		if err = json.Unmarshal(x.raw, &req); err != nil {
			return nil, err
		}
		reason, e := ce.PriceSnapshotCurrent(ctx, r.pool, req, x.p.Channel, r.editorPublicationMode, x.origin)
		if e != nil {
			return nil, e
		}
		busy, e := ce.PriceContentBusy(ctx, r.pool, x.p.VariantID, x.p.Channel, r.editorPublicationMode)
		if e != nil {
			return nil, e
		}
		if busy {
			reason = "Дождитесь публикации карточки"
		}
		if r.editorPublicationMode != "live" && r.editorPublicationMode != "mock" {
			reason = "Публикация цен выключена"
		}
		if r.editorPublicationMode == "live" && r.priceChannelsConfigured != nil && !r.priceChannelsConfigured[x.p.Channel] {
			reason = "API канала не подключён"
		}
		x.p.BlockReason = reason
		x.p.CanApprove = x.p.Status == "draft" && reason == ""
		x.p.CanRetry = x.p.Status == "failed" && reason == "" && !x.progress.Ambiguous && (x.progress.Stage == "" || x.progress.Stage == "price-rejected")
		if x.p.Status == "needs_attention" {
			available := r.editorPublicationMode == "mock" || r.editorPublicationMode == "live" && (r.priceChannelsConfigured == nil || r.priceChannelsConfigured[x.p.Channel])
			x.p.CanRetry = available && x.p.ExecutionMode == r.editorPublicationMode
			if x.p.CanRetry {
				x.p.BlockReason = ""
			}
		}
		out = append(out, x.p)
	}
	return out, nil
}
func (r *PostgresRepository) ApproveEditorPrice(ctx context.Context, a Actor, product, id int64, fingerprint string) (EditorPricePublication, error) {
	return r.queueEditorPrice(ctx, a, product, id, fingerprint, false)
}
func (r *PostgresRepository) RetryEditorPrice(ctx context.Context, a Actor, product, id int64, fingerprint string) (EditorPricePublication, error) {
	return r.queueEditorPrice(ctx, a, product, id, fingerprint, true)
}
func (r *PostgresRepository) queueEditorPrice(ctx context.Context, a Actor, product, id int64, fp string, retry bool) (EditorPricePublication, error) {
	if !Can(a.Role, PermissionProductsManage) {
		return EditorPricePublication{}, ErrForbidden
	}
	mode := r.editorPublicationMode
	if mode != "live" && mode != "mock" {
		return EditorPricePublication{}, ErrPriceChannelUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return EditorPricePublication{}, err
	}
	defer tx.Rollback(ctx)
	var locked int64
	if err = tx.QueryRow(ctx, "SELECT id FROM products WHERE id=$1 FOR UPDATE", product).Scan(&locked); err != nil {
		return EditorPricePublication{}, err
	}
	var variant int64
	if err = tx.QueryRow(ctx, "SELECT variant_id FROM catalog_editor_price_publications WHERE id=$1 AND product_id=$2", id, product).Scan(&variant); err != nil {
		return EditorPricePublication{}, err
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE", variant, product).Scan(&locked); err != nil {
		return EditorPricePublication{}, err
	}
	var raw []byte
	var channel, status, hash, currentHash, origin, target, storedMode string
	var progress ce.Progress
	if err = tx.QueryRow(ctx, "SELECT payload,channel,status,fingerprint,encode(sha256(convert_to(payload::text,'UTF8')),'hex'),price_origin,target_id,progress,execution_mode FROM catalog_editor_price_publications WHERE id=$1 FOR UPDATE", id).Scan(&raw, &channel, &status, &hash, &currentHash, &origin, &target, &progress, &storedMode); err != nil {
		return EditorPricePublication{}, err
	}
	if mode == "live" && r.priceChannelsConfigured != nil && !r.priceChannelsConfigured[channel] {
		return EditorPricePublication{}, ErrPriceChannelUnavailable
	}
	reconcile := retry && status == "needs_attention"
	if hash != fp || hash != currentHash || reconcile && storedMode != mode || !retry && status != "draft" || retry && !reconcile && (status != "failed" || progress.Ambiguous || progress.Stage != "" && progress.Stage != "price-rejected") {
		return EditorPricePublication{}, pgx.ErrNoRows
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", mode+":"+channel+":"+target); err != nil {
		return EditorPricePublication{}, err
	}
	var competing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_price_publications WHERE id<>$1 AND channel=$2 AND target_id=$3 AND execution_mode=$4 AND status IN('queued','processing','needs_attention'))`, id, channel, target, mode).Scan(&competing); err != nil {
		return EditorPricePublication{}, err
	}
	if competing {
		return EditorPricePublication{}, fmt.Errorf("%w: предыдущая цена ещё проверяется", ErrInvalidInput)
	}
	var req ce.Request
	if err = json.Unmarshal(raw, &req); err != nil {
		return EditorPricePublication{}, err
	}
	if !reconcile {
		reason, err := ce.PriceSnapshotCurrent(ctx, tx, req, channel, mode, origin)
		if err != nil {
			return EditorPricePublication{}, err
		}
		if reason != "" {
			return EditorPricePublication{}, pgx.ErrNoRows
		}
		busy, err := ce.PriceContentBusy(ctx, tx, variant, channel, mode)
		if err != nil {
			return EditorPricePublication{}, err
		}
		if busy {
			return EditorPricePublication{}, fmt.Errorf("%w: дождитесь публикации карточки", ErrInvalidInput)
		}
	}
	if reconcile {
		progress.Stage = "price-verify"
		progress.Ambiguous = true
		if _, err = tx.Exec(ctx, `UPDATE catalog_editor_price_publications SET status='queued',progress=$2,next_attempt_at=NOW(),lease_until=NULL,claim_token='',updated_at=NOW() WHERE id=$1`, id, progress); err != nil {
			return EditorPricePublication{}, err
		}
	} else if _, err = tx.Exec(ctx, `UPDATE catalog_editor_price_publications SET status='queued',execution_mode=$2,approved_by=$3,approved_at=NOW(),attempts=0,progress='{}',next_attempt_at=NOW(),lease_until=NULL,claim_token='',message='',updated_at=NOW() WHERE id=$1`, id, mode, a.CustomerID); err != nil {
		return EditorPricePublication{}, err
	}
	if err = insertAudit(ctx, tx, a, "editor.price.approve", "editor_price", fmt.Sprint(id), nil, map[string]any{"fingerprint": fp, "channel": channel, "targetId": target, "retry": retry}); err != nil {
		return EditorPricePublication{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EditorPricePublication{}, err
	}
	items, err := r.editorPriceRows(ctx, product, id)
	if err != nil {
		return EditorPricePublication{}, err
	}
	for _, p := range items {
		if p.ID == id {
			return p, nil
		}
	}
	return EditorPricePublication{}, pgx.ErrNoRows
}

func (r *PostgresRepository) EditorPricePublicationMode() string { return r.editorPublicationMode }
