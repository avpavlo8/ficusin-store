package order

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stage08Sender struct{ mu sync.Mutex; sent int; fail bool }
func (sender *stage08Sender) Configured() bool{return true}
func (sender *stage08Sender) Send(context.Context,mail.Letter)error{sender.mu.Lock();defer sender.mu.Unlock();sender.sent++;if sender.fail{return errors.New("smtp unavailable")};return nil}

func TestStage08NotificationStartsOneStableFortyEightHourWindow(t *testing.T){
	databaseURL:=os.Getenv("CRM_TEST_DATABASE_URL");if databaseURL==""{t.Skip("CRM_TEST_DATABASE_URL is not set")};ctx:=context.Background();pool,err:=pgxpool.New(ctx,databaseURL);if err!=nil{t.Fatal(err)};defer pool.Close()
	unique:=time.Now().UnixNano();var customerID,orderID int64;if err:=pool.QueryRow(ctx,`SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID);err!=nil{t.Fatal(err)}
	if err:=pool.QueryRow(ctx,`INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,$2,'Stage 08','+70000000000','stage08@example.invalid','pickup',0,100,100,'online','pending','confirmed') RETURNING id`,fmt.Sprintf("CRM-S08-L-%d",unique),customerID).Scan(&orderID);err!=nil{t.Fatal(err)};defer func(){_,_=pool.Exec(ctx,`DELETE FROM orders WHERE id=$1`,orderID)}()
	var offerID int64;if err:=pool.QueryRow(ctx,`INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,subtotal,total,created_by) VALUES($1,$2,1,'notifying','pickup',100,100,$3) RETURNING id`,orderID,fmt.Sprintf("stage08-%d",unique),customerID).Scan(&offerID);err!=nil{t.Fatal(err)}
	if _,err:=pool.Exec(ctx,`INSERT INTO outbox(recipient,subject,body,shipment_offer_id) VALUES('stage08@example.invalid','Stage 08','body',$1)`,offerID);err!=nil{t.Fatal(err)}
	sender:=&stage08Sender{};logger:=slog.New(slog.NewTextHandler(io.Discard,nil));first:=NewLetterWorker(pool,sender,logger);second:=NewLetterWorker(pool,sender,logger)
	var workers sync.WaitGroup;workers.Add(2);go func(){defer workers.Done();first.process(ctx)}();go func(){defer workers.Done();second.process(ctx)}();workers.Wait()
	if sender.sent!=1{t.Fatalf("two workers sent %d letters, want 1",sender.sent)}
	var status string;var notified,expires time.Time;if err:=pool.QueryRow(ctx,`SELECT status,notified_at,expires_at FROM shipment_offers WHERE id=$1`,offerID).Scan(&status,&notified,&expires);err!=nil{t.Fatal(err)};if status!="offered"{t.Fatalf("status=%s",status)};if delta:=expires.Sub(notified);delta<48*time.Hour-time.Second||delta>48*time.Hour+time.Second{t.Fatalf("expiry delta=%s",delta)}
	first.process(ctx);var expiresAgain time.Time;_ = pool.QueryRow(ctx,`SELECT expires_at FROM shipment_offers WHERE id=$1`,offerID).Scan(&expiresAgain);if !expiresAgain.Equal(expires){t.Fatal("repeat processing extended offer expiry")}
}

func TestStage08FailedSMTPDoesNotStartExpiry(t *testing.T){
	databaseURL:=os.Getenv("CRM_TEST_DATABASE_URL");if databaseURL==""{t.Skip("CRM_TEST_DATABASE_URL is not set")};ctx:=context.Background();pool,err:=pgxpool.New(ctx,databaseURL);if err!=nil{t.Fatal(err)};defer pool.Close();unique:=time.Now().UnixNano();var customerID,orderID int64;_ = pool.QueryRow(ctx,`SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID);_ = pool.QueryRow(ctx,`INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,$2,'Stage 08','+70000000000','stage08@example.invalid','pickup',0,100,100,'online','pending','confirmed') RETURNING id`,fmt.Sprintf("CRM-S08-F-%d",unique),customerID).Scan(&orderID);defer func(){_,_=pool.Exec(ctx,`DELETE FROM orders WHERE id=$1`,orderID)}();var offerID int64;_ = pool.QueryRow(ctx,`INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,subtotal,total,created_by) VALUES($1,$2,1,'notifying','pickup',100,100,$3) RETURNING id`,orderID,fmt.Sprintf("stage08-f-%d",unique),customerID).Scan(&offerID);_,_=pool.Exec(ctx,`INSERT INTO outbox(recipient,subject,body,shipment_offer_id) VALUES('stage08@example.invalid','Stage 08','body',$1)`,offerID)
	NewLetterWorker(pool,&stage08Sender{fail:true},slog.New(slog.NewTextHandler(io.Discard,nil))).process(ctx);var hasExpiry bool;_ = pool.QueryRow(ctx,`SELECT expires_at IS NOT NULL FROM shipment_offers WHERE id=$1`,offerID).Scan(&hasExpiry);if hasExpiry{t.Fatal("failed SMTP started expiry")}
}
