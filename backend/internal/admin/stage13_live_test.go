package admin

import (
 "context"
 "fmt"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestStage13PnLUsesMoscowBusinessDay(t *testing.T) {
 if os.Getenv("CRM_TEST_DATABASE_URL")==""{t.Skip("CRM_TEST_DATABASE_URL is not set")}
 ctx:=context.Background();pool,err:=pgxpool.New(ctx,os.Getenv("CRM_TEST_DATABASE_URL"));if err!=nil{t.Fatal(err)};defer pool.Close()
 prefix:=fmt.Sprintf("stage13-day-%d",time.Now().UnixNano())
 defer func(){_,_=pool.Exec(ctx,`DELETE FROM sales_events WHERE source_event_id LIKE $1`,prefix+"%") }()
 for i,date:=range []string{"2051-09-01 00:30:00+03","2051-09-02 00:30:00+03"} {
  if _,err=pool.Exec(ctx,`INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,units,gross_rub,effect,reconciliation_status,import_batch_id) VALUES('saby',$1,$1,'1','sale','confirmed',$2::TIMESTAMPTZ,'test',1,$3,1,'counted',gen_random_uuid())`,fmt.Sprintf("%s-%d",prefix,i),date,100*(i+1));err!=nil{t.Fatal(err)}
 }
 report,err:=NewPostgresRepository(pool).FinancePnL(ctx,"2051-09-01","2051-09-01");if err!=nil{t.Fatal(err)}
 if report.Revenue!=100||report.UnknownCostUnits!=1 {t.Fatalf("wrong business day: %+v",report)}
}
