package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManualPriceProposalsAndStaleApprovalOnLiveDatabase(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var variantID, oldPrice, actorID int64
	err = pool.QueryRow(ctx, `SELECT v.id,v.base_price_minor,c.id FROM product_variants v CROSS JOIN customers c ORDER BY v.id,c.id LIMIT 1`).Scan(&variantID, &oldPrice, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no variant and customer in test database")
	}
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err = createManualPriceProposals(ctx, tx, Actor{CustomerID: actorID}, variantID, oldPrice, oldPrice+10000); err != nil {
		t.Fatal(err)
	}
	var batchID int64
	var sitePrice int64
	if err = tx.QueryRow(ctx, `SELECT id,manual_site_price_minor FROM procurement_action_batches WHERE source='manual' AND manual_variant_id=$1 ORDER BY id DESC LIMIT 1`, variantID).Scan(&batchID, &sitePrice); err != nil {
		t.Fatal(err)
	}
	if sitePrice != oldPrice+10000 {
		t.Fatalf("stored site price = %d", sitePrice)
	}
	var proposalID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM procurement_action_items WHERE batch_id=$1 AND channel='wb'`, batchID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	// The edit is intentionally rolled back; an approval for its proposed price
	// must therefore reject the stale snapshot of the live site price.
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM procurement_action_batches WHERE id=$1`, batchID) //nolint:errcheck
	repository := NewPostgresRepository(pool).WithPriceChannelsConfigured(map[string]bool{"wb": true})
	_, err = repository.ApproveManualPriceProposal(ctx, Actor{CustomerID: actorID, Role: RoleOwner}, proposalID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale approval error = %v", err)
	}
}

func TestManualSabyExportRejectsSupersededPrice(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var actorID, productID, variantID, proposalID int64
	sabyID := fmt.Sprintf("manual-price-%d", unique)
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'test','Price test',now()) RETURNING id`,
		fmt.Sprintf("manual-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Manual price test',$1,'draft') RETURNING id`, sabyID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,'TEST-PRICE','Manual price test')`, sabyID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor)
		VALUES($1,$2,$3,'D12',150000) RETURNING id`, productID, sabyID,
		fmt.Sprintf("9%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = createManualPriceProposals(ctx, tx, actor, variantID, 100000, 150000); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT item.id FROM procurement_action_items item
		JOIN procurement_action_batches batch ON batch.id=item.batch_id
		WHERE batch.manual_variant_id=$1 AND item.channel='saby_price'`, variantID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	if _, err = repository.ApproveManualPriceProposal(ctx, actor, proposalID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repository.ManualSabyPriceXLSX(ctx, proposalID); err != nil {
		t.Fatalf("current approved price must export: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=200000 WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repository.ManualSabyPriceXLSX(ctx, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale Saby price exported: %v", err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = createManualPriceProposals(ctx, tx, actor, variantID, 150000, 200000); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM procurement_action_items WHERE id=$1`, proposalID).Scan(&status); err != nil || status != "skipped" {
		t.Fatalf("old approved Saby proposal status=%q err=%v", status, err)
	}
}

func TestManualMarketplacePriceRetryChecksSitePriceAndLink(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("retry-price-%d", unique)
	var actorID, productID, variantID, batchID, proposalID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Price retry test',now()) RETURNING id`,
		fmt.Sprintf("retry-price-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status) VALUES('Price retry test',$1,'draft') RETURNING id`, sabyID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID) }()
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,'RETRY','Price retry test')`, sabyID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor)
		VALUES($1,$2,$3,'D12',150000) RETURNING id`, productID, sabyID,
		fmt.Sprintf("7%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_product_channels(saby_id,wb_nm_id) VALUES($1,123456789)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(kind,source,manual_variant_id,manual_site_price_minor,created_by)
		VALUES('prices','manual',$1,150000,$2) RETURNING id`, variantID, actorID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE entity_type='price_proposal' AND entity_id=$1`, fmt.Sprint(proposalID))
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_action_batches WHERE id=$1`, batchID)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_action_items(batch_id,channel,external_article,new_value,status,attempts,error_message)
		VALUES($1,'wb','123456789',1500,'failed',5,'temporary failure') RETURNING id`, batchID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool).WithPriceChannelsConfigured(map[string]bool{"wb": true})
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	proposals, err := repository.ListManualPriceProposals(ctx, variantID)
	if err != nil || len(proposals) != 1 || !proposals[0].CanRetry {
		t.Fatalf("current proposal: %+v, err=%v", proposals, err)
	}
	// A link edited in the site catalogue must win over the unchanged legacy channel row.
	if _, err = pool.Exec(ctx, `UPDATE product_external_ids SET status='legacy',is_primary=false
		WHERE variant_id=$1 AND provider='wildberries' AND external_id='123456789'`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source)
		VALUES($1,$2,'wildberries','sku','987654321','active',true,'manual')`, productID, variantID); err != nil {
		t.Fatal(err)
	}
	proposals, err = repository.ListManualPriceProposals(ctx, variantID)
	if err != nil || len(proposals) != 1 || proposals[0].CanRetry || proposals[0].BlockReason != "Связь с каналом изменилась" {
		t.Fatalf("changed link proposal: %+v, err=%v", proposals, err)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("changed link retry error = %v", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM product_external_ids WHERE variant_id=$1 AND provider='wildberries' AND external_id='987654321'`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE product_external_ids SET status='active',is_primary=true
		WHERE variant_id=$1 AND provider='wildberries' AND external_id='123456789'`, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=160000 WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	proposals, err = repository.ListManualPriceProposals(ctx, variantID)
	if err != nil || len(proposals) != 1 || proposals[0].CanRetry || proposals[0].BlockReason != "Цена сайта изменилась; предложение устарело" {
		t.Fatalf("changed price proposal: %+v, err=%v", proposals, err)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("changed price retry error = %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE product_variants SET base_price_minor=150000 WHERE id=$1`, variantID); err != nil {
		t.Fatal(err)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); err != nil {
		t.Fatal(err)
	}
	var status, message string
	var attempts int
	if err = pool.QueryRow(ctx, `SELECT status,attempts,error_message FROM procurement_action_items WHERE id=$1`, proposalID).Scan(&status, &attempts, &message); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 0 || message != "" {
		t.Fatalf("retried proposal status=%q attempts=%d message=%q", status, attempts, message)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate retry error = %v", err)
	}
}

