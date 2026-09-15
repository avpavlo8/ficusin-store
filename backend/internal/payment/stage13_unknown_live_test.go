package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

type unknownPaymentProvider struct {
	creates int
	firstTimeout bool
	lastRequest integration.PaymentRequest
	fetched integration.Payment
}

func (*unknownPaymentProvider) Configured() bool { return true }
func (provider *unknownPaymentProvider) CreatePayment(_ context.Context, request integration.PaymentRequest) (integration.Payment,error) {
	provider.creates++
	provider.lastRequest=request
	result:=integration.Payment{ID:"stage13-recovered-provider",Status:StatusPending,Amount:request.Amount,ConfirmationURL:"https://payments.example.invalid/recovered"}
	if provider.firstTimeout && provider.creates==1 { return integration.Payment{},fmt.Errorf("%w: response timeout after acceptance",integration.ErrPaymentOutcomeUnknown) }
	return result,nil
}
func (provider *unknownPaymentProvider) FetchPayment(context.Context,string)(integration.Payment,error){ return provider.fetched,nil }
func (*unknownPaymentProvider) Refund(context.Context,string,float64,string)error{return nil}
func (*unknownPaymentProvider) CancelPayment(context.Context,string,string)error{return nil}

func TestStage13UnknownPaymentRecoveryOnLiveDatabase(t *testing.T) {
	databaseURL:=os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL=="" { t.Skip("CRM_TEST_DATABASE_URL is not set") }
	ctx:=context.Background()
	pool,err:=pgxpool.New(ctx,databaseURL);if err!=nil{t.Fatal(err)};defer pool.Close()
	provider:=&unknownPaymentProvider{firstTimeout:true}
	service:=NewService(pool,provider,"https://ficusin.example.invalid",slog.New(slog.NewTextHandler(io.Discard,nil)))

	insertOrder:=func(label string)(int64,string){
		number:=fmt.Sprintf("CRM-S13-UNKNOWN-%s-%d",label,time.Now().UnixNano())
		var id int64
		if err:=pool.QueryRow(ctx,`INSERT INTO orders(order_number,customer_name,phone,email,address,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder) VALUES($1,'Unknown payment','+70000000000','unknown@example.invalid','Рязань','pickup',0,1490,1490,'online','pending','confirmed',0) RETURNING id`,number).Scan(&id);err!=nil{t.Fatal(err)}
		t.Cleanup(func(){_,_=pool.Exec(context.Background(),`DELETE FROM orders WHERE id=$1`,id)})
		return id,number
	}

	orderID,number:=insertOrder("retry")
	if _,_,err=service.StartOutstanding(ctx,number);err==nil{t.Fatal("ambiguous timeout was treated as success")}
	var paymentID int64;var key,status,providerID string;var payload []byte;var attempts int
	if err=pool.QueryRow(ctx,`SELECT id,idempotence_key,status,provider_payment_id,request_payload,create_attempts FROM payments WHERE order_id=$1`,orderID).Scan(&paymentID,&key,&status,&providerID,&payload,&attempts);err!=nil{t.Fatal(err)}
	if status!=StatusPending||providerID!=""||len(payload)==0||attempts!=1{t.Fatalf("unknown attempt was not durable: status=%s provider=%q payload=%s attempts=%d",status,providerID,payload,attempts)}
	url,err:=service.RecoverUnknown(ctx,paymentID);if err!=nil{t.Fatal(err)}
	if url==""||provider.creates!=2||provider.lastRequest.IdempotenceKey!=key{t.Fatalf("unsafe recovery: url=%q creates=%d key=%q/%q",url,provider.creates,provider.lastRequest.IdempotenceKey,key)}
	if provider.lastRequest.Metadata["ficusin_payment_id"]!=strconv.FormatInt(paymentID,10){t.Fatalf("provider metadata=%v",provider.lastRequest.Metadata)}

	webhookOrderID,webhookNumber:=insertOrder("webhook")
	var webhookPaymentID int64
	if err=pool.QueryRow(ctx,`INSERT INTO payments(order_id,idempotence_key,amount,status) VALUES($1,$2,1490,'pending') RETURNING id`,webhookOrderID,fmt.Sprintf("webhook-key-%d",time.Now().UnixNano())).Scan(&webhookPaymentID);err!=nil{t.Fatal(err)}
	provider.fetched=integration.Payment{ID:"webhook-provider-id",Status:"succeeded",Paid:true,Amount:1490,Description:"Оплата заказа "+webhookNumber+" — Фикусин",Metadata:map[string]string{"ficusin_payment_id":strconv.FormatInt(webhookPaymentID,10)}}
	if err=service.SyncOutstanding(ctx,"webhook-provider-id");err!=nil{t.Fatal(err)}
	var webhookStatus string
	if err=pool.QueryRow(ctx,`SELECT status FROM payments WHERE id=$1`,webhookPaymentID).Scan(&webhookStatus);err!=nil||webhookStatus!=StatusPaid{t.Fatalf("webhook did not adopt unknown payment: %s %v",webhookStatus,err)}

	oldOrderID,oldNumber:=insertOrder("manual")
	oldKey:=fmt.Sprintf("old-key-%d",time.Now().UnixNano())
	oldRequest:=integration.PaymentRequest{IdempotenceKey:oldKey,Amount:1490,Description:"Оплата заказа "+oldNumber+" — Фикусин",ReturnURL:"https://ficusin.example.invalid",Items:[]integration.PaymentItem{{Name:"Заказ",Price:1490,Quantity:1}}}
	oldPayload,_:=json.Marshal(oldRequest)
	var oldPaymentID int64
	if err=pool.QueryRow(ctx,`INSERT INTO payments(order_id,idempotence_key,amount,status,request_payload,created_at) VALUES($1,$2,1490,'pending',$3,CURRENT_TIMESTAMP-INTERVAL '25 hours') RETURNING id`,oldOrderID,oldKey,oldPayload).Scan(&oldPaymentID);err!=nil{t.Fatal(err)}
	createsBefore:=provider.creates
	if _,err=service.RecoverUnknown(ctx,oldPaymentID);!errors.Is(err,ErrPaymentNeedsReview){t.Fatalf("expired idempotency window error=%v",err)}
	if provider.creates!=createsBefore{t.Fatal("provider was called after safe idempotency window")}
	if err=service.CancelPending(ctx,oldOrderID);!errors.Is(err,ErrPaymentNeedsReview){t.Fatalf("unknown payment allowed cancellation: %v",err)}

	provider.fetched=integration.Payment{ID:"manual-provider-id",Status:"succeeded",Paid:true,Amount:1490,Description:"Оплата заказа "+oldNumber+" — Фикусин"}
	balance,err:=service.ResolveUnknown(ctx,oldOrderID,oldPaymentID,"manual-provider-id");if err!=nil{t.Fatal(err)}
	if balance.PaymentStatus!=StatusPaid||balance.NetPaid!=1490||len(balance.Issues)!=0{t.Fatalf("manual reconciliation balance=%+v",balance)}

	dismissOrderID,_:=insertOrder("absent")
	var dismissPaymentID int64
	if err=pool.QueryRow(ctx,`INSERT INTO payments(order_id,idempotence_key,amount,status,created_at) VALUES($1,$2,1490,'pending',CURRENT_TIMESTAMP-INTERVAL '25 hours') RETURNING id`,dismissOrderID,fmt.Sprintf("absent-key-%d",time.Now().UnixNano())).Scan(&dismissPaymentID);err!=nil{t.Fatal(err)}
	dismissed,err:=service.DismissUnknown(ctx,dismissOrderID,dismissPaymentID);if err!=nil{t.Fatal(err)}
	if len(dismissed.Issues)!=0{t.Fatalf("dismissed attempt is still blocking: %+v",dismissed.Issues)}
}
