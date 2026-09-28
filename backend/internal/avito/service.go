package avito

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultBaseURL = "https://api.avito.ru"

type Product struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Slug  string  `json:"slug"`
	Image string  `json:"image"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}
type Listing struct {
	ItemID           string    `json:"itemId"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	URL              string    `json:"url"`
	RemotePrice      float64   `json:"remotePrice"`
	DesiredPublished *bool     `json:"desiredPublished"`
	Products         []Product `json:"products"`
}
type State struct {
	Configured         bool       `json:"configured"`
	PublicationEnabled bool       `json:"publicationEnabled"`
	FeedURL            string     `json:"feedUrl"`
	LastImportAt       *time.Time `json:"lastImportAt"`
	LastWorkerAt       *time.Time `json:"lastWorkerAt"`
	LastSuccessAt      *time.Time `json:"lastSuccessAt"`
	LastError          string     `json:"lastError"`
}

type Service struct {
	pool                                                    *pgxpool.Pool
	client                                                  *http.Client
	clientID, clientSecret, baseURL, siteURL                string
	address, managerName, contactPhone, category, goodsType string
	mu                                                      sync.Mutex
	token                                                   string
	tokenUntil                                              time.Time
}

func New(pool *pgxpool.Pool, clientID, clientSecret, siteURL, address, managerName, contactPhone, category, goodsType string) *Service {
	return &Service{pool: pool, client: &http.Client{Timeout: 30 * time.Second}, clientID: strings.TrimSpace(clientID), clientSecret: strings.TrimSpace(clientSecret), baseURL: defaultBaseURL, siteURL: strings.TrimRight(siteURL, "/"), address: strings.TrimSpace(address), managerName: strings.TrimSpace(managerName), contactPhone: strings.TrimSpace(contactPhone), category: strings.TrimSpace(category), goodsType: strings.TrimSpace(goodsType)}
}
func (s *Service) Configured() bool { return s != nil && s.clientID != "" && s.clientSecret != "" }

