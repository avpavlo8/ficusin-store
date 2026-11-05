package catalogsync_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogsync"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func TestDurableCatalogueExportOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var actorID int64
	e = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Catalogue export test',NOW()) RETURNING id`, fmt.Sprintf("export-%d@example.invalid", unique), fmt.Sprintf("+799%09d", unique%1000000000)).Scan(&actorID)
	if e != nil {
		t.Fatal(e)
	}
	repo := admin.NewPostgresRepository(pool)
	actor := admin.Actor{CustomerID: actorID, Role: admin.RoleOwner}
	p, e := repo.CreateProduct(ctx, actor, admin.ProductCreate{Name: fmt.Sprintf("Catalogue export %d", unique), CatalogSection: "plants", PriceMinor: 149099, VariantAttributes: map[string]any{"pot_diameter_cm": 11.3, "height_cm": 40.0}})
	if e != nil {
		t.Fatal(e)
	}
	var id int64
	if e = pool.QueryRow(ctx, `SELECT id FROM product_variants WHERE product_id=$1`, p.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	remoteID := fmt.Sprintf("test-export-%d", unique)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM catalog_saby_exports WHERE variant_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, actorID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, p.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, remoteID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID)
	}()
	store := catalogsync.NewStore(pool)
	v, e := store.View(ctx, id)
	if e != nil || v.Snapshot.PotCM != 11.3 || v.Snapshot.HeightCM != 40 || v.Validation != "" {
		t.Fatalf("preview %+v %v", v, e)
	}
	if _, e = store.Enqueue(ctx, id, actorID, "stale", false); !errors.Is(e, catalogsync.ErrConflict) {
		t.Fatalf("stale preview accepted: %v", e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := store.Enqueue(ctx, id, actorID, v.Fingerprint, false); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e = range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count, rev int
	if e = pool.QueryRow(ctx, `SELECT COUNT(*),MAX(revision)::int FROM catalog_saby_exports WHERE variant_id=$1`, id).Scan(&count, &rev); e != nil || count != 1 || rev != 1 {
		t.Fatalf("duplicate commands %d %d %v", count, rev, e)
	}
	if e = repo.DeleteProductVariant(ctx, actor, id); !errors.Is(e, admin.ErrInvalidInput) {
		t.Fatalf("pending SKU deletion allowed: %v", e)
	}
	if e = repo.LinkVariantToSaby(ctx, actor, id, "unrelated"); !errors.Is(e, admin.ErrInvalidInput) {
		t.Fatalf("pending identity replacement allowed: %v", e)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM product_variants WHERE id=$1`, id); err == nil {
		t.Fatal("database allowed deletion of in-flight export identity")
	}
	c, e := store.Claim(ctx)
	if e != nil || c == nil {
		t.Fatalf("claim %+v %v", c, e)
	}
	other, e := store.Claim(ctx)
	if e != nil || other != nil {
		t.Fatalf("parallel claim %+v %v", other, e)
	}
	if e = store.Sent(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE catalog_saby_exports SET lease_until=NOW()-INTERVAL '1 second' WHERE variant_id=$1`, id); e != nil {
		t.Fatal(e)
	}
	recovered, e := store.Claim(ctx)
	if e != nil || recovered == nil || recovered.AttemptedAt == nil {
		t.Fatalf("lost durable POST boundary: %+v %v", recovered, e)
	}
	// An expired claimant must not erase the new claimant's attempted boundary.
	if e = store.Release(ctx, c, "error", "stale claimant", 0); e != nil {
		t.Fatal(e)
	}
	var state string
	if e = pool.QueryRow(ctx, `SELECT state FROM catalog_saby_exports WHERE variant_id=$1`, id).Scan(&state); e != nil || state != "verifying" {
		t.Fatalf("stale claimant changed state: %s %v", state, e)
	}
	r := &catalogsync.Remote{ID: remoteID, Code: c.Snapshot.Code, Article: c.Snapshot.Article, Name: c.Snapshot.Name, PotCM: 11.3, HeightCM: 40, PriceMinor: 149099}
	if e = store.Finish(ctx, recovered, r); e != nil {
		t.Fatal(e)
	}
	v, e = store.View(ctx, id)
	if e != nil || v.State != "synced" || v.Dirty || v.Snapshot.SabyID != remoteID {
		t.Fatalf("linked result %+v %v", v, e)
	}
	if _, e = store.Enqueue(ctx, id, actorID, v.Fingerprint, false); e != nil {
		t.Fatal(e)
	}
	other, e = store.Claim(ctx)
	if e != nil || other != nil {
		t.Fatalf("unchanged payload queued %+v %v", other, e)
	}
	if e = pool.QueryRow(ctx, `SELECT COUNT(*) FROM product_external_ids WHERE variant_id=$1 AND provider='saby' AND external_id=$2`, id, remoteID).Scan(&count); e != nil || count != 1 {
		t.Fatalf("missing canonical mapping %d %v", count, e)
	}
	var status string
	if e = pool.QueryRow(ctx, `SELECT status FROM products WHERE id=$1`, p.ID).Scan(&status); e != nil || status != "draft" {
		t.Fatal("export published the product")
	}
	// Any stale preview after a measurement edit is rejected, including before POST.
	_, e = pool.Exec(ctx, `UPDATE variant_attribute_values SET value='41' WHERE variant_id=$1 AND attribute_id=(SELECT id FROM attribute_definitions WHERE code='height_cm')`, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Enqueue(ctx, id, actorID, v.Fingerprint, false); !errors.Is(e, catalogsync.ErrConflict) {
		t.Fatalf("old dimensions accepted %v", e)
	}
	v, e = store.View(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Enqueue(ctx, id, actorID, v.Fingerprint, false); e != nil {
		t.Fatal(e)
	}
	c, e = store.Claim(ctx)
	if e != nil || c == nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=150000 WHERE id=$1`, id)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Sent(ctx, c); !errors.Is(e, catalogsync.ErrConflict) {
		t.Fatalf("changed price sent %v", e)
	}
}
