package payment

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage08ShipmentPaymentIsIdempotentAndHasExactReceipt(t *testing.T){
	databaseURL:=os.Getenv("CRM_TEST_DATABASE_URL");if databaseURL==""{t.Skip("CRM_TEST_DATABASE_URL is not set")};ctx:=context.Background();pool,err:=pgxpool.New(ctx,databaseURL);if err!=nil{t.Fatal(err)};defer pool.Close();unique:=time.Now().UnixNano();var customerID,orderID,productID,variantID,itemID,offerID int64;var sku string
	_ = pool.QueryRow(ctx,`SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID);_ = pool.QueryRow(ctx,`SELECT p.id,pv.id,pv.sku FROM products p JOIN product_variants pv ON pv.product_id=p.id WHERE p.slug='crm-stage07-a'`).Scan(&productID,&variantID,&sku)
	if err:=pool.QueryRow(ctx,`INSERT INTO orders(order_number,customer_id,customer_name,phone,email,address,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder) VALUES($1,$2,'Stage 08','+70000000000','stage08@example.invalid','Рязань','pickup',0,2290,2290,'online','pending','confirmed',1) RETURNING id`,fmt.Sprintf("CRM-S08-P-%d",unique),customerID).Scan(&orderID);err!=nil{t.Fatal(err)};defer func(){_,_=pool.Exec(ctx,`DELETE FROM orders WHERE id=$1`,orderID)}();_ = pool.QueryRow(ctx,`INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty) VALUES($1,$2,$3,$4,'Stage 08 plant','D17','{}',2290,1,1,0) RETURNING id`,orderID,productID,variantID,sku).Scan(&itemID);token:=fmt.Sprintf("stage08-pay-%d",unique);_ = pool.QueryRow(ctx,`INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,address_snapshot,subtotal,delivery_fee,total,notified_at,expires_at,created_by) VALUES($1,$2,1,'offered','pickup','Рязань',2290,0,2290,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP+INTERVAL '48 hours',$3) RETURNING id`,orderID,token,customerID).Scan(&offerID);_,_=pool.Exec(ctx,`INSERT INTO shipment_offer_items(shipment_offer_id,order_item_id,variant_id,sku,product_name,unit_price,quantity) VALUES($1,$2,$3,$4,'Stage 08 plant',2290,1)`,offerID,itemID,variantID,sku)
	var warehouseID int64
	if err=pool.QueryRow(ctx,`INSERT INTO warehouses(saby_id,name,city,address) VALUES($1,'Stage13','Test','Test') RETURNING id`,fmt.Sprintf("stage13-%d",unique)).Scan(&warehouseID);err!=nil{t.Fatal(err)}
	defer func(){_,_=pool.Exec(ctx,`DELETE FROM warehouses WHERE id=$1`,warehouseID)}()
	if _,err=pool.Exec(ctx,`INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty) VALUES($1,$2,5,0)`,warehouseID,variantID);err!=nil{t.Fatal(err)}
	provider:=&stage13TimeoutProvider{};service:=NewService(pool,provider,"https://ficusin.example.invalid",slog.New(slog.NewTextHandler(io.Discard,nil)));if _,err=service.StartShipmentOffer(ctx,token,customerID);err==nil{t.Fatal("expected ambiguous provider timeout")};originalKey:=provider.request.IdempotenceKey
	first,err:=service.StartShipmentOffer(ctx,token,customerID);if err!=nil{t.Fatal(err)};second,err:=service.StartShipmentOffer(ctx,token,customerID);if err!=nil{t.Fatal(err)};if first!=second||provider.creates!=2{t.Fatalf("not idempotent: %q %q creates=%d",first,second,provider.creates)};if provider.request.Amount!=2290||len(provider.request.Items)!=1||provider.request.Items[0].Name!="Stage 08 plant"{t.Fatalf("invalid receipt: %+v",provider.request)}
	if provider.request.IdempotenceKey!=originalKey{t.Fatal("timeout changed payment idempotence key")}
	var reserved int;if err=pool.QueryRow(ctx,`SELECT reserved_qty FROM order_items WHERE id=$1`,itemID).Scan(&reserved);err!=nil||reserved!=1{t.Fatalf("checkout reservation=%d err=%v",reserved,err)}
	provider.status=integration.Payment{ID:"ci-provider-payment",Status:"succeeded",Paid:true,Amount:2290};if err:=service.SyncOutstanding(ctx,"ci-provider-payment");err!=nil{t.Fatal(err)};var status string;_ = pool.QueryRow(ctx,`SELECT status FROM shipment_offers WHERE id=$1`,offerID).Scan(&status);if status!="paid"{t.Fatalf("offer status=%s",status)}
	if _,err=pool.Exec(ctx,`UPDATE shipment_offers SET status='shipped' WHERE id=$1`,offerID);err!=nil{t.Fatal(err)}
	if err=service.SyncOutstanding(ctx,"ci-provider-payment");err!=nil{t.Fatal(err)}
	if err=pool.QueryRow(ctx,`SELECT status FROM shipment_offers WHERE id=$1`,offerID).Scan(&status);err!=nil||status!="shipped"{t.Fatalf("repeated webhook rewound fulfillment: %s %v",status,err)}
}

type stage13TimeoutProvider struct { livePaymentProvider }
func (provider *stage13TimeoutProvider) CreatePayment(ctx context.Context, request integration.PaymentRequest) (integration.Payment,error) {
	result,err:=provider.livePaymentProvider.CreatePayment(ctx,request)
	if provider.creates==1{return integration.Payment{},fmt.Errorf("%w: simulated response timeout after provider acceptance",integration.ErrPaymentOutcomeUnknown)}
	return result,err
}
