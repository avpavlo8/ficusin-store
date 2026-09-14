package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ShipmentOfferLineInput struct {
	OrderItemID int64 `json:"orderItemId"`
	Quantity int `json:"quantity"`
}

type ShipmentBoxInput struct {
	LengthCM int `json:"lengthCm"`
	WidthCM int `json:"widthCm"`
	HeightCM int `json:"heightCm"`
	WeightGrams int `json:"weightGrams"`
	Contents []ShipmentBoxContent `json:"contents"`
}

type ShipmentBoxContent struct { OrderItemID int64 `json:"orderItemId"`; Quantity int `json:"quantity"` }

type ShipmentOfferInput struct {
	Items []ShipmentOfferLineInput `json:"items"`
	Boxes []ShipmentBoxInput `json:"boxes"`
	DeliveryFee float64 `json:"deliveryFee"`
	CDEKTariffCode *int `json:"cdekTariffCode"`
	CDEKTariffName string `json:"cdekTariffName"`
	ManagerNote string `json:"managerNote"`
	PackagingRequired bool `json:"packagingRequired"`
}

type ShipmentOfferItem struct {
	OrderItemID int64 `json:"orderItemId"`
	SKU string `json:"sku"`
	ProductName string `json:"productName"`
	UnitPrice float64 `json:"unitPrice"`
	OriginalUnitPrice float64 `json:"originalUnitPrice"`
	Quantity int `json:"quantity"`
}

type ShipmentOfferBox struct {
	BoxNo int `json:"boxNo"`
	LengthCM int `json:"lengthCm"`
	WidthCM int `json:"widthCm"`
	HeightCM int `json:"heightCm"`
	WeightGrams int `json:"weightGrams"`
	Contents []ShipmentBoxContent `json:"contents"`
}

