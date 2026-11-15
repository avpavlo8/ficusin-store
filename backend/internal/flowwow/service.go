// Package flowwow connects the existing catalogue publisher and site inventory
// to a single configured Flowwow shop. Credentials are never persisted.
package flowwow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	Pool   *pgxpool.Pool
	Client *integration.FlowwowClient
	Mode   string
}

func (s *Service) Configured() bool { return s.Client.Configured() }

// Match storefront availability, including freshness, warehouse and disabled SKUs.
const stockSQL = `CASE WHEN p.status='published' AND v.is_active=1 AND v.archived_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM launch_hidden_extra_plant_variants h WHERE h.variant_id=v.id)
 THEN COALESCE((SELECT SUM(GREATEST(i.available_qty-i.reserved_qty,0)) FROM order_sale_inventory i
 JOIN warehouses w ON w.id=i.warehouse_id AND w.saby_id='saby-ryazan-main' AND w.is_active=1
 JOIN saby_nomenclature n ON n.saby_id=v.saby_id AND n.missing_since IS NULL
 WHERE i.variant_id=v.id AND 'stock'=ANY(p.saby_fields) AND i.synced_at>=NOW()-INTERVAL '2 hours'),0)::int ELSE 0 END`

const SiteStockSQL = stockSQL

func (s *Service) Advance(ctx context.Context, ch string, r ce.Request, p ce.Progress) (ce.Result, error) {
	if s.Mode != "live" && s.Mode != "mock" {
		return ce.Result{}, errors.New("Flowwow: реальная публикация выключена")
	}
	if e := s.checkShop(ctx); e != nil {
		return ce.Result{}, e
	}
	conn, e := s.Pool.Acquire(ctx)
	if e != nil {
		return ce.Result{}, e
	}
	defer conn.Release()
	// Inventory and content writes for this SKU cannot overlap across processes.
	key := int64(620000000000000000) + r.VariantID
	if _, e = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); e != nil {
		return ce.Result{}, e
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.Exec(clean, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			conn.Conn().Close(clean)
		}
	}()
	if p.Stage == "" || p.Stage == "create" || p.Stage == "update" || p.Stage == "wait-stock" {
		var stock int
		if e = conn.QueryRow(ctx, `SELECT `+stockSQL+` FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, r.VariantID).Scan(&stock); e != nil {
			return ce.Result{}, e
		}
		attrs := map[string]any{}
		for k, v := range r.Channel.Attributes {
			attrs[k] = v
		}
		delete(attrs, "productionTime")
		if stock > 0 {
			attrs["stock"] = stock
		} else if p.Stage == "create" || p.Stage == "update" || p.Stage == "wait-stock" || p.Stage == "" && r.Action != "link" && r.Action != "update" {
			// A full update may require stock>=1. Never temporarily reopen a
			// sold-out listing merely to replace its description or photos.
			p.Stage = "wait-stock"
			p.CreateAttempted = false
			p.Native = nil
			return ce.Result{Progress: p, Status: "queued", Message: "Контент ожидает свежего положительного остатка. Нулевое наличие отправляется отдельно."}, nil
		}
		if p.Stage == "wait-stock" {
			p.Stage = ""
		}
		r.Channel.Attributes = attrs
	}
	// The independent inventory worker may already have applied a newer site
	// price while photos were being processed. Verify that current price instead
	// of waiting forever for an obsolete frozen amount to reappear.
	if p.Stage == "verify" {
		if e = conn.QueryRow(ctx, `SELECT base_price_minor FROM product_variants WHERE id=$1`, r.VariantID).Scan(&r.PriceMinor); e != nil {
			return ce.Result{}, e
		}
	}
	result, err := s.Client.Advance(ctx, ch, r, p)
	if err == nil && result.Progress.Stage == "verify" && (p.Stage == "create" || p.Stage == "update") {
		_, err = conn.Exec(ctx, `UPDATE flowwow_inventory_sync SET price_minor=NULL,next_attempt_at=NOW() WHERE variant_id=$1`, r.VariantID)
	}
	return result, err
}
func (s *Service) Reference(ctx context.Context, kind, category string) (json.RawMessage, error) {
	return s.Client.Reference(ctx, kind, category)
}
func (s *Service) Remote(ctx context.Context, page int) (any, error) {
	if page < 0 {
		return nil, errors.New("неверная страница")
	}
	items, total, e := s.Client.Products(ctx, map[string]any{"extended": true, "withArchive": false, "page": page, "limit": 50})
	return map[string]any{"items": items, "total": total}, e
}

type Item struct {
	VariantID         int64      `json:"variantId"`
	ProductID         int64      `json:"productId"`
	Name              string     `json:"name"`
	SKU               string     `json:"sku"`
	PriceMinor        int64      `json:"priceMinor"`
	Stock             int        `json:"stock"`
	RemoteID          string     `json:"remoteId"`
	OfferID           string     `json:"offerId"`
	Status            string     `json:"status"`
	Message           string     `json:"message"`
	LastError         string     `json:"lastError"`
	ContentUpdatedAt  time.Time  `json:"contentUpdatedAt"`
	InventorySyncedAt *time.Time `json:"inventorySyncedAt"`
	InventoryPrice    *int64     `json:"inventoryPriceMinor"`
	InventoryStock    *int       `json:"inventoryStock"`
}

func (s *Service) List(ctx context.Context) (any, error) {
	rows, e := s.Pool.Query(ctx, `SELECT v.id,p.id,p.name,v.sku,v.base_price_minor,`+stockSQL+`,COALESCE(l.remote_id,''),COALESCE(l.offer_id,''),pub.status,pub.message,COALESCE(sync.last_error,''),pub.updated_at,sync.synced_at,sync.price_minor,sync.stock FROM catalog_editor_publications pub JOIN product_variants v ON v.id=pub.variant_id JOIN products p ON p.id=v.product_id LEFT JOIN catalog_editor_remote_links l ON l.variant_id=v.id AND l.channel='flowwow' AND l.execution_mode=pub.execution_mode LEFT JOIN flowwow_inventory_sync sync ON sync.variant_id=v.id WHERE pub.channel='flowwow' AND pub.execution_mode=$1 ORDER BY p.name,v.id LIMIT 1000`, s.Mode)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var i Item
		if e = rows.Scan(&i.VariantID, &i.ProductID, &i.Name, &i.SKU, &i.PriceMinor, &i.Stock, &i.RemoteID, &i.OfferID, &i.Status, &i.Message, &i.LastError, &i.ContentUpdatedAt, &i.InventorySyncedAt, &i.InventoryPrice, &i.InventoryStock); e != nil {
			return nil, e
		}
		items = append(items, i)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	preview, e := s.LinkPreview(ctx)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": items, "configured": s.Configured(), "publicationMode": s.Mode, "shopId": s.Client.ShopID(), "preview": preview}, nil
}
func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	if (s.Mode != "live" && s.Mode != "mock") || !s.Configured() {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e := s.InventoryStep(ctx); e != nil && ctx.Err() == nil {
			logger.Error("Flowwow inventory sync", "error", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) InventoryStep(ctx context.Context) error {
	if (s.Mode != "live" && s.Mode != "mock") || !s.Configured() {
		return nil
	}
	if e := s.checkShop(ctx); e != nil {
		return e
	}
	conn, e := s.Pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	// Only existing links in the current execution mode qualify. Content errors do not block availability.
	rows, e := conn.Query(ctx, `SELECT v.id,v.base_price_minor,`+stockSQL+`,l.offer_id FROM catalog_editor_remote_links l JOIN product_variants v ON v.id=l.variant_id JOIN products p ON p.id=v.product_id JOIN catalog_editor_publications pub ON pub.variant_id=v.id AND pub.channel='flowwow' AND pub.execution_mode=$1  LEFT JOIN flowwow_inventory_sync sync ON sync.variant_id=v.id WHERE l.channel='flowwow' AND l.execution_mode=$1 AND (sync.next_attempt_at IS NULL OR sync.next_attempt_at<=NOW()) ORDER BY v.id`, s.Mode)
	if e != nil {
		return e
	}
	type snapshot struct {
		id, price int64
		stock     int
		offer     string
	}
	list := []snapshot{}
	for rows.Next() {
		var x snapshot
		if e = rows.Scan(&x.id, &x.price, &x.stock, &x.offer); e != nil {
			rows.Close()
			return e
		}
		list = append(list, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, x := range list {
		var locked bool
		key := int64(620000000000000000) + x.id
		if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&locked); e != nil {
			return e
		}
		if !locked {
			continue
		}
		operationErr := func() error {
			defer func() {
				clean, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if _, err := conn.Exec(clean, `SELECT pg_advisory_unlock($1)`, key); err != nil {
					conn.Conn().Close(clean)
				}
			}()
			err := func() error {
				// Re-read under the same lock used by the content publisher.
				if e := conn.QueryRow(ctx, `SELECT v.base_price_minor,`+stockSQL+` FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, x.id).Scan(&x.price, &x.stock); e != nil {
					return e
				}
				var same bool
				if e := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM flowwow_inventory_sync WHERE variant_id=$1 AND price_minor=$2 AND stock=$3 AND offer_id=$4 AND shop_id=$5 AND last_error='')`, x.id, x.price, x.stock, x.offer, s.Client.ShopID()).Scan(&same); e != nil {
					return e
				}
				if same {
					return nil
				}
				if e := s.Client.SyncInventory(ctx, x.offer, x.price, x.stock); e != nil {
					return e
				}
				_, e := conn.Exec(ctx, `INSERT INTO flowwow_inventory_sync(variant_id,price_minor,stock,offer_id,shop_id,synced_at) VALUES($1,$2,$3,$4,$5,NOW()) ON CONFLICT(variant_id) DO UPDATE SET price_minor=$2,stock=$3,offer_id=$4,shop_id=$5,last_error='',next_attempt_at=NOW(),synced_at=NOW()`, x.id, x.price, x.stock, x.offer, s.Client.ShopID())
				return e
			}()
			if err != nil {
				if _, e = conn.Exec(ctx, `INSERT INTO flowwow_inventory_sync(variant_id,shop_id,last_error,next_attempt_at) VALUES($1,$2,$3,NOW()+INTERVAL '1 minute') ON CONFLICT(variant_id) DO UPDATE SET last_error=$3,next_attempt_at=NOW()+INTERVAL '1 minute'`, x.id, s.Client.ShopID(), err.Error()); e != nil {
					return e
				}
			}
			return nil
		}()
		if operationErr != nil {
			return operationErr
		}
	}
	return nil
}

func (s *Service) checkShop(ctx context.Context) error {
	if s.Mode != "live" {
		return nil
	}
	if !s.Configured() {
		return errors.New("Flowwow API не подключён")
	}
	if _, e := s.Pool.Exec(ctx, `INSERT INTO flowwow_shop_binding(singleton,shop_id) VALUES(TRUE,$1) ON CONFLICT DO NOTHING`, s.Client.ShopID()); e != nil {
		return e
	}
	var shop string
	if e := s.Pool.QueryRow(ctx, `SELECT shop_id FROM flowwow_shop_binding WHERE singleton`).Scan(&shop); e != nil {
		return e
	}
	if shop != s.Client.ShopID() {
		return errors.New("FLOWWOW_SHOP_ID изменён: существующие связи относятся к другому магазину. Проверьте настройку перед обменом")
	}
	return nil
}

type Candidate struct {
	VariantID  int64  `json:"variantId"`
	ProductID  int64  `json:"productId"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	SKU        string `json:"sku"`
	PriceMinor int64  `json:"priceMinor"`
	Stock      int    `json:"stock"`
}

