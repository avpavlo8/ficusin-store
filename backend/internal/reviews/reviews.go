package reviews

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotPurchased = errors.New("completed purchase required")
var ErrAlreadyReviewed = errors.New("already reviewed")

type Input struct { Rating int `json:"rating"`; Text string `json:"text"`; Photos []MediaInput `json:"photos"` }
type MediaInput struct { ContentType string `json:"contentType"`; Data string `json:"data"` }
type ModerationMedia struct { URL string `json:"url"`; ContentType string `json:"contentType"` }
type ModerationItem struct { ID int64 `json:"id"`; Product string `json:"product"`; Author string `json:"author"`; Rating int `json:"rating"`; Text string `json:"text"`; Status string `json:"status"`; CreatedAt string `json:"createdAt"`; Media []ModerationMedia `json:"media"` }
type AccountItem struct { ID int64 `json:"id"`; Product string `json:"product"`; Slug string `json:"slug"`; Rating int `json:"rating"`; Text string `json:"text"`; Status string `json:"status"`; CreatedAt string `json:"createdAt"` }
type mediaStorage interface { Configured() bool; Put(context.Context,string,[]byte,string) error; Get(context.Context,string)([]byte,error) }
type Store struct { pool *pgxpool.Pool; media mediaStorage }
func NewStore(pool *pgxpool.Pool, media ...mediaStorage) *Store { store:=&Store{pool:pool};if len(media)>0&&media[0]!=nil&&media[0].Configured(){store.media=media[0]};return store }

func (s *Store) Create(ctx context.Context, customerID int64, slug string, input Input) (int64, error) {
	if input.Rating < 1 || input.Rating > 5 || len(strings.TrimSpace(input.Text)) < 10 || len(input.Text) > 3000 || len(input.Photos) > 4 { return 0, errors.New("invalid review") }
	tx, err := s.pool.Begin(ctx); if err != nil { return 0, err }; defer func(){ _ = tx.Rollback(ctx) }()
	var productID, orderID, variantID int64
	var purchasedSKU string
	err = tx.QueryRow(ctx, `SELECT p.id,o.id,oi.variant_id,oi.sku FROM products p JOIN order_items oi ON oi.product_id=p.id JOIN orders o ON o.id=oi.order_id WHERE p.product_code::TEXT=$1 AND o.customer_id=$2 AND o.status='completed' ORDER BY o.created_at DESC LIMIT 1`, slug, customerID).Scan(&productID,&orderID,&variantID,&purchasedSKU)
	if errors.Is(err, pgx.ErrNoRows) { return 0, ErrNotPurchased }; if err != nil { return 0, fmt.Errorf("verify purchase: %w",err) }
	// Serialize submissions for this customer/product pair and count hidden reviews too:
	// hiding a review in the admin panel must not create another review allowance.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text || ':' || $2::text, 0))`, productID, customerID); err != nil { return 0, err }
	var alreadyReviewed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_reviews WHERE product_id=$1 AND customer_id=$2)`, productID, customerID).Scan(&alreadyReviewed); err != nil { return 0, err }
	if alreadyReviewed { return 0, ErrAlreadyReviewed }
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO product_reviews(product_id,customer_id,order_id,variant_id,purchased_sku,rating,body,status) VALUES($1,$2,$3,$4,$5,$6,$7,'published') ON CONFLICT DO NOTHING RETURNING id`, productID,customerID,orderID,variantID,purchasedSKU,input.Rating,strings.TrimSpace(input.Text)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) { return 0, ErrAlreadyReviewed }; if err != nil { return 0, err }
	videoCount := 0
	for index, media := range input.Photos {
		isVideo := media.ContentType == "video/mp4" || media.ContentType == "video/webm"
		isImage := media.ContentType == "image/jpeg" || media.ContentType == "image/png" || media.ContentType == "image/webp"
		if !isImage && !isVideo { return 0, errors.New("invalid media type") }
		if isVideo { videoCount++; if videoCount > 1 { return 0, errors.New("only one video is allowed") } }
		data, decodeErr := base64.StdEncoding.DecodeString(media.Data)
		limit := 5 * 1024 * 1024
		if isVideo { limit = 20 * 1024 * 1024 }
		if decodeErr != nil || len(data) == 0 || len(data) > limit { return 0, errors.New("invalid media size") }
		if !validMediaSignature(media.ContentType, data) { return 0, errors.New("media content does not match type") }
		var objectKey *string
		var databaseData []byte = data
		if s.media != nil { key, keyErr := reviewMediaKey(id, media.ContentType); if keyErr != nil { return 0, keyErr }; if keyErr = s.media.Put(ctx,key,data,media.ContentType); keyErr != nil { return 0,keyErr }; objectKey=&key; databaseData=nil }
		if _, err = tx.Exec(ctx, `INSERT INTO product_review_photos(review_id,content_type,image,object_key,sort_order) VALUES($1,$2,$3,$4,$5)`, id, media.ContentType, databaseData, objectKey, index); err != nil { return 0, err }
	}
	if err=tx.Commit(ctx); err != nil{return 0,err}; return id,nil
}

