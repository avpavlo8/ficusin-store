package avito

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAvitoAutoloadPricesAndManualStopsOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	listingID := fmt.Sprintf("autoload-%d", unique)
	var productID, warehouseID int64
	var enabled bool
	var oldSyncStatus string
	var oldSuccess *time.Time
	if err = pool.QueryRow(ctx, `SELECT publication_enabled FROM avito_integration_state WHERE id=1`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO procurement_integration_sync_state(channel,resource,status) VALUES('saby','catalog','ok') ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status,last_success_at FROM procurement_integration_sync_state WHERE channel='saby' AND resource='catalog'`).Scan(&oldSyncStatus, &oldSuccess); err != nil {
		t.Fatal(err)
	}
	sabyIDs := []string{}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM avito_listings WHERE item_id=$1`, listingID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		for _, id := range sabyIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, id)
		}
		_, _ = pool.Exec(ctx, `UPDATE avito_integration_state SET publication_enabled=$1 WHERE id=1`, enabled)
		_, _ = pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET status=$1,last_success_at=$2 WHERE channel='saby' AND resource='catalog'`, oldSyncStatus, oldSuccess)
	}()
	if err = pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1 LIMIT 1`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields,description) VALUES('Autoload plant',$1,'published',ARRAY['stock'],'Plant description') RETURNING id`, listingID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	for i, v := range []struct {
		label string
		price int
		qty   int
	}{{"D12", 100000, 2}, {"D17", 150000, 1}, {"D6 unavailable", 50000, 0}} {
		sabyID := fmt.Sprintf("%s-%d", listingID, i)
		sabyIDs = append(sabyIDs, sabyID)
		if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$1,'Autoload test')`, sabyID); err != nil {
			t.Fatal(err)
		}
		var variantID int64
		if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active) VALUES($1,$2,$3,$4,$5,1) RETURNING id`, productID, sabyID, fmt.Sprintf("7%017d", unique%10000000000000000+int64(i)), v.label, v.price).Scan(&variantID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,synced_at) VALUES($1,$2,$3,CURRENT_TIMESTAMP)`, warehouseID, variantID, v.qty); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listings(item_id,title,status,remote_price_minor) VALUES($1,'Autoload plant','active',999900)`, listingID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, listingID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE avito_integration_state SET publication_enabled=TRUE WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_integration_sync_state SET status='ok',last_success_at=CURRENT_TIMESTAMP WHERE channel='saby' AND resource='catalog'`); err != nil {
		t.Fatal(err)
	}
	remoteStatus := "active"
	apiUnavailable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiUnavailable {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"test","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/core/v1/items" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		items := []map[string]any{}
		if r.URL.Query().Get("status") == remoteStatus {
			items = append(items, map[string]any{"id": listingID, "title": "Autoload plant", "status": remoteStatus, "price": 9999})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"resources": items})
	}))
	defer server.Close()
	s := New(pool, "client", "secret", "https://ficusin.ru", "address", "manager", "phone", "category", "goods")
	s.baseURL = server.URL
	s.client = server.Client()
	state, err := s.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(state.FeedURL, "https://ficusin.ru/feeds/avito/"), ".xml")
	feed := func() []feedAd {
		t.Helper()
		w := httptest.NewRecorder()
		if err := s.WriteFeed(ctx, token, w); err != nil {
			t.Fatal(err)
		}
		var data feedAds
		if err := xml.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data.Ads
	}
	ads := feed()
	if len(ads) != 1 || ads[0].Price != 1000 || !strings.Contains(ads[0].Description, "D12 — 1000 ₽") || !strings.Contains(ads[0].Description, "D17 — 1500 ₽") || strings.Contains(ads[0].Description, "D6 unavailable") {
		t.Fatalf("wrong feed: %+v", ads)
	}
	// A site price edit is reflected without updating the old remote price mirror.
	if _, err = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=120000 WHERE product_id=$1 AND label='D12'`, productID); err != nil {
		t.Fatal(err)
	}
	if ads = feed(); len(ads) != 1 || ads[0].Price != 1200 {
		t.Fatalf("price edit not reflected: %+v", ads)
	}
	// An archive in Avito is read before the next response and stays paused.
	remoteStatus = "old"
	if ads = feed(); len(ads) != 0 {
		t.Fatalf("manual archive was restored: %+v", ads)
	}
	var paused bool
	if err = pool.QueryRow(ctx, `SELECT manual_paused FROM avito_listings WHERE item_id=$1`, listingID).Scan(&paused); err != nil || !paused {
		t.Fatalf("manual pause missing: %v %v", paused, err)
	}
	if err = s.SetPaused(ctx, listingID, false); err != nil {
		t.Fatal(err)
	}
	if ads = feed(); len(ads) != 1 {
		t.Fatalf("explicit resume failed: %+v", ads)
	}
	// An explicit panel pause survives an active remote status and repeated feeds.
	remoteStatus = "active"
	if err = s.SetPaused(ctx, listingID, true); err != nil {
		t.Fatal(err)
	}
	if ads = feed(); len(ads) != 0 {
		t.Fatalf("explicit pause was undone: %+v", ads)
	}
	if err = s.SetPaused(ctx, listingID, false); err != nil {
		t.Fatal(err)
	}
	if ads = feed(); len(ads) != 1 {
		t.Fatalf("explicit panel resume failed: %+v", ads)
	}
	// A stock-driven archive can restore on stock return; it is not a manual pause.
	remoteStatus = "active"
	_ = feed()
	if _, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=0 WHERE variant_id IN (SELECT id FROM product_variants WHERE product_id=$1)`, productID); err != nil {
		t.Fatal(err)
	}
	if ads = feed(); len(ads) != 0 {
		t.Fatalf("zero stock still advertised: %+v", ads)
	}
	remoteStatus = "old"
	_ = feed()
	if _, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=2 WHERE variant_id IN (SELECT id FROM product_variants WHERE product_id=$1 AND label='D12')`, productID); err != nil {
		t.Fatal(err)
	}
	if ads = feed(); len(ads) != 1 {
		t.Fatalf("stock return failed: %+v", ads)
	}
	// A partly stale stock snapshot cannot be emitted as an empty/partial feed.
	if _, err = pool.Exec(ctx, `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP-INTERVAL '3 hours' WHERE variant_id IN (SELECT id FROM product_variants WHERE product_id=$1 AND label='D17')`, productID); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err = s.WriteFeed(ctx, token, w); err == nil || w.Body.Len() != 0 {
		t.Fatalf("stale snapshot emitted: %v %s", err, w.Body.String())
	}
	apiUnavailable = true
	w = httptest.NewRecorder()
	if err = s.WriteFeed(ctx, token, w); err == nil || w.Body.Len() != 0 {
		t.Fatalf("remote status failure emitted feed: %v", err)
	}
}
