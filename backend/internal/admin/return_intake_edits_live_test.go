package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestReturnIntakeEditsPreserveUnitsOnLiveDatabase(t *testing.T) {
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
	var ownerID, managerID, productID, variantID int64
	var id string
	for _, entry := range []struct {
		email  string
		target *int64
	}{{"crm-owner@example.invalid", &ownerID}, {"crm-manager@example.invalid", &managerID}} {
		if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email=$1`, entry.email).Scan(entry.target); err != nil {
			t.Fatal(err)
		}
	}
	unique := fmt.Sprint(time.Now().UnixNano())
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Араукария',$1,'draft','plants') RETURNING id`, "intake-edit-"+unique).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor) VALUES($1,$2,$2,'50 см · D14',275000) RETURNING id`, productID, unique).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT gen_random_uuid()::TEXT`).Scan(&id)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE intake_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM return_intakes WHERE id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM product_variants WHERE id=$1`, variantID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
	}()
	owner := Actor{CustomerID: ownerID, Role: RoleOwner}
	manager := Actor{CustomerID: managerID, Role: RoleManager}
	data := ReturnIntakeData{Channel: "wb", Rows: []ReturnIntakeRow{{Key: "row", VariantID: variantID, Name: "Араукария", Quantity: 2, Condition: "restoring"}}}
	draft, err := repo.SaveReturnIntakeDraft(ctx, owner, id, ReturnIntakeSave{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := repo.AcceptReturnIntake(ctx, owner, id, draft.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	first, second := saved.Items[0].ID, saved.Items[1].ID
	var photoID int64
	if err = pool.QueryRow(ctx, `INSERT INTO marketplace_return_photos(marketplace_return_id,content_type,image,created_by) VALUES($1,'image/png',$2,$3) RETURNING id`, first, []byte{1, 2, 3}, ownerID).Scan(&photoID); err != nil {
		t.Fatal(err)
	}
	request := func(doc *ReturnIntake) ReturnIntakeEdit {
		versions := map[string]string{}
		for _, u := range doc.Items {
			versions[strconv.FormatInt(u.ID, 10)] = u.Version
		}
		return ReturnIntakeEdit{Revision: doc.Revision, Data: ReturnIntakeData{Channel: "other", ChannelComment: "Из магазина", Rows: []ReturnIntakeRow{{Key: "keep", ItemIDs: []int64{first, second}, VariantID: variantID, Name: "Араукария", Quantity: 2, Condition: "ready", Comment: "После осмотра"}, {Key: "added", VariantID: variantID, Name: "Араукария", Quantity: 1, Condition: "restoring"}}}, ItemVersions: versions}
	}
	edit := request(saved)
	raw, _ := json.Marshal(edit)
	changed, err := repo.UpdateReturnIntake(ctx, owner, id, edit)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Items) != 3 || changed.Items[0].ID != first || changed.Items[1].ID != second || len(changed.Items[0].PhotoIDs) != 1 || changed.Items[0].PhotoIDs[0] != photoID || changed.DocumentNumber != saved.DocumentNumber || changed.Items[0].Channel != "other" || changed.Items[0].Price != 2750 {
		t.Fatalf("edit lost identity or snapshot: %+v", changed)
	}
	var repeat ReturnIntakeEdit
	_ = json.Unmarshal(raw, &repeat)
	again, err := repo.UpdateReturnIntake(ctx, owner, id, repeat)
	if err != nil || len(again.Items) != 3 || again.Revision != changed.Revision {
		t.Fatalf("lost response duplicated edit: %+v %v", again, err)
	}
	stale := request(changed)
	if _, err = repo.UpdateMarketplaceReturn(ctx, manager, first, ReturnUpdate{Condition: "ready", Comment: "Оценка коллеги"}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.UpdateReturnIntake(ctx, owner, id, stale); err == nil {
		t.Fatal("overwrote concurrent assessment")
	}
	current, err := repo.ReturnIntake(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	removal := request(current)
	removal.Data.Rows = removal.Data.Rows[:1]
	removal.Data.Rows[0].ItemIDs = []int64{first}
	removal.Data.Rows[0].Quantity = 1
	if _, err = repo.UpdateReturnIntake(ctx, manager, id, removal); err != ErrForbidden {
		t.Fatalf("manager removed units: %v", err)
	}
	removed, err := repo.UpdateReturnIntake(ctx, owner, id, removal)
	if err != nil || len(removed.Items) != 1 || removed.Items[0].ID != first {
		t.Fatalf("safe reduction: %+v %v", removed, err)
	}
	archived, err := repo.ArchivedReturns(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range archived.Items {
		if u.ID == second {
			found = true
		}
	}
	if !found {
		t.Fatal("removed unit is not recoverable")
	}
	if _, err = pool.Exec(ctx, `UPDATE marketplace_returns SET receipt_status='posted',receipt_external_url='https://ret.saby.ru/edit-test',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	current, _ = repo.ReturnIntake(ctx, owner, id)
	linked := request(current)
	linked.Data.Rows = linked.Data.Rows[:1]
	linked.Data.Rows[0].ItemIDs = []int64{first}
	linked.Data.Rows[0].Quantity = 1
	linked.Data.Rows[0].Condition = "restoring"
	corrected, err := repo.UpdateReturnIntake(ctx, owner, id, linked)
	if err != nil || corrected.Items[0].ReceiptStatus != "correction_required" || corrected.Items[0].ReceiptExternalURL != "https://ret.saby.ru/edit-test" {
		t.Fatalf("lost receipt association: %+v %v", corrected, err)
	}
	linkedRemoval := request(corrected)
	linkedRemoval.Data.Rows = linkedRemoval.Data.Rows[1:]
	if _, err = repo.UpdateReturnIntake(ctx, owner, id, linkedRemoval); err == nil {
		t.Fatal("removed a unit linked to Saby")
	}
	replacement := request(corrected)
	replacement.Data.Rows = replacement.Data.Rows[:1]
	replacement.Data.Rows[0].ItemIDs = []int64{first}
	replacement.Data.Rows[0].Quantity = 1
	replacement.Data.Rows[0].VariantID = variantID + 999999
	if _, err = repo.UpdateReturnIntake(ctx, owner, id, replacement); err == nil {
		t.Fatal("replaced a unit linked to Saby")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM return_intake_edits WHERE intake_id=$1`, id).Scan(&count); err != nil || count != 3 {
		t.Fatalf("audit snapshots: %d %v", count, err)
	}
	labelItems, err := repo.ReturnLabelItems(ctx, owner, []int64{first})
	if err != nil || labelItems[0].DiscountPercent != 0 || labelItems[0].DiscountedPrice != 2750 {
		t.Fatalf("zero-discount label: %+v %v", labelItems, err)
	}
}
