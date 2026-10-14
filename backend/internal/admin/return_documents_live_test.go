package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"
)

func TestReturnIntakeRecoveryDocumentsOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewPostgresRepository(pool)
	var managerID, ownerID, productID, variantID int64
	var id string
	for _, entry := range []struct {
		email  string
		target *int64
	}{{"crm-manager@example.invalid", &managerID}, {"crm-owner@example.invalid", &ownerID}} {
		if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email=$1`, entry.email).Scan(entry.target); err != nil {
			t.Fatal(err)
		}
	}
	unique := fmt.Sprint(time.Now().UnixNano())
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Аглаонема Вайт Ленс',$1,'draft','plants') RETURNING id`, "recovery-test-"+unique).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor) VALUES($1,$2,$2,'D12',100000) RETURNING id`, productID, unique).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT gen_random_uuid()::TEXT`).Scan(&id)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE intake_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM return_intakes WHERE id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM product_variants WHERE id=$1`, variantID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
	}()
	manager := Actor{CustomerID: managerID, Role: RoleManager}
	owner := Actor{CustomerID: ownerID, Role: RoleOwner}
	administrator := Actor{CustomerID: ownerID, Role: RoleAdministrator}
	data := ReturnIntakeData{Channel: "wb", Rows: []ReturnIntakeRow{{Key: "recovery", VariantID: variantID, Name: "Аглаонема", Quantity: 2, Condition: "restoring", DiscountPercent: 30}, {Key: "ready", VariantID: variantID, Name: "Аглаонема", Quantity: 1, Condition: "ready"}, {Key: "dead", VariantID: variantID, Name: "Аглаонема", Quantity: 1, Condition: "dead"}}}
	draft, err := repo.SaveReturnIntakeDraft(ctx, manager, id, ReturnIntakeSave{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ReturnIntake(ctx, owner, id); err == nil {
		t.Fatal("private draft leaked")
	}
	saved, err := repo.AcceptReturnIntake(ctx, manager, id, draft.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DocumentNumber <= 0 || saved.CreatedAt.IsZero() || len(saved.Items) != 4 {
		t.Fatalf("document %+v", saved)
	}
	reopened, err := repo.ReturnIntake(ctx, owner, id)
	if err != nil || len(reopened.Items) != 4 {
		t.Fatalf("shared accepted doc: %+v %v", reopened, err)
	}
	recovery := saved.Items[0]
	if recovery.Price != 1000 || recovery.DiscountedPrice != 700 || recovery.Article == saved.Items[1].Article {
		t.Fatalf("instance pricing/art: %+v", recovery)
	}
	if _, err = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=200000 WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	recovery, err = repo.MarketplaceReturn(ctx, recovery.ID)
	if err != nil || recovery.DiscountedPrice != 700 {
		t.Fatalf("price snapshot changed %+v %v", recovery, err)
	}
	labels, err := repo.ReturnLabelItems(ctx, manager, []int64{saved.Items[0].ID, saved.Items[1].ID})
	if err != nil || len(labels) != 2 {
		t.Fatalf("labels %v %v", labels, err)
	}
	offers, err := repo.DiscountPlants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, offer := range offers {
		if offer.ID == saved.Items[0].ID || offer.ID == saved.Items[1].ID {
			found++
			if offer.DiscountedPrice != 700 {
				t.Fatalf("public price %+v", offer)
			}
		}
	}
	if found != 2 {
		t.Fatalf("offers found=%d", found)
	}
	// A valid phone-sized PNG larger than the old 1 MiB JSON limit, including
	// an empty/unreliable browser MIME type, is stored by its real content.
	picture := image.NewRGBA(image.Rect(0, 0, 600, 600))
	var random uint32 = 17
	for y := 0; y < 600; y++ {
		for x := 0; x < 600; x++ {
			random = random*1664525 + 1013904223
			picture.Set(x, y, color.RGBA{uint8(random), uint8(random >> 8), uint8(random >> 16), 255})
		}
	}
	var buffer bytes.Buffer
	if err = png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	photographed, err := repo.AddReturnPhoto(ctx, manager, recovery.ID, "", base64.StdEncoding.EncodeToString(buffer.Bytes()))
	if err != nil || len(photographed.PhotoIDs) != 1 {
		t.Fatalf("photo %+v %v", photographed, err)
	}
	if _, _, err = repo.DiscountPlantPhoto(ctx, recovery.ID); err != nil {
		t.Fatal(err)
	}
	// A failed selection is atomic: no receipt or condition changes.
	if _, err = repo.CreateReturnReceiptBatch(ctx, manager, []int64{saved.Items[2].ID, saved.Items[3].ID}, false); err == nil {
		t.Fatal("dead plant accepted")
	}
	fresh, _ := repo.MarketplaceReturn(ctx, saved.Items[2].ID)
	if fresh.ReceiptStatus != "none" {
		t.Fatal("partial receipt queued")
	}
	if _, err = repo.CreateReturnReceiptBatch(ctx, manager, []int64{saved.Items[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	sold, _ := repo.MarketplaceReturn(ctx, saved.Items[0].ID)
	if sold.Condition != "ready" || sold.DiscountedPrice != 700 || len(sold.History) != 2 {
		t.Fatalf("sell %+v", sold)
	}
	if _, _, err = repo.DiscountPlantPhoto(ctx, sold.ID); err == nil {
		t.Fatal("removed instance photo still public")
	}
	if _, err = repo.UpdateMarketplaceReturn(ctx, manager, saved.Items[1].ID, ReturnUpdate{Condition: "ready"}); err != nil {
		t.Fatal(err)
	}
	ids := []int64{saved.Items[1].ID, saved.Items[2].ID}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.CreateReturnReceiptBatch(ctx, manager, ids, false)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var actionID int64
	var payload []byte
	var actions int
	if err = pool.QueryRow(ctx, `SELECT receipt_action_id FROM marketplace_returns WHERE id=$1`, ids[0]).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items WHERE marketplace_return_id=ANY($1)`, ids).Scan(&actions)
	if actions != 1 {
		t.Fatalf("duplicate receipts %d", actions)
	}
	_ = pool.QueryRow(ctx, `SELECT payload FROM procurement_action_items WHERE id=$1`, actionID).Scan(&payload)
	var batch struct {
		Lines []struct {
			Quantity int `json:"quantity"`
		} `json:"lines"`
	}
	_ = json.Unmarshal(payload, &batch)
	if len(batch.Lines) != 1 || batch.Lines[0].Quantity != 2 {
		t.Fatalf("aggregation %s", payload)
	}
	_, _ = pool.Exec(ctx, `UPDATE procurement_action_items SET status='processing',lock_owner='recovery-test',lock_token=1 WHERE id=$1`, actionID)
	applied, err := procurement.NewPostgresStore(pool).FinishAction(ctx, actionID, "recovery-test", 1, procurement.ActionExecution{Completed: true, DraftCreated: true, ExternalOperationID: "123", ExternalURL: "https://ret.saby.ru/test"}, nil)
	if err != nil || !applied {
		t.Fatalf("draft finish %v %v", applied, err)
	}
	changed, err := repo.UpdateMarketplaceReturn(ctx, manager, ids[0], ReturnUpdate{Condition: "restoring"})
	if err != nil || changed.ReceiptStatus != "correction_required" {
		t.Fatalf("ready reversal %+v %v", changed, err)
	}
	reopened, err = repo.ReturnIntake(ctx, manager, id)
	if err != nil || reopened.Data.Rows[0].Condition != "restoring" || reopened.Data.Rows[0].Quantity != 2 {
		t.Fatalf("immutable original changed %+v %v", reopened, err)
	}
	if err = repo.DeleteReturns(ctx, manager, nil, id, false); err != ErrForbidden {
		t.Fatalf("manager delete %v", err)
	}
	if err = repo.DeleteReturns(ctx, administrator, nil, id, false); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.MarketplaceReturn(ctx, saved.Items[0].ID); err == nil {
		t.Fatal("deleted return visible")
	}
	if _, err = repo.ReturnIntake(ctx, owner, id); err == nil {
		t.Fatal("deleted document visible")
	}
}
