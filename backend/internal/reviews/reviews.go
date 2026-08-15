package reviews

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotPurchased = errors.New("completed purchase required")
var ErrAlreadyReviewed = errors.New("already reviewed")

type Input struct { Rating int `json:"rating"`; Text string `json:"text"`; Photos []PhotoInput `json:"photos"` }
type PhotoInput struct { ContentType string `json:"contentType"`; Data string `json:"data"` }
type ModerationItem struct { ID int64 `json:"id"`; Product string `json:"product"`; Author string `json:"author"`; Rating int `json:"rating"`; Text string `json:"text"`; CreatedAt string `json:"createdAt"` }
type Store struct { pool *pgxpool.Pool }
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, customerID int64, slug string, input Input) (int64, error) {
	if input.Rating < 1 || input.Rating > 5 || len(strings.TrimSpace(input.Text)) < 10 || len(input.Text) > 3000 || len(input.Photos) > 3 { return 0, errors.New("invalid review") }
	tx, err := s.pool.Begin(ctx); if err != nil { return 0, err }; defer func(){ _ = tx.Rollback(ctx) }()
	var productID, orderID int64
	err = tx.QueryRow(ctx, `SELECT p.id,o.id FROM products p JOIN order_items oi ON oi.product_id=p.slug JOIN orders o ON o.id=oi.order_id WHERE p.slug=$1 AND o.customer_id=$2 AND o.status='completed' ORDER BY o.created_at DESC LIMIT 1`, slug, customerID).Scan(&productID,&orderID)
	if errors.Is(err, pgx.ErrNoRows) { return 0, ErrNotPurchased }; if err != nil { return 0, fmt.Errorf("verify purchase: %w",err) }
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO product_reviews(product_id,customer_id,order_id,rating,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id`, productID,customerID,orderID,input.Rating,strings.TrimSpace(input.Text)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) { return 0, ErrAlreadyReviewed }; if err != nil { return 0, err }
	for index, photo := range input.Photos { if photo.ContentType != "image/jpeg" && photo.ContentType != "image/png" && photo.ContentType != "image/webp" { return 0, errors.New("invalid photo type") }; data, err := base64.StdEncoding.DecodeString(photo.Data); if err != nil || len(data)==0 || len(data)>5*1024*1024 { return 0, errors.New("invalid photo") }; detected:=http.DetectContentType(data); if detected!=photo.ContentType { return 0, errors.New("photo content does not match type") }; if _,err=tx.Exec(ctx,`INSERT INTO product_review_photos(review_id,content_type,image,sort_order) VALUES($1,$2,$3,$4)`,id,photo.ContentType,data,index); err != nil{return 0,err} }
	if err=tx.Commit(ctx); err != nil{return 0,err}; return id,nil
}

func (s *Store) Photo(ctx context.Context, id int64) (string,[]byte,error) { var kind string; var data []byte; err:=s.pool.QueryRow(ctx,`SELECT p.content_type,p.image FROM product_review_photos p JOIN product_reviews r ON r.id=p.review_id WHERE p.id=$1 AND r.status='published'`,id).Scan(&kind,&data); return kind,data,err }

func (s *Store) Pending(ctx context.Context) ([]ModerationItem,error) { rows,err:=s.pool.Query(ctx,`SELECT r.id,p.name,COALESCE(NULLIF(c.full_name,''),'Покупатель'),r.rating,r.body,r.created_at::text FROM product_reviews r JOIN products p ON p.id=r.product_id JOIN customers c ON c.id=r.customer_id WHERE r.status='pending' ORDER BY r.created_at`);if err!=nil{return nil,err};defer rows.Close();result:=[]ModerationItem{};for rows.Next(){var item ModerationItem;if err:=rows.Scan(&item.ID,&item.Product,&item.Author,&item.Rating,&item.Text,&item.CreatedAt);err!=nil{return nil,err};result=append(result,item)};return result,rows.Err() }
func (s *Store) Moderate(ctx context.Context,id,actorID int64,status string) error { if status!="published"&&status!="rejected"{return errors.New("invalid status")};tag,err:=s.pool.Exec(ctx,`UPDATE product_reviews SET status=$2,moderated_at=CURRENT_TIMESTAMP,moderated_by=$3 WHERE id=$1 AND status='pending'`,id,status,actorID);if err!=nil{return err};if tag.RowsAffected()!=1{return pgx.ErrNoRows};return nil }