func (s *Service) tokenFor(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenUntil) {
		return s.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {s.clientID}, "client_secret": {s.clientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Avito OAuth: HTTP %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.AccessToken == "" {
		return "", errors.New("Avito OAuth не вернул access_token")
	}
	s.token = body.AccessToken
	if body.ExpiresIn <= 0 {
		body.ExpiresIn = 3600
	}
	s.tokenUntil = time.Now().Add(time.Duration(body.ExpiresIn-60) * time.Second)
	return s.token, nil
}

type apiItem struct {
	ID     json.RawMessage `json:"id"`
	Title  string          `json:"title"`
	Status string          `json:"status"`
	URL    string          `json:"url"`
	Price  json.Number     `json:"price"`
}

func itemID(raw json.RawMessage) string { return strings.Trim(strings.TrimSpace(string(raw)), `"`) }
func (s *Service) Import(ctx context.Context) (int, error) {
	if !s.Configured() {
		return 0, errors.New("ключи Avito не настроены")
	}
	token, err := s.tokenFor(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, status := range []string{"active", "removed", "old", "blocked", "rejected"} {
		for page := 1; page <= 100; page++ {
			endpoint := fmt.Sprintf("%s/core/v1/items?status=%s&page=%d&per_page=100", s.baseURL, url.QueryEscape(status), page)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := s.client.Do(req)
			if err != nil {
				return count, err
			}
			if resp.StatusCode/100 != 2 {
				resp.Body.Close()
				return count, fmt.Errorf("получить объявления Avito: HTTP %d", resp.StatusCode)
			}
			var body struct {
				Resources []apiItem `json:"resources"`
			}
			err = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if err != nil {
				return count, err
			}
			for _, item := range body.Resources {
				id := itemID(item.ID)
				if id == "" || id == "null" {
					continue
				}
				price, _ := strconv.ParseFloat(item.Price.String(), 64)
				_, err = s.pool.Exec(ctx, `INSERT INTO avito_listings(item_id,title,status,url,remote_price_minor,last_seen_at) VALUES($1,$2,$3,$4,$5,CURRENT_TIMESTAMP) ON CONFLICT(item_id) DO UPDATE SET title=EXCLUDED.title,status=EXCLUDED.status,url=EXCLUDED.url,remote_price_minor=EXCLUDED.remote_price_minor,last_seen_at=CURRENT_TIMESTAMP`, id, item.Title, item.Status, item.URL, int64(price*100))
				if err != nil {
					return count, err
				}
				count++
			}
			if len(body.Resources) < 100 {
				break
			}
		}
	}
	_, _ = s.pool.Exec(ctx, `UPDATE avito_integration_state SET last_import_at=CURRENT_TIMESTAMP,last_success_at=CURRENT_TIMESTAMP,last_error='' WHERE id=1`)
	return count, nil
}

func (s *Service) List(ctx context.Context) ([]Listing, State, error) {
	state, err := s.State(ctx)
	if err != nil {
		return nil, state, err
	}
	rows, err := s.pool.Query(ctx, `SELECT l.item_id,l.title,l.status,l.url,l.remote_price_minor,l.desired_published,p.id,p.name,p.slug,COALESCE((SELECT MIN(v.base_price_minor) FROM product_variants v JOIN inventory i ON i.variant_id=v.id WHERE v.product_id=p.id AND v.is_active<>0 AND GREATEST(i.available_qty-i.reserved_qty,0)>0),0),COALESCE((SELECT SUM(GREATEST(i.available_qty-i.reserved_qty,0)) FROM product_variants v JOIN inventory i ON i.variant_id=v.id WHERE v.product_id=p.id AND v.is_active<>0),0),COALESCE((SELECT object_key FROM product_media WHERE product_id=p.id ORDER BY is_primary DESC,sort_order,id LIMIT 1),'') FROM avito_listings l LEFT JOIN avito_listing_products lp ON lp.item_id=l.item_id LEFT JOIN products p ON p.id=lp.product_id WHERE LOWER(l.status)<>'removed' ORDER BY l.last_seen_at DESC,l.item_id,p.name`)
	if err != nil {
		return nil, state, err
	}
	defer rows.Close()
	items := []Listing{}
	index := map[string]int{}
	for rows.Next() {
		var l Listing
		var productID *int64
		var name, slug, image *string
		var remoteMinor int64
		var productMinor, stock *int64
		if err := rows.Scan(&l.ItemID, &l.Title, &l.Status, &l.URL, &remoteMinor, &l.DesiredPublished, &productID, &name, &slug, &productMinor, &stock, &image); err != nil {
			return nil, state, err
		}
		pos, ok := index[l.ItemID]
		if !ok {
			l.RemotePrice = float64(remoteMinor) / 100
			l.Products = []Product{}
			index[l.ItemID] = len(items)
			items = append(items, l)
			pos = len(items) - 1
		}
		if productID != nil {
			items[pos].Products = append(items[pos].Products, Product{ID: *productID, Name: *name, Slug: *slug, Image: *image, Price: float64(*productMinor) / 100, Stock: int(*stock)})
		}
	}
	return items, state, rows.Err()
}

func (s *Service) State(ctx context.Context) (State, error) {
	var st State
	var token string
	st.Configured = s.Configured()
	err := s.pool.QueryRow(ctx, `SELECT publication_enabled,feed_token,last_import_at,last_worker_at,last_success_at,last_error FROM avito_integration_state WHERE id=1`).Scan(&st.PublicationEnabled, &token, &st.LastImportAt, &st.LastWorkerAt, &st.LastSuccessAt, &st.LastError)
	st.FeedURL = s.siteURL + "/feeds/avito/" + token + ".xml"
	return st, err
}
func (s *Service) ReplaceProducts(ctx context.Context, itemID string, ids []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM avito_listings WHERE item_id=$1)`, itemID).Scan(&exists); err != nil || !exists {
		if err == nil {
			err = pgx.ErrNoRows
		}
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM avito_listing_products WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, itemID, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Service) SetPublication(ctx context.Context, enabled bool) error {
	if enabled && (s.address == "" || s.managerName == "" || s.contactPhone == "" || s.category == "" || s.goodsType == "") {
		return errors.New("для автопубликации заполните адрес, контакт и категорию Avito")
	}
	_, err := s.pool.Exec(ctx, `UPDATE avito_integration_state SET publication_enabled=$1 WHERE id=1`, enabled)
	return err
}

type feedAds struct {
	XMLName       xml.Name `xml:"Ads"`
	FormatVersion string   `xml:"formatVersion,attr"`
	Target        string   `xml:"target,attr"`
	Ads           []feedAd `xml:"Ad"`
}
type feedAd struct {
	ID           string     `xml:"Id"`
	AvitoID      string     `xml:"AvitoId"`
	Title        string     `xml:"Title"`
	Description  string     `xml:"Description"`
	Category     string     `xml:"Category"`
	GoodsType    string     `xml:"GoodsType"`
	Condition    string     `xml:"Condition"`
	Price        int        `xml:"Price"`
	Address      string     `xml:"Address"`
	ManagerName  string     `xml:"ManagerName"`
	ContactPhone string     `xml:"ContactPhone"`
	Images       feedImages `xml:"Images"`
}
type feedImages struct {
	Items []feedImage `xml:"Image"`
}
type feedImage struct {
	URL string `xml:"url,attr"`
}

func (s *Service) WriteFeed(ctx context.Context, token string, w http.ResponseWriter) error {
	var enabled bool
	var expected string
	if err := s.pool.QueryRow(ctx, `SELECT publication_enabled,feed_token FROM avito_integration_state WHERE id=1`).Scan(&enabled, &expected); err != nil {
		return err
	}
	if !enabled || token != expected {
		return pgx.ErrNoRows
	}
	rows, err := s.pool.Query(ctx, `SELECT l.item_id,l.title,MIN(p.description),l.remote_price_minor,MIN(COALESCE((SELECT object_key FROM product_media WHERE product_id=p.id ORDER BY is_primary DESC,sort_order,id LIMIT 1),'')) FROM avito_listings l JOIN avito_listing_products lp ON lp.item_id=l.item_id JOIN products p ON p.id=lp.product_id JOIN product_variants v ON v.product_id=p.id AND v.is_active<>0 JOIN inventory i ON i.variant_id=v.id AND GREATEST(i.available_qty-i.reserved_qty,0)>0 WHERE l.desired_published IS TRUE AND LOWER(l.status) NOT IN ('blocked','rejected') GROUP BY l.item_id,l.title,l.remote_price_minor ORDER BY l.item_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	ads := []feedAd{}
	for rows.Next() {
		var id, title, description, image string
		var price int64
		if err := rows.Scan(&id, &title, &description, &price, &image); err != nil {
			return err
		}
		ad := feedAd{ID: id, AvitoID: id, Title: title, Description: description, Category: s.category, GoodsType: s.goodsType, Condition: "Новое", Price: int((price + 50) / 100), Address: s.address, ManagerName: s.managerName, ContactPhone: s.contactPhone}
		if image != "" {
			if !strings.HasPrefix(image, "http") {
				image = s.siteURL + "/" + strings.TrimLeft(image, "/")
			}
			ad.Images.Items = []feedImage{{URL: image}}
		}
		ads = append(ads, ad)
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(xml.Header))
	return xml.NewEncoder(w).Encode(feedAds{FormatVersion: "3", Target: "Avito.ru", Ads: ads})
}

func (s *Service) UpdatePrice(ctx context.Context, itemID string, price float64) error {
	if !s.Configured() {
		return errors.New("ключи Avito не настроены")
	}
	token, err := s.tokenFor(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]int{"price": int(price + 0.5)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/core/v1/items/"+url.PathEscape(itemID)+"/update_price", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("обновить цену Avito: HTTP %d", resp.StatusCode)
	}
	_, err = s.pool.Exec(ctx, `UPDATE avito_listings SET remote_price_minor=$2,last_error='' WHERE item_id=$1`, itemID, int64(price*100))
	return err
}

// Reconcile records the stock-derived desired state every minute. Unknown or stale
// inventory never forces an ad down: only a successful site inventory snapshot does.
func (s *Service) Reconcile(ctx context.Context) error {
	var stockKnown bool
	if err := s.pool.QueryRow(ctx, `SELECT status<>'error' AND last_success_at IS NOT NULL FROM procurement_integration_sync_state WHERE channel='saby' AND resource='catalog'`).Scan(&stockKnown); err != nil {
		return err
	}
	if !stockKnown {
		_, _ = s.pool.Exec(ctx, `UPDATE avito_integration_state SET last_worker_at=CURRENT_TIMESTAMP,last_error='Остаток СБИС не подтверждён; состояние объявлений сохранено' WHERE id=1`)
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE avito_listings l SET desired_published=CASE WHEN LOWER(l.status) IN ('blocked','rejected') THEN NULL ELSE x.in_stock END,last_reconciled_at=CURRENT_TIMESTAMP,last_error='' FROM (SELECT lp.item_id,BOOL_OR(GREATEST(i.available_qty-i.reserved_qty,0)>0) in_stock FROM avito_listing_products lp JOIN product_variants v ON v.product_id=lp.product_id AND v.is_active<>0 JOIN inventory i ON i.variant_id=v.id GROUP BY lp.item_id) x WHERE x.item_id=l.item_id`)
	if err == nil {
		_, _ = s.pool.Exec(ctx, `UPDATE avito_integration_state SET last_worker_at=CURRENT_TIMESTAMP,last_success_at=CURRENT_TIMESTAMP,last_error='' WHERE id=1`)
	} else {
		_, _ = s.pool.Exec(ctx, `UPDATE avito_integration_state SET last_worker_at=CURRENT_TIMESTAMP,last_error=$1 WHERE id=1`, err.Error())
	}
	return err
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	daily := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	defer daily.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Reconcile(ctx)
		case <-daily.C:
			if s.Configured() {
				_, _ = s.Import(ctx)
			}
		}
	}
}

func (s *Service) SearchProducts(ctx context.Context, q string) ([]Product, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.name,p.slug,COALESCE(MIN(CASE WHEN GREATEST(i.available_qty-i.reserved_qty,0)>0 THEN v.base_price_minor END),0),COALESCE(SUM(GREATEST(i.available_qty-i.reserved_qty,0)),0),COALESCE((SELECT object_key FROM product_media WHERE product_id=p.id ORDER BY is_primary DESC,sort_order,id LIMIT 1),'') FROM products p JOIN product_variants v ON v.product_id=p.id AND v.is_active<>0 JOIN inventory i ON i.variant_id=v.id WHERE p.name ILIKE '%'||$1||'%' OR p.slug ILIKE '%'||$1||'%' GROUP BY p.id ORDER BY p.name LIMIT 30`, strings.TrimSpace(q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		var p Product
		var minor int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &minor, &p.Stock, &p.Image); err != nil {
			return nil, err
		}
		p.Price = float64(minor) / 100
		out = append(out, p)
	}
	return out, rows.Err()
}
