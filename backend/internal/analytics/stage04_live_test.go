package analytics

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage04SalesSummaryOnLiveDatabase(t *testing.T){
	dsn:=os.Getenv("CRM_TEST_DATABASE_URL");if dsn==""{t.Skip("CRM_TEST_DATABASE_URL is not set")};ctx:=context.Background();pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close();unique:=time.Now().UnixNano();prefix:=fmt.Sprintf("analytics-stage04-%d",unique)
	insert:=func(channel,eventID,document,eventType,status string,eventAt time.Time,units int,gross float64,effect int,reconciliation string){t.Helper();_,err:=pool.Exec(ctx,`INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,units,gross_rub,effect,reconciliation_status,import_batch_id) VALUES($1,$2,$3,$2,$4,$5,$6,'__adjustment__',$7,$8,$9,$10,gen_random_uuid())`,channel,prefix+eventID,prefix+document,eventType,status,eventAt,units,gross,effect,reconciliation);if err!=nil{t.Fatal(err)}}
	insert("ozon","-sale","-order","sale","confirmed",time.Date(2026,9,10,20,30,0,0,time.UTC),2,2400,1,"counted")
	insert("saby","-duplicate","-receipt","sale","confirmed",time.Date(2026,9,10,20,35,0,0,time.UTC),2,2400,1,"duplicate")
	insert("ozon","-return","-return","return","confirmed",time.Date(2026,9,11,21,30,0,0,time.UTC),1,1200,-1,"counted")
	insert("ozon","-pending","-pending","sale","pending",time.Date(2026,9,11,10,0,0,0,time.UTC),1,1200,1,"excluded")
	insert("wb","-ambiguous","-ambiguous","sale","confirmed",time.Date(2026,9,11,11,0,0,0,time.UTC),1,1200,1,"ambiguous")
	store:=NewStore(pool);store.now=func()time.Time{return time.Date(2026,9,12,12,0,0,0,time.FixedZone("MSK",3*60*60))}
	summary,err:=store.Summary(ctx,7,"");if err!=nil{t.Fatal(err)}
	if summary.Revenue!=1200||summary.Orders!=1||summary.SoldUnits!=1||summary.Returns!=1||summary.AverageCheck!=2400{t.Fatalf("summary=%+v",summary)}
	if summary.DataQuality.Duplicates<1||summary.DataQuality.Ambiguous<1||summary.DataQuality.Pending<1{t.Fatalf("quality=%+v",summary.DataQuality)}
	if len(summary.Daily)!=7||summary.Daily[5].Date!="2026-09-11"||summary.Daily[6].Date!="2026-09-12"{t.Fatalf("daily boundaries=%+v",summary.Daily)}
	if len(summary.SalesChannels)!=5||summary.SalesChannels[4].Channel!="avito"||summary.SalesChannels[4].Availability!="no_data"{t.Fatalf("channels=%+v",summary.SalesChannels)}
	if _,err:=pool.Exec(ctx,`UPDATE procurement_sales_sync_state SET status='error',last_error='stage 04 fixture' WHERE channel='wb'`);err!=nil{t.Fatal(err)}
	unavailable,err:=store.Summary(ctx,7,"");if err!=nil{t.Fatal(err)};if unavailable.SalesChannels[1].Channel!="wb"||unavailable.SalesChannels[1].Availability!="unavailable"{t.Fatalf("unavailable channel=%+v",unavailable.SalesChannels[1])}
	if _,err:=pool.Exec(ctx,`UPDATE procurement_sales_sync_state SET status='ok',last_error='' WHERE channel='wb'`);err!=nil{t.Fatal(err)}
	filtered,err:=store.Summary(ctx,7,"ozon");if err!=nil{t.Fatal(err)};if filtered.Revenue!=1200||len(filtered.SalesChannels)!=1||filtered.SalesChannels[0].Channel!="ozon"{t.Fatalf("filtered=%+v",filtered)}
}
