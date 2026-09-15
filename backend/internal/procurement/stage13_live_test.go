package procurement

import (
 "context"
 "fmt"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestStage13PartialSalesDoNotCountUnpaidPlants(t *testing.T) {
 if os.Getenv("CRM_TEST_DATABASE_URL")=="" { t.Skip("CRM_TEST_DATABASE_URL is not set") }
 ctx:=context.Background()
 pool,err:=pgxpool.New(ctx,os.Getenv("CRM_TEST_DATABASE_URL"));if err!=nil{t.Fatal(err)};defer pool.Close()
 var owner,variant,product,orderID,itemID,offerID int64
 if err=pool.QueryRow(ctx,`SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&owner);err!=nil{t.Fatal(err)}
 if err=pool.QueryRow(ctx,`SELECT id,product_id FROM product_variants WHERE saby_id='crm-stage07-a'`).Scan(&variant,&product);err!=nil{t.Fatal(err)}
 prefix:=fmt.Sprintf("CRM-STAGE13-%d",time.Now().UnixNano())
 if err=pool.QueryRow(ctx,`INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder,created_at) VALUES($1,$2,'Stage13','+70000000000','stage13@example.invalid','pickup',0,1500,1500,'online','partially_paid','confirmed',1,'2041-09-10 12:00+03') RETURNING id`,prefix,owner).Scan(&orderID);err!=nil{t.Fatal(err)}
 defer func(){_,_=pool.Exec(ctx,`DELETE FROM sales_events WHERE source_document_id=$1`,prefix);_,_=pool.Exec(ctx,`DELETE FROM orders WHERE id=$1`,orderID)}()
 if err=pool.QueryRow(ctx,`INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty) VALUES($1,$2,$3,'stage13','Plant','P12','{}',500,3,1,0) RETURNING id`,orderID,product,variant).Scan(&itemID);err!=nil{t.Fatal(err)}
 if err=pool.QueryRow(ctx,`INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,subtotal,total,created_by) VALUES($1,$2,1,'paid','pickup',500,500,$3) RETURNING id`,orderID,prefix,owner).Scan(&offerID);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO shipment_offer_items(shipment_offer_id,order_item_id,variant_id,sku,product_name,unit_price,quantity) VALUES($1,$2,$3,'stage13','Plant',500,1)`,offerID,itemID,variant);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO payments(order_id,shipment_offer_id,idempotence_key,amount,status,paid_at) VALUES($1,$2,$3,500,'paid','2041-09-10 12:00+03')`,orderID,offerID,prefix);err!=nil{t.Fatal(err)}
 store:=NewPostgresStore(pool);date:=time.Date(2041,9,10,0,0,0,0,time.UTC)
 for i:=0;i<2;i++ { if _,err=store.RefreshSiteSales(ctx,date,date);err!=nil{t.Fatal(err)} }
 var units,count int;var gross float64
 if err=pool.QueryRow(ctx,`SELECT COUNT(*),COALESCE(SUM(units),0),COALESCE(SUM(gross_rub),0)::DOUBLE PRECISION FROM sales_events WHERE source_document_id=$1 AND reconciliation_status='counted'`,prefix).Scan(&count,&units,&gross);err!=nil{t.Fatal(err)}
 if count!=1||units!=1||gross!=500 {t.Fatalf("unpaid or duplicated sale count=%d units=%d gross=%v",count,units,gross)}
 var packaging float64
 checkPack:=func(want float64){t.Helper();if err=pool.QueryRow(ctx,`SELECT COALESCE(SUM(p.amount_rub),0)::DOUBLE PRECISION FROM finance_packaging_snapshots p JOIN sales_events s ON s.id=p.sales_event_id WHERE s.source_document_id=$1`,prefix).Scan(&packaging);err!=nil||packaging!=want{t.Fatalf("packaging=%v want=%v err=%v",packaging,want,err)}}
 checkPack(0)
 if _,err=pool.Exec(ctx,`UPDATE shipment_offers SET delivery_method='cdek',status='shipped' WHERE id=$1`,offerID);err!=nil{t.Fatal(err)}
 if _,err=store.RefreshSiteSales(ctx,date,date);err!=nil{t.Fatal(err)};checkPack(150)
 if _,err=store.RefreshSiteSales(ctx,date,date);err!=nil{t.Fatal(err)};checkPack(150)
}
