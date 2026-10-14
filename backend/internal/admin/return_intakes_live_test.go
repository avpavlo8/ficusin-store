package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReturnIntakeOnLiveDatabase(t *testing.T) {
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
	var ownerID, managerID, variantID, secondVariantID int64
	for _, row := range []struct {
		query string
		into  *int64
	}{
		{`SELECT id FROM customers WHERE email='crm-owner@example.invalid'`, &ownerID},
		{`SELECT id FROM customers WHERE email='crm-manager@example.invalid'`, &managerID},
		{`SELECT id FROM product_variants WHERE saby_id='crm-stage07-a'`, &variantID},
		{`SELECT id FROM product_variants WHERE saby_id='crm-stage07-b'`, &secondVariantID},
	} {
		if err = pool.QueryRow(ctx, row.query).Scan(row.into); err != nil {
			t.Fatal(err)
		}
	}

	// Use independent numeric Saby identities; no external requests are made.
	unique := time.Now().UnixNano()
	var productIDs []int64
	for index, target := range []*int64{&variantID, &secondVariantID} {
		var productID int64
		sabyID := fmt.Sprint(unique + int64(index))
		if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section,saby_id) VALUES($1,$2,'draft','plants',$3) RETURNING id`, fmt.Sprintf("Растение Stage 07 %d", index), fmt.Sprintf("intake-test-%d-%d", unique, index), sabyID).Scan(&productID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,current_unit_cost_rub) VALUES($1,$2,$3,'D12',500) RETURNING id`, productID, sabyID, fmt.Sprint(unique+int64(index))).Scan(target); err != nil {
			t.Fatal(err)
		}
		productIDs = append(productIDs, productID)
	}
	defer func() {
		for _, productID := range productIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM product_variants WHERE product_id=$1`, productID)
			_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		}
	}()
	actor := Actor{CustomerID: ownerID, Role: RoleOwner}
	var id string
	if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::TEXT`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE intake_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM return_intakes WHERE id=$1`, id)
	}()
	products, err := repo.ReturnProducts(ctx, "stage 07")
	if err != nil || len(products) < 2 {
		t.Fatalf("plant catalogue: %v %v", products, err)
	}
	var stockBefore, stockAfter int
	if err = pool.QueryRow(ctx, `SELECT COALESCE(SUM(available_qty),0)::INTEGER FROM inventory WHERE variant_id IN ($1,$2)`, variantID, secondVariantID).Scan(&stockBefore); err != nil {
		t.Fatal(err)
	}
	data := ReturnIntakeData{Channel: "other", ChannelComment: "Возврат из магазина", Rows: []ReturnIntakeRow{
		{Key: "a", VariantID: variantID, Name: "A", Quantity: 2, Condition: "ready", Comment: "Здоровые"},
		{Key: "b", VariantID: secondVariantID, Name: "B", Quantity: 1, Condition: "ready"},
		{Key: "c", VariantID: variantID, Name: "A", Quantity: 3, Condition: "restoring"},
		{Key: "d", VariantID: secondVariantID, Name: "B", Quantity: 4, Condition: "dead"},
	}}
	draft, err := repo.SaveReturnIntakeDraft(ctx, actor, id, ReturnIntakeSave{Data: data})
	if err != nil || draft.Revision != 1 || len(draft.Items) != 0 {
		t.Fatalf("save draft: %+v %v", draft, err)
	}
	// Lost successful response: the original PUT is safe to repeat.
	repeated, err := repo.SaveReturnIntakeDraft(ctx, actor, id, ReturnIntakeSave{Data: data})
	if err != nil || repeated.Revision != 1 {
		t.Fatalf("repeat draft: %+v %v", repeated, err)
	}
	loaded, err := repo.ReturnIntakeDraft(ctx, actor)
	if err != nil || loaded.ID != id || loaded.Data.ChannelComment != data.ChannelComment {
		t.Fatalf("reload draft: %+v %v", loaded, err)
	}
	if _, err = repo.ReturnIntake(ctx, Actor{CustomerID: managerID, Role: RoleManager}, id); err == nil {
		t.Fatal("another user read owner's draft")
	}
	data.Rows[0].Comment = "Обновлено"
	if _, err = repo.SaveReturnIntakeDraft(ctx, actor, id, ReturnIntakeSave{Data: data}); err == nil {
		t.Fatal("stale revision overwrote draft")
	}
	draft, err = repo.SaveReturnIntakeDraft(ctx, actor, id, ReturnIntakeSave{Revision: 1, Data: data})
	if err != nil || draft.Revision != 2 {
		t.Fatalf("save newer draft: %+v %v", draft, err)
	}
	if _, err = repo.AcceptReturnIntake(ctx, actor, id, 1, true); err == nil {
		t.Fatal("stale accept succeeded")
	}
	saved, err := repo.AcceptReturnIntake(ctx, actor, id, 2, true)
	if err != nil || len(saved.Items) != 10 || saved.ReceiptStatus != "queued" {
		t.Fatalf("accept receipt: %+v %v", saved, err)
	}
	again, err := repo.AcceptReturnIntake(ctx, actor, id, 2, true)
	if err != nil || len(again.Items) != 10 || again.Items[0].ID != saved.Items[0].ID {
		t.Fatalf("repeat receipt: %+v %v", again, err)
	}
	var actionID int64
	var count int
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT receipt_action_id FROM return_intakes WHERE id=$1`, id).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT payload FROM procurement_action_items WHERE id=$1`, actionID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Lines []struct {
			SabyID   string `json:"sabyId"`
			Quantity int    `json:"quantity"`
		} `json:"lines"`
	}
	if err = json.Unmarshal(payload, &receipt); err != nil || len(receipt.Lines) != 2 || receipt.Lines[0].Quantity != 2 || receipt.Lines[1].Quantity != 1 {
		t.Fatalf("ready-only receipt: %s %v", payload, err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items WHERE marketplace_return_id IN (SELECT id FROM marketplace_returns WHERE intake_id=$1)`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate action count=%d err=%v", count, err)
	}
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM marketplace_returns WHERE intake_id=$1 AND receipt_action_id=$2`, id, actionID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("ready unit count=%d err=%v", count, err)
	}
	if _, err = repo.CreateReturnReceipt(ctx, actor, saved.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_action_items SET status='processing',lock_owner='return-test',lock_token=1,attempts=1 WHERE id=$1`, actionID); err != nil {
		t.Fatal(err)
	}
	applied, err := procurement.NewPostgresStore(pool).FinishAction(ctx, actionID, "return-test", 1, procurement.ActionExecution{Completed: true, ExternalOperationID: "12345", ExternalURL: "https://ret.saby.ru/test"}, nil)
	if err != nil || !applied {
		t.Fatalf("finish grouped receipt: %v %v", applied, err)
	}
	posted, err := repo.ReturnIntake(ctx, actor, id)
	if err != nil || posted.ReceiptStatus != "posted" {
		t.Fatalf("posted intake: %+v %v", posted, err)
	}
	for _, item := range posted.Items {
		if item.ChannelComment != data.ChannelComment || (item.Condition == "ready" && item.ReceiptStatus != "posted") || (item.Condition != "ready" && item.ReceiptStatus != "none") {
			t.Fatalf("incorrect unit receipt state: %+v", item)
		}
	}
	if err = pool.QueryRow(ctx, `SELECT COALESCE(SUM(available_qty),0)::INTEGER FROM inventory WHERE variant_id IN ($1,$2)`, variantID, secondVariantID).Scan(&stockAfter); err != nil || stockAfter != stockBefore {
		t.Fatalf("intake changed stock %d -> %d: %v", stockBefore, stockAfter, err)
	}
	if _, err = repo.SaveReturnIntakeDraft(ctx, actor, id, ReturnIntakeSave{Revision: posted.Revision, Data: data}); err == nil {
		t.Fatal("saved intake was editable")
	}
}