type ShipmentOffer struct {
	ID int64 `json:"id"`
	Version int `json:"version"`
	Status string `json:"status"`
	DeliveryFee float64 `json:"deliveryFee"`
	Subtotal float64 `json:"subtotal"`
	Total float64 `json:"total"`
	CDEKTariffCode *int `json:"cdekTariffCode,omitempty"`
	CDEKTariffName string `json:"cdekTariffName"`
	ManagerNote string `json:"managerNote"`
	NotifiedAt *time.Time `json:"notifiedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Items []ShipmentOfferItem `json:"items"`
	Boxes []ShipmentOfferBox `json:"boxes"`
}

func shipmentToken() (string,error) { b:=make([]byte,24);if _,err:=rand.Read(b);err!=nil{return "",err};return hex.EncodeToString(b),nil }

func validateShipmentBoxes(boxes []ShipmentBoxInput) error {
	if len(boxes)==0{return errors.New("укажите фактические коробки отправки")}
	for _,box:=range boxes{if box.LengthCM<=0||box.WidthCM<=0||box.HeightCM<=0||box.WeightGrams<=0{return errors.New("размеры и вес каждой коробки должны быть больше нуля")}}
	return nil
}

func (repository *PostgresRepository) CreateShipmentOffer(ctx context.Context,actor Actor,orderID int64,input ShipmentOfferInput)(ShipmentOffer,error){
	if !Can(actor.Role,PermissionOrdersEdit){return ShipmentOffer{},ErrForbidden}
	if len(input.Items)==0{return ShipmentOffer{},errors.New("выберите хотя бы одну позицию")}
	if input.DeliveryFee<0||input.DeliveryFee>100000{return ShipmentOffer{},errors.New("стоимость доставки вне разумных пределов")}
	if !input.PackagingRequired { if err:=validateShipmentBoxes(input.Boxes);err!=nil{return ShipmentOffer{},err} }
	token,err:=shipmentToken();if err!=nil{return ShipmentOffer{},err}
	tx,err:=repository.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return ShipmentOffer{},err};defer func(){_ = tx.Rollback(ctx)}()
	var revision int;var status,delivery,address,email string;var customerID *int64
	if err:=tx.QueryRow(ctx,`SELECT shipment_revision,status,delivery_method,address,COALESCE(email,''),customer_id FROM orders WHERE id=$1 FOR UPDATE`,orderID).Scan(&revision,&status,&delivery,&address,&email,&customerID);err!=nil{return ShipmentOffer{},err}
	if status=="cancelled"||status=="completed"||status=="shipped"{return ShipmentOffer{},errors.New("для закрытого или отправленного заказа нельзя создать предложение")}
	discountBPS:=0;if customerID!=nil{_ = tx.QueryRow(ctx,`SELECT COALESCE(retail_discount_bps,0) FROM customers WHERE id=$1`,*customerID).Scan(&discountBPS)};if discountBPS<0{discountBPS=0};if discountBPS>9000{discountBPS=9000}
	if delivery=="cdek"&&!input.PackagingRequired&&input.CDEKTariffCode==nil{return ShipmentOffer{},errors.New("пересчитайте тариф СДЭК для этого состава коробок")}
	var offerID int64;offerStatus:="draft";if input.PackagingRequired{offerStatus="packaging_required"}
	if err:=tx.QueryRow(ctx,`INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,address_snapshot,delivery_fee,cdek_tariff_code,cdek_tariff_name,manager_note,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,orderID,token,revision,offerStatus,delivery,address,input.DeliveryFee,input.CDEKTariffCode,strings.TrimSpace(input.CDEKTariffName),strings.TrimSpace(input.ManagerNote),actor.CustomerID).Scan(&offerID);err!=nil{return ShipmentOffer{},fmt.Errorf("create shipment offer: %w",err)}
	seen:=map[int64]bool{};selected:=map[int64]int{};subtotal:=0.0
	for _,requested:=range input.Items{
		if requested.OrderItemID<=0||requested.Quantity<=0||seen[requested.OrderItemID]{return ShipmentOffer{},errors.New("некорректный состав предложения")};seen[requested.OrderItemID]=true
		var variantID *int64;var sku,name string;var originalPrice float64;var currentPriceMinor *int64;var ordered,already int
		if err:=tx.QueryRow(ctx,`SELECT oi.variant_id,COALESCE(oi.sku,''),oi.product_name,oi.unit_price::DOUBLE PRECISION,pv.base_price_minor,oi.quantity,COALESCE((SELECT SUM(soi.quantity) FROM shipment_offer_items soi JOIN shipment_offers so ON so.id=soi.shipment_offer_id WHERE soi.order_item_id=oi.id AND so.status IN ('paid','shipping','shipped','ready','completed')),0)::INTEGER FROM order_items oi LEFT JOIN product_variants pv ON pv.id=oi.variant_id WHERE oi.id=$1 AND oi.order_id=$2`,requested.OrderItemID,orderID).Scan(&variantID,&sku,&name,&originalPrice,&currentPriceMinor,&ordered,&already);err!=nil{return ShipmentOffer{},errors.New("позиция заказа не найдена")}
		if requested.Quantity>ordered-already{return ShipmentOffer{},fmt.Errorf("для %s доступно к следующей отправке %d",name,ordered-already)}
		price:=originalPrice;if currentPriceMinor!=nil&&*currentPriceMinor>0{minor:=*currentPriceMinor;if discountBPS>0{minor=(minor*int64(10000-discountBPS)+5000)/10000};price=float64(minor)/100}
		if _,err:=tx.Exec(ctx,`INSERT INTO shipment_offer_items(shipment_offer_id,order_item_id,variant_id,sku,product_name,unit_price,original_unit_price,quantity) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,offerID,requested.OrderItemID,variantID,sku,name,price,originalPrice,requested.Quantity);err!=nil{return ShipmentOffer{},err};selected[requested.OrderItemID]=requested.Quantity;subtotal+=price*float64(requested.Quantity)
	}
	packed:=map[int64]int{}
	for index,box:=range input.Boxes{for _,content:=range box.Contents{if selected[content.OrderItemID]==0||content.Quantity<=0{return ShipmentOffer{},fmt.Errorf("некорректный состав коробки %d",index+1)};packed[content.OrderItemID]+=content.Quantity};contents,_:=json.Marshal(box.Contents);if _,err:=tx.Exec(ctx,`INSERT INTO shipment_offer_boxes(shipment_offer_id,box_no,length_cm,width_cm,height_cm,weight_grams,contents) VALUES($1,$2,$3,$4,$5,$6,$7)`,offerID,index+1,box.LengthCM,box.WidthCM,box.HeightCM,box.WeightGrams,contents);err!=nil{return ShipmentOffer{},err}}
	if !input.PackagingRequired{for id,quantity:=range selected{if packed[id]!=quantity{return ShipmentOffer{},errors.New("распределите по коробкам каждую выбранную единицу товара")}}}
	fingerprint,_:=json.Marshal(struct{Address string;Revision int;Items []ShipmentOfferLineInput;Boxes []ShipmentBoxInput}{address,revision,input.Items,input.Boxes})
	if _,err:=tx.Exec(ctx,`UPDATE shipment_offers SET subtotal=$2,total=$2+delivery_fee,quote_fingerprint=md5($3),updated_at=CURRENT_TIMESTAMP WHERE id=$1`,offerID,subtotal,string(fingerprint));err!=nil{return ShipmentOffer{},err}
	if err:=insertAudit(ctx,tx,actor,"order.shipment_offer.create","shipment_offer",fmt.Sprint(offerID),nil,map[string]any{"orderId":orderID,"status":offerStatus,"subtotal":subtotal,"deliveryFee":input.DeliveryFee});err!=nil{return ShipmentOffer{},err}
	if err:=tx.Commit(ctx);err!=nil{return ShipmentOffer{},err};return repository.ShipmentOffer(ctx,offerID)
}

func (repository *PostgresRepository) SendShipmentOffer(ctx context.Context,actor Actor,offerID int64)(ShipmentOffer,error){
	if !Can(actor.Role,PermissionOrdersEdit){return ShipmentOffer{},ErrForbidden}
	tx,err:=repository.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return ShipmentOffer{},err};defer func(){_ = tx.Rollback(ctx)}()
	var orderID int64;var offerStatus,email,number,addressSnapshot,address string;var revision,offerRevision,boxes int
	if err:=tx.QueryRow(ctx,`SELECT so.order_id,so.status,COALESCE(o.email,''),o.order_number,so.address_snapshot,o.address,so.order_revision,o.shipment_revision,(SELECT COUNT(*) FROM shipment_offer_boxes WHERE shipment_offer_id=so.id) FROM shipment_offers so JOIN orders o ON o.id=so.order_id WHERE so.id=$1 FOR UPDATE`,offerID).Scan(&orderID,&offerStatus,&email,&number,&addressSnapshot,&address,&offerRevision,&revision,&boxes);err!=nil{return ShipmentOffer{},err}
	if offerStatus=="offered"||offerStatus=="payment_pending"||offerStatus=="paid"{if err:=tx.Commit(ctx);err!=nil{return ShipmentOffer{},err};return repository.ShipmentOffer(ctx,offerID)}
	if offerStatus!="draft"{return ShipmentOffer{},errors.New("сначала завершите упаковку и пересчитайте доставку")}
	if email==""{return ShipmentOffer{},errors.New("у клиента не указана почта — срок оплаты нельзя запустить без подтверждаемой отправки")}
	if offerRevision!=revision||addressSnapshot!=address{return ShipmentOffer{},errors.New("заказ изменился — создайте предложение заново")}
	if boxes==0{return ShipmentOffer{},errors.New("состав коробок не заполнен")}
	link:="/account/orders/"+number
	subject:="Товар по вашему заказу поступил"
	body:="Товар по вашему заказу поступил. Теперь можно оплатить выбранную отправку в течение 48 часов.\n\nОткрыть заказ: "+link
	command,err:=tx.Exec(ctx,`INSERT INTO outbox(recipient,subject,body,shipment_offer_id) VALUES($1,$2,$3,$4) ON CONFLICT (shipment_offer_id) WHERE shipment_offer_id IS NOT NULL AND cancelled_at IS NULL DO NOTHING`,email,subject,body,offerID);if err!=nil{return ShipmentOffer{},err}
	if command.RowsAffected()>0{if _,err:=tx.Exec(ctx,`UPDATE shipment_offers SET status='notifying',updated_at=CURRENT_TIMESTAMP WHERE id=$1`,offerID);err!=nil{return ShipmentOffer{},err}}
	if err:=insertAudit(ctx,tx,actor,"order.shipment_offer.notify","shipment_offer",fmt.Sprint(offerID),map[string]any{"status":offerStatus},map[string]any{"status":"notifying"});err!=nil{return ShipmentOffer{},err}
	if err:=tx.Commit(ctx);err!=nil{return ShipmentOffer{},err}
	if repository.notifier!=nil{var customerID *int64;_ = repository.pool.QueryRow(ctx,`SELECT customer_id FROM orders WHERE id=$1`,orderID).Scan(&customerID);if customerID!=nil{_ = repository.notifier.NotifyOrderStatus(ctx,*customerID,number,"shipment_offer")}}
	return repository.ShipmentOffer(ctx,offerID)
}

func (repository *PostgresRepository) ShipmentOffer(ctx context.Context,id int64)(ShipmentOffer,error){
	var result ShipmentOffer
	err:=repository.pool.QueryRow(ctx,`SELECT id,version,status,delivery_fee::DOUBLE PRECISION,subtotal::DOUBLE PRECISION,total::DOUBLE PRECISION,cdek_tariff_code,cdek_tariff_name,manager_note,notified_at,expires_at FROM shipment_offers WHERE id=$1`,id).Scan(&result.ID,&result.Version,&result.Status,&result.DeliveryFee,&result.Subtotal,&result.Total,&result.CDEKTariffCode,&result.CDEKTariffName,&result.ManagerNote,&result.NotifiedAt,&result.ExpiresAt);if err!=nil{return result,err}
	items,err:=repository.pool.Query(ctx,`SELECT order_item_id,sku,product_name,unit_price::DOUBLE PRECISION,COALESCE(original_unit_price,unit_price)::DOUBLE PRECISION,quantity FROM shipment_offer_items WHERE shipment_offer_id=$1 ORDER BY id`,id);if err!=nil{return result,err};defer items.Close();result.Items=[]ShipmentOfferItem{};for items.Next(){var item ShipmentOfferItem;if err:=items.Scan(&item.OrderItemID,&item.SKU,&item.ProductName,&item.UnitPrice,&item.OriginalUnitPrice,&item.Quantity);err!=nil{return result,err};result.Items=append(result.Items,item)};if err:=items.Err();err!=nil{return result,err}
	rows,err:=repository.pool.Query(ctx,`SELECT box_no,length_cm,width_cm,height_cm,weight_grams,contents FROM shipment_offer_boxes WHERE shipment_offer_id=$1 ORDER BY box_no`,id);if err!=nil{return result,err};defer rows.Close();result.Boxes=[]ShipmentOfferBox{};for rows.Next(){var box ShipmentOfferBox;var raw []byte;if err:=rows.Scan(&box.BoxNo,&box.LengthCM,&box.WidthCM,&box.HeightCM,&box.WeightGrams,&raw);err!=nil{return result,err};_ = json.Unmarshal(raw,&box.Contents);result.Boxes=append(result.Boxes,box)};return result,rows.Err()
}

func (repository *PostgresRepository) ShipmentOffers(ctx context.Context,orderID int64)([]ShipmentOffer,error){rows,err:=repository.pool.Query(ctx,`SELECT id FROM shipment_offers WHERE order_id=$1 ORDER BY id DESC`,orderID);if err!=nil{return nil,err};ids:=[]int64{};for rows.Next(){var id int64;if err:=rows.Scan(&id);err!=nil{rows.Close();return nil,err};ids=append(ids,id)};rows.Close();result:=[]ShipmentOffer{};for _,id:=range ids{item,err:=repository.ShipmentOffer(ctx,id);if err!=nil{return nil,err};result=append(result,item)};return result,nil}