func reviewMediaKey(reviewID int64, contentType string) (string,error) { token:=make([]byte,16);if _,err:=rand.Read(token);err!=nil{return "",err};extension:=map[string]string{"image/jpeg":"jpg","image/png":"png","image/webp":"webp","video/mp4":"mp4","video/webm":"webm"}[contentType];return fmt.Sprintf("reviews/%d/%s.%s",reviewID,hex.EncodeToString(token),extension),nil }

func validMediaSignature(contentType string, data []byte) bool {
	if strings.HasPrefix(contentType, "image/") { return http.DetectContentType(data) == contentType }
	if contentType == "video/mp4" { return len(data) >= 12 && string(data[4:8]) == "ftyp" }
	if contentType == "video/webm" { return len(data) >= 4 && data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3 }
	return false
}

func (s *Store) mediaData(ctx context.Context,id int64,status string)(string,[]byte,error){var kind string;var data []byte;var key *string;err:=s.pool.QueryRow(ctx,`SELECT p.content_type,p.image,p.object_key FROM product_review_photos p JOIN product_reviews r ON r.id=p.review_id WHERE p.id=$1 AND r.status=$2`,id,status).Scan(&kind,&data,&key);if err!=nil{return "",nil,err};if key!=nil&&*key!=""{if s.media==nil{return "",nil,errors.New("media storage unavailable")};data,err=s.media.Get(ctx,*key)};return kind,data,err}
func (s *Store) Photo(ctx context.Context, id int64) (string,[]byte,error) { return s.mediaData(ctx,id,"published") }
func (s *Store) ModerationMedia(ctx context.Context, id int64) (string,[]byte,error) { var status string;err:=s.pool.QueryRow(ctx,`SELECT r.status FROM product_reviews r JOIN product_review_photos p ON p.review_id=r.id WHERE p.id=$1`,id).Scan(&status);if err!=nil{return "",nil,err};return s.mediaData(ctx,id,status) }

func (s *Store) Pending(ctx context.Context) ([]ModerationItem,error) { rows,err:=s.pool.Query(ctx,`SELECT r.id,p.name,COALESCE(NULLIF(c.full_name,''),'Покупатель'),r.rating,r.body,r.status,r.created_at::text FROM product_reviews r JOIN products p ON p.id=r.product_id JOIN customers c ON c.id=r.customer_id ORDER BY r.created_at DESC`);if err!=nil{return nil,err};defer rows.Close();result:=[]ModerationItem{};for rows.Next(){var item ModerationItem;if err:=rows.Scan(&item.ID,&item.Product,&item.Author,&item.Rating,&item.Text,&item.Status,&item.CreatedAt);err!=nil{return nil,err}; mediaRows,_:=s.pool.Query(ctx,`SELECT '/api/v1/admin/review-media/' || id,content_type FROM product_review_photos WHERE review_id=$1 ORDER BY sort_order,id`,item.ID);for mediaRows!=nil&&mediaRows.Next(){var media ModerationMedia;_ = mediaRows.Scan(&media.URL,&media.ContentType);item.Media=append(item.Media,media)};if mediaRows!=nil{mediaRows.Close()};result=append(result,item)};return result,rows.Err() }
func (s *Store) Moderate(ctx context.Context,id,actorID int64,status string) error { if status!="published"&&status!="rejected"{return errors.New("invalid status")};tag,err:=s.pool.Exec(ctx,`UPDATE product_reviews SET status=$2,moderated_at=CURRENT_TIMESTAMP,moderated_by=$3 WHERE id=$1 AND status<>$2`,id,status,actorID);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil }
func (s *Store) Mine(ctx context.Context,customerID int64)([]AccountItem,error){rows,err:=s.pool.Query(ctx,`SELECT r.id,p.name,p.product_code::TEXT,r.rating,r.body,r.status,r.created_at::text FROM product_reviews r JOIN products p ON p.id=r.product_id WHERE r.customer_id=$1 ORDER BY r.created_at DESC`,customerID);if err!=nil{return nil,err};defer rows.Close();items:=[]AccountItem{};for rows.Next(){var item AccountItem;if err:=rows.Scan(&item.ID,&item.Product,&item.Slug,&item.Rating,&item.Text,&item.Status,&item.CreatedAt);err!=nil{return nil,err};items=append(items,item)};return items,rows.Err()}
func (s *Store) UpdateMine(ctx context.Context,customerID,id int64,rating int,text string)error{body:=strings.TrimSpace(text);if rating<1||rating>5||len(body)<10||len(body)>3000{return errors.New("invalid review")};tag,err:=s.pool.Exec(ctx,`UPDATE product_reviews SET rating=$3,body=$4 WHERE id=$1 AND customer_id=$2 AND status='published'`,id,customerID,rating,body);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil}