func TestReturnIntakeMissingSabyRollsBackOnLiveDatabase(t *testing.T) {
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
	var ownerID, productID, variantID int64
	var id string
	if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Без СБИС',$1,'draft','plants') RETURNING id`, fmt.Sprintf("return-intake-%d", time.Now().UnixNano())).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,sku,label) VALUES($1,$2,'D12') RETURNING id`, productID, fmt.Sprint(time.Now().UnixNano())).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::TEXT`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE intake_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM return_intakes WHERE id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM product_variants WHERE id=$1`, variantID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
	}()
	actor := Actor{CustomerID: ownerID, Role: RoleOwner}
	repo := NewPostgresRepository(pool)
	data := ReturnIntakeData{Channel: "ozon", Rows: []ReturnIntakeRow{{Key: "a", VariantID: variantID, Quantity: 2, Condition: "ready"}}}
	draft, err := repo.SaveReturnIntakeDraft(ctx, actor, id, ReturnIntakeSave{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AcceptReturnIntake(ctx, actor, id, draft.Revision, true); err == nil {
		t.Fatal("missing Saby accepted a receipt")
	}
	loaded, err := repo.ReturnIntake(ctx, actor, id)
	if err != nil || loaded.Status != "draft" || len(loaded.Items) != 0 {
		t.Fatalf("failed receipt left partial ledger: %+v %v", loaded, err)
	}
	// Saving a physical return must still work without Saby integration.
	saved, err := repo.AcceptReturnIntake(ctx, actor, id, draft.Revision, false)
	if err != nil || len(saved.Items) != 2 || saved.ReceiptStatus != "none" {
		t.Fatalf("save without receipt: %+v %v", saved, err)
	}
}