func TestManualAvitoPriceRetryChecksAllSizes(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	unique := time.Now().UnixNano()
	var actorID, productID, cheapID, expensiveID, warehouseID, batchID, proposalID int64
	listingID := fmt.Sprintf("retry-avito-%d", unique)
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Avito retry test',now()) RETURNING id`,
		fmt.Sprintf("retry-avito-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, actorID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,saby_fields)
		VALUES('Avito retry test',$1,'published',ARRAY['stock']) RETURNING id`, listingID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		for index := 0; index < 2; index++ {
			_, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, fmt.Sprintf("%s-%d", listingID, index))
		}
	}()
	for index, target := range []*int64{&cheapID, &expensiveID} {
		sabyID := fmt.Sprintf("%s-%d", listingID, index)
		if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name) VALUES($1,$2,'Avito retry test')`, sabyID, sabyID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor)
			VALUES($1,$2,$3,$4,$5) RETURNING id`, productID, sabyID,
			fmt.Sprintf("6%017d", unique%100000000000000000+int64(index)), fmt.Sprintf("D%d", 12+index), 100000+index*50000).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active=1 LIMIT 1`).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	for _, variantID := range []int64{cheapID, expensiveID} {
		if _, err = pool.Exec(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,synced_at)
			VALUES($1,$2,1,now())`, warehouseID, variantID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listings(item_id,title,status,remote_price_minor)
		VALUES($1,'Avito retry test','active',100000)`, listingID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM avito_listings WHERE item_id=$1`, listingID) }()
	if _, err = pool.Exec(ctx, `INSERT INTO avito_listing_products(item_id,product_id) VALUES($1,$2)`, listingID, productID); err != nil {
		t.Fatal(err)
	}
	var priorStatus string
	var priorSuccess *time.Time
	if err = pool.QueryRow(ctx, `SELECT status,last_success_at FROM procurement_integration_sync_state
		WHERE channel='saby' AND resource='catalog'`).Scan(&priorStatus, &priorSuccess); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `UPDATE procurement_integration_sync_state
		SET status=$1,last_success_at=$2 WHERE channel='saby' AND resource='catalog'`, priorStatus, priorSuccess)
	}()
	if _, err = pool.Exec(ctx, `UPDATE procurement_integration_sync_state
		SET status='ok',last_success_at=now() WHERE channel='saby' AND resource='catalog'`); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_action_batches(kind,source,manual_variant_id,manual_site_price_minor,created_by)
		VALUES('prices','manual',$1,150000,$2) RETURNING id`, expensiveID, actorID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_log WHERE entity_type='price_proposal' AND entity_id=$1`, fmt.Sprint(proposalID))
		_, _ = pool.Exec(ctx, `DELETE FROM procurement_action_batches WHERE id=$1`, batchID)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_action_items(batch_id,channel,external_article,new_value,status,attempts,error_message)
		VALUES($1,'avito',$2,1000,'failed',5,'temporary failure') RETURNING id`, batchID, listingID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool).WithPriceChannelsConfigured(map[string]bool{"avito": true})
	actor := Actor{CustomerID: actorID, Role: RoleOwner}
	proposals, err := repository.ListManualPriceProposals(ctx, expensiveID)
	if err != nil || len(proposals) != 1 || !proposals[0].CanRetry {
		t.Fatalf("fresh Avito proposal: %+v, err=%v", proposals, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=0 WHERE variant_id=$1`, cheapID); err != nil {
		t.Fatal(err)
	}
	proposals, err = repository.ListManualPriceProposals(ctx, expensiveID)
	if err != nil || len(proposals) != 1 || proposals[0].CanRetry || proposals[0].BlockReason != "Минимальная доступная цена сайта изменилась" {
		t.Fatalf("changed Avito minimum: %+v, err=%v", proposals, err)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("changed Avito minimum retry error = %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET available_qty=1,synced_at=now() WHERE variant_id=$1`, cheapID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET synced_at=now()-INTERVAL '3 hours' WHERE variant_id=$1`, expensiveID); err != nil {
		t.Fatal(err)
	}
	proposals, err = repository.ListManualPriceProposals(ctx, expensiveID)
	if err != nil || len(proposals) != 1 || proposals[0].CanRetry || proposals[0].BlockReason != "Нужен свежий остаток СБИС для всех размеров" {
		t.Fatalf("stale Avito stock: %+v, err=%v", proposals, err)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale Avito stock retry error = %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE inventory SET synced_at=now() WHERE variant_id=$1`, expensiveID); err != nil {
		t.Fatal(err)
	}
	if err = repository.RetryManualPriceProposal(ctx, actor, proposalID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM procurement_action_items WHERE id=$1`, proposalID).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("Avito retry status=%q err=%v", status, err)
	}
}