func (s *Service) Candidates(ctx context.Context, query string) ([]Candidate, error) {
	if len([]rune(query)) > 128 {
		return nil, fmt.Errorf("Запрос слишком длинный")
	}
	rows, e := s.Pool.Query(ctx, `SELECT v.id,p.id,p.name,v.label,v.sku,v.base_price_minor,`+stockSQL+` FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.archived_at IS NULL AND (p.name ILIKE '%'||$1||'%' OR v.sku=$1) AND NOT EXISTS(SELECT 1 FROM catalog_editor_remote_links l WHERE l.variant_id=v.id AND l.channel='flowwow' AND l.execution_mode=$2) ORDER BY p.name,v.id LIMIT 50`, query, s.Mode)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var i Candidate
		if e = rows.Scan(&i.VariantID, &i.ProductID, &i.Name, &i.Label, &i.SKU, &i.PriceMinor, &i.Stock); e != nil {
			return nil, e
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (s *Service) Resolve(ctx context.Context, id int64) (integration.FlowwowProduct, string, error) {
	if id <= 0 || (s.Mode != "mock" && s.Mode != "live") || !s.Configured() {
		return integration.FlowwowProduct{}, "", fmt.Errorf("Flowwow не подключён для отправки")
	}
	cards, _, e := s.Client.Products(ctx, map[string]any{"productIds": []int64{id}, "extended": true, "withArchive": false, "limit": 2})
	if e != nil {
		return integration.FlowwowProduct{}, "", e
	}
	if len(cards) != 1 || cards[0].ProductID != id || cards[0].Type != 1 {
		return integration.FlowwowProduct{}, "", fmt.Errorf("Обычная карточка Flowwow %s не найдена", strconv.FormatInt(id, 10))
	}
	return cards[0], s.Client.ShopID(), nil
}
