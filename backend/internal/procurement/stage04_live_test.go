package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage04SalesLedgerOnLiveDatabase(t *testing.T) {
	dsn:=os.Getenv("CRM_TEST_DATABASE_URL");if dsn==""{t.Skip("CRM_TEST_DATABASE_URL is not set")}
	ctx:=context.Background();pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
	store:=NewPostgresStore(pool);unique:=time.Now().UnixNano();sabyID:=fmt.Sprintf("stage04-saby-%d",unique);offer:=fmt.Sprintf("stage04-offer-%d",unique);wbID:=fmt.Sprintf("%d",unique%1000000000)
	var productID,variantID int64
	if err:=pool.QueryRow(ctx,`INSERT INTO products(saby_id,name,slug,status,catalog_section) VALUES($1,'Stage 04 plant',$2,'published','plants') RETURNING id`,sabyID,fmt.Sprintf("stage04-%d",unique)).Scan(&productID);err!=nil{t.Fatal(err)}
	if err:=pool.QueryRow(ctx,`INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active) VALUES($1,$2,$3,'P12',10000,1) RETURNING id`,productID,sabyID,fmt.Sprintf("stage04-sku-%d",unique)).Scan(&variantID);err!=nil{t.Fatal(err)}
	if _,err:=pool.Exec(ctx,`INSERT INTO saby_nomenclature(saby_id,code,name,price_minor,balance) VALUES($1,$2,'Stage 04 plant',10000,5)`,sabyID,fmt.Sprintf("S04-%d",unique));err!=nil{t.Fatal(err)}
	if _,err:=pool.Exec(ctx,`INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,is_primary,source) VALUES
		($1,$2,'ozon','offer_id',$3,'active',TRUE,'manual'),($1,$2,'wildberries','sku',$4,'active',TRUE,'manual')`,productID,variantID,offer,wbID);err!=nil{t.Fatal(err)}
	from:=time.Date(2026,9,10,0,0,0,0,time.UTC);to:=time.Date(2026,9,12,0,0,0,0,time.UTC)
	ozon:=SalesRecord{Date:from.Add(12*time.Hour),ExternalID:offer,Units:2,GrossRUB:2400,SourceEventID:"OZ-ORDER-1",SourceDocumentID:"OZ-ORDER-1",SourceLineID:"line-1",CrossSourceKey:"shared-order-1",EventType:"sale",EventStatus:"confirmed"}
	if _,err:=store.ReplaceSales(ctx,"ozon",from,to,[]SalesRecord{ozon});err!=nil{t.Fatal(err)}
	saby:=SalesRecord{Date:from.Add(13*time.Hour),ExternalID:sabyID,SabyID:sabyID,Units:2,GrossRUB:2400,SourceEventID:"SBIS-RECEIPT-1",SourceDocumentID:"SBIS-RECEIPT-1",SourceLineID:"line-1",CrossSourceKey:"shared-order-1",EventType:"sale",EventStatus:"confirmed"}
	if _,err:=store.ReplaceSales(ctx,"saby",from,to,[]SalesRecord{saby});err!=nil{t.Fatal(err)}
	var counted,duplicates int;var revenue float64
	if err:=pool.QueryRow(ctx,`SELECT COUNT(*) FILTER(WHERE reconciliation_status='counted'),COUNT(*) FILTER(WHERE reconciliation_status='duplicate'),COALESCE(SUM(gross_rub*effect) FILTER(WHERE reconciliation_status='counted'),0)::DOUBLE PRECISION FROM sales_events WHERE source_event_id IN ('OZ-ORDER-1','SBIS-RECEIPT-1')`).Scan(&counted,&duplicates,&revenue);err!=nil{t.Fatal(err)}
	if counted!=1||duplicates!=1||revenue!=2400{t.Fatalf("dedupe counted=%d duplicates=%d revenue=%.2f",counted,duplicates,revenue)}
	// Replaying an overlapping window updates the same source event.
	ozon.GrossRUB=2500;if _,err:=store.ReplaceSales(ctx,"ozon",from,to,[]SalesRecord{ozon});err!=nil{t.Fatal(err)}
	var rows int;if err:=pool.QueryRow(ctx,`SELECT COUNT(*) FROM sales_events WHERE channel='ozon' AND source_event_id='OZ-ORDER-1'`).Scan(&rows);err!=nil{t.Fatal(err)};if rows!=1{t.Fatalf("overlap created %d rows",rows)}
	// No common source key means no unsafe amount/time deduplication.
	withoutKey:=saby;withoutKey.SourceEventID="SBIS-RECEIPT-2";withoutKey.SourceDocumentID="SBIS-RECEIPT-2";withoutKey.CrossSourceKey="";withoutKey.GrossRUB=2500
	if _,err:=store.ReplaceSales(ctx,"saby",from,to,[]SalesRecord{saby,withoutKey});err!=nil{t.Fatal(err)}
	if err:=pool.QueryRow(ctx,`SELECT COUNT(*) FROM sales_events WHERE source_event_id='SBIS-RECEIPT-2' AND reconciliation_status='counted'`).Scan(&rows);err!=nil{t.Fatal(err)};if rows!=1{t.Fatal("event without common key was collapsed")}
	// A late return belongs to its own event day and repeated quantity stays intact.
	returnEvent:=ozon;returnEvent.Date=to.Add(21*time.Hour+30*time.Minute);returnEvent.SourceEventID="OZ-RETURN-1";returnEvent.SourceDocumentID="OZ-RETURN-1";returnEvent.CrossSourceKey="";returnEvent.EventType="return";returnEvent.Units=-1;returnEvent.GrossRUB=-1250
	if _,err:=store.ReplaceSales(ctx,"ozon",from,to.AddDate(0,0,1),[]SalesRecord{ozon,returnEvent});err!=nil{t.Fatal(err)}
	var localDate string;var effect,units int;if err:=pool.QueryRow(ctx,`SELECT TO_CHAR(event_at AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD'),effect,units FROM sales_events WHERE source_event_id='OZ-RETURN-1'`).Scan(&localDate,&effect,&units);err!=nil{t.Fatal(err)}
	if localDate!="2026-09-13"||effect!=-1||units!=1{t.Fatalf("late return date=%s effect=%d units=%d",localDate,effect,units)}
	// Pending orders and unknown products remain visible but excluded from totals.
	pending:=ozon;pending.SourceEventID="OZ-PENDING";pending.SourceDocumentID="OZ-PENDING";pending.CrossSourceKey="";pending.EventStatus="pending"
	unmatched:=ozon;unmatched.SourceEventID="OZ-UNKNOWN";unmatched.SourceDocumentID="OZ-UNKNOWN";unmatched.SourceLineID="unknown";unmatched.CrossSourceKey="";unmatched.ExternalID="unknown-product"
	if _,err:=store.ReplaceSales(ctx,"ozon",from,to.AddDate(0,0,1),[]SalesRecord{ozon,returnEvent,pending,unmatched});err!=nil{t.Fatal(err)}
	var pendingStatus,unknownStatus string;if err:=pool.QueryRow(ctx,`SELECT MAX(reconciliation_status) FILTER(WHERE source_event_id='OZ-PENDING'),MAX(reconciliation_status) FILTER(WHERE source_event_id='OZ-UNKNOWN') FROM sales_events`).Scan(&pendingStatus,&unknownStatus);err!=nil{t.Fatal(err)}
	if pendingStatus!="excluded"||unknownStatus!="unmatched"{t.Fatalf("pending=%s unknown=%s",pendingStatus,unknownStatus)}
	// A cancellation before confirmation is retained as an excluded fact, not a negative sale.
	cancelled:=ozon;cancelled.SourceEventID="OZ-CANCELLED";cancelled.SourceDocumentID="OZ-CANCELLED";cancelled.CrossSourceKey="";cancelled.EventType="cancellation";cancelled.EventStatus="cancelled";cancelled.Units=0;cancelled.GrossRUB=0
	if _,err:=store.ReplaceSales(ctx,"ozon",from,to.AddDate(0,0,1),[]SalesRecord{ozon,returnEvent,pending,unmatched,cancelled});err!=nil{t.Fatal(err)}
	if err:=pool.QueryRow(ctx,`SELECT COUNT(*) FROM sales_events WHERE source_event_id='OZ-CANCELLED' AND reconciliation_status='excluded' AND effect=1`).Scan(&rows);err!=nil{t.Fatal(err)};if rows!=1{t.Fatal("cancellation became a negative sale")}
}
