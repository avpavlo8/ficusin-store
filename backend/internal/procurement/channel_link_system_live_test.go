package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSystemChannelLinkLeavesUpdatedByNullOnLiveDatabase(t *testing.T) {
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

	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("ci-system-link-%d", unique)
	article := fmt.Sprintf("ci-system-article-%d", unique)
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,article,name) VALUES($1,$2,'System channel link test')`, sabyID, article); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM saby_nomenclature WHERE saby_id=$1`, sabyID) }()

	store := NewPostgresStore(pool)
	for _, channel := range []string{"wb", "ozon"} {
		externalID := fmt.Sprint(unique)
		result, err := store.LinkChannelProducts(ctx, Actor{Role: "system"}, channel, []ChannelProduct{{ExternalID: externalID, Article: article}})
		if err != nil || result.Linked != 1 {
			t.Fatalf("%s link: result=%+v err=%v", channel, result, err)
		}
		var updatedByIsNull bool
		if err := pool.QueryRow(ctx, `SELECT updated_by IS NULL FROM procurement_product_channels WHERE saby_id=$1`, sabyID).Scan(&updatedByIsNull); err != nil || !updatedByIsNull {
			t.Fatalf("%s updated_by: null=%v err=%v", channel, updatedByIsNull, err)
		}
	}
}
