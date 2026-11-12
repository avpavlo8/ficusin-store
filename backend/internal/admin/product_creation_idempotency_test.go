package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type createCommitTraceKey struct{}
type cancelCreateResponseTracer struct {
	armed  atomic.Bool
	cancel context.CancelFunc
}

func (t *cancelCreateResponseTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if t.armed.Load() && strings.EqualFold(data.SQL, "commit") {
		return context.WithValue(ctx, createCommitTraceKey{}, true)
	}
	return ctx
}
func (t *cancelCreateResponseTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(createCommitTraceKey{}) == true && data.Err == nil && t.armed.Swap(false) {
		t.cancel()
	}
}

func TestProductCreationIDRejectsMalformedKeysBeforeWriting(t *testing.T) {
	r := NewPostgresRepository(nil)
	for _, key := range []string{"short", "../../anything", "00000000-0000-0000-0000-00000000000g"} {
		if _, err := r.CreateProduct(context.Background(), Actor{CustomerID: 1, Role: RoleOwner}, ProductCreate{CreationID: key, Name: "Test"}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key=%q error=%v", key, err)
		}
	}
	if _, err := r.CreateProduct(context.Background(), Actor{Role: RoleOwner}, ProductCreate{CreationID: "00000000-0000-0000-0000-000000000001", Name: "Test"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unscoped key accepted", err)
	}
}

func TestProductCreationReceiptSurvivesLostResponseOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	tracer := &cancelCreateResponseTracer{}
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewPostgresRepository(pool)
	tag := fmt.Sprint(time.Now().UnixNano())
	actors := []Actor{}
	for i := 0; i < 2; i++ {
		var id int64
		if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Creation retry test',NOW()) RETURNING id`, fmt.Sprintf("creation-%s-%d@example.invalid", tag, i), fmt.Sprintf("+7%s%d", tag[len(tag)-9:], i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		actors = append(actors, Actor{CustomerID: id, Role: RoleOwner})
	}
	defer func() {
		for _, a := range actors {
			pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_customer_id=$1`, a.CustomerID)
			pool.Exec(ctx, `DELETE FROM products WHERE id IN(SELECT product_id FROM catalog_product_creation_requests WHERE actor_customer_id=$1)`, a.CustomerID)
			pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, a.CustomerID)
		}
	}()
	input := ProductCreate{CreationID: "00000000-0000-4000-8000-000000000001", Name: "Lost response " + tag, CatalogSection: "plants", PriceMinor: 149099}
	// Cancel only after PostgreSQL confirms COMMIT, before productByID can return
	// the response. The caller sees failure even though the creation committed.
	lostCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tracer.cancel = cancel
	tracer.armed.Store(true)
	if _, err = repo.CreateProduct(lostCtx, actors[0], input); err == nil {
		t.Fatal("expected a lost post-commit response")
	}
	var committed int64
	if err = pool.QueryRow(ctx, `SELECT product_id FROM catalog_product_creation_requests WHERE actor_customer_id=$1 AND creation_id=$2`, actors[0].CustomerID, input.CreationID).Scan(&committed); err != nil {
		t.Fatal("receipt was not atomic with the committed product", err)
	}
	// Concurrent retries, even with a changed draft, return the original identity
	// and do not replay the initial price/name/variant writes.
	retry := input
	retry.Name = "Do not overwrite"
	retry.PriceMinor = 999999
	var wg sync.WaitGroup
	results := make(chan Product, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := repo.CreateProduct(ctx, actors[0], retry)
			if e != nil {
				errs <- e
			} else {
				results <- p
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	for p := range results {
		if p.ID != committed || p.Name != input.Name || p.Price != 1490.99 {
			t.Fatalf("retry created or rewrote a product: %+v", p)
		}
	}
	var variants, audits int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM product_variants WHERE product_id=$1`, committed).Scan(&variants); err != nil || variants != 1 {
		t.Fatalf("duplicate SKU: %d %v", variants, err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_log WHERE actor_customer_id=$1 AND action='product.create'`, actors[0].CustomerID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("creation repeated: %d %v", audits, err)
	}
	// The same public key under another administrator cannot disclose the first
	// administrator's response; permissions are checked before receipt lookup.
	other, err := repo.CreateProduct(ctx, actors[1], input)
	if err != nil || other.ID == committed {
		t.Fatalf("actor scope leaked: %+v %v", other, err)
	}
	if _, err = repo.CreateProduct(ctx, Actor{CustomerID: actors[0].CustomerID, Role: "customer"}, input); !errors.Is(err, ErrForbidden) {
		t.Fatal("receipt bypassed authorization", err)
	}
	// Merging two independently created drafts retargets both receipts. A
	// retry of the source must return the surviving card, not recreate it.
	for _, productID := range []int64{committed, other.ID} {
		if _, err = pool.Exec(ctx, `INSERT INTO catalog_editor_plans(product_id,config) SELECT $1,jsonb_build_object('variants',jsonb_build_array(jsonb_build_object('variantId',id))) FROM product_variants WHERE product_id=$1 LIMIT 1`, productID); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.MergeDraftProducts(ctx, actors[0], MergeProductsRequest{TargetProductID: committed, SourceProductIDs: []int64{other.ID}}); err != nil {
		t.Fatal(err)
	}
	var planCount int
	if err = pool.QueryRow(ctx, `SELECT jsonb_array_length(config->'variants') FROM catalog_editor_plans WHERE product_id=$1`, committed).Scan(&planCount); err != nil || planCount != 2 {
		t.Fatalf("draft editor plans lost: %d %v", planCount, err)
	}
	merged, err := repo.CreateProduct(ctx, actors[1], input)
	if err != nil || merged.ID != committed {
		t.Fatalf("merged receipt lost: %+v %v", merged, err)
	}
	// A deleted product is a tombstone, not permission to create a duplicate.
	if _, err = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, committed); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateProduct(ctx, actors[0], input); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("deleted creation was replayed as a new product", err)
	}
}
