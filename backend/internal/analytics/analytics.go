package analytics

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxBatch = 25

var ErrInvalid = errors.New("invalid analytics event")

var publicEvents = map[string]bool{
	"page_view": true, "view_item_list": true, "select_item": true,
	"search": true, "filter": true, "view_item": true,
	"add_to_cart": true, "remove_from_cart": true, "view_cart": true,
	"begin_checkout": true, "checkout_step": true,
	"add_shipping_info": true, "add_payment_info": true,
	"checkout_error": true, "payment_redirect": true,
}

type Event struct {
	EventID     string         `json:"eventId"`
	Name        string         `json:"name"`
	VisitorID   string         `json:"visitorId"`
	SessionID   string         `json:"sessionId"`
	OccurredAt  time.Time      `json:"occurredAt"`
	PagePath    string         `json:"pagePath"`
	PageTitle   string         `json:"pageTitle"`
	Referrer    string         `json:"referrer"`
	Source      string         `json:"source"`
	Medium      string         `json:"medium"`
	Campaign    string         `json:"campaign"`
	Content     string         `json:"content"`
	Term        string         `json:"term"`
	ProductCode string         `json:"productCode"`
	SKU         string         `json:"sku"`
	OrderNumber string         `json:"orderNumber"`
	Value       float64        `json:"value"`
	Quantity    int            `json:"quantity"`
	Properties  map[string]any `json:"properties"`
}

type Attribution struct {
	VisitorID string `json:"visitorId"`
	SessionID string `json:"sessionId"`
	Source    string `json:"source"`
	Medium    string `json:"medium"`
	Campaign  string `json:"campaign"`
	Content   string `json:"content"`
	Term      string `json:"term"`
	Referrer  string `json:"referrer"`
}

type Summary struct {
	Period         int          `json:"period"`
	Visitors       int          `json:"visitors"`
	Sessions       int          `json:"sessions"`
	ProductViews   int          `json:"productViews"`
	CartAdds       int          `json:"cartAdds"`
	Checkouts      int          `json:"checkouts"`
	Orders         int          `json:"orders"`
	Revenue        float64      `json:"revenue"`
	AbandonedCarts int          `json:"abandonedCarts"`
	CheckoutErrors int          `json:"checkoutErrors"`
	Funnel         []FunnelStep `json:"funnel"`
	Sources        []SourceRow  `json:"sources"`
	Products       []ProductRow `json:"products"`
	Searches       []SearchRow  `json:"searches"`
	Daily          []DailyRow   `json:"daily"`
}

type FunnelStep struct {
	Name     string `json:"name"`
	Sessions int    `json:"sessions"`
}
type SourceRow struct {
	Source   string  `json:"source"`
	Sessions int     `json:"sessions"`
	Orders   int     `json:"orders"`
	Revenue  float64 `json:"revenue"`
}
type ProductRow struct {
	ProductCode string  `json:"productCode"`
	Views       int     `json:"views"`
	CartAdds    int     `json:"cartAdds"`
	Orders      int     `json:"orders"`
	Revenue     float64 `json:"revenue"`
}
type SearchRow struct {
	Query       string `json:"query"`
	Searches    int    `json:"searches"`
	ZeroResults int    `json:"zeroResults"`
}
type DailyRow struct {
	Date     string  `json:"date"`
	Sessions int     `json:"sessions"`
	Orders   int     `json:"orders"`
	Revenue  float64 `json:"revenue"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func validUUID(value string) bool { return uuidPattern.MatchString(value) }

func newUUID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func clean(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func normalize(event Event) (Event, error) {
	if !publicEvents[event.Name] {
		return Event{}, ErrInvalid
	}
	if !validUUID(event.EventID) || !validUUID(event.VisitorID) || !validUUID(event.SessionID) {
		return Event{}, ErrInvalid
	}
	if event.OccurredAt.IsZero() || event.OccurredAt.Before(time.Now().Add(-48*time.Hour)) || event.OccurredAt.After(time.Now().Add(10*time.Minute)) {
		event.OccurredAt = time.Now()
	}
	event.PagePath = clean(event.PagePath, 500)
	event.PageTitle = clean(event.PageTitle, 300)
	event.Referrer = clean(event.Referrer, 1000)
	event.Source = clean(event.Source, 120)
	event.Medium = clean(event.Medium, 120)
	event.Campaign = clean(event.Campaign, 200)
	event.Content = clean(event.Content, 200)
	event.Term = clean(event.Term, 200)
	event.ProductCode = clean(event.ProductCode, 160)
	event.SKU = clean(event.SKU, 160)
	if event.Value < 0 || event.Value > 100000000 || event.Quantity < 0 || event.Quantity > 1000 {
		return Event{}, ErrInvalid
	}
	encoded, err := json.Marshal(event.Properties)
	if err != nil || len(encoded) > 8192 {
		return Event{}, ErrInvalid
	}
	return event, nil
}

func (store *Store) Record(ctx context.Context, customerID *int64, events []Event) error {
	if len(events) == 0 || len(events) > MaxBatch {
		return ErrInvalid
	}
	batch := &pgx.Batch{}
	for _, input := range events {
		event, err := normalize(input)
		if err != nil {
			return err
		}
		properties, _ := json.Marshal(event.Properties)
		batch.Queue(`
			INSERT INTO analytics_events(event_id,event_name,visitor_id,session_id,customer_id,occurred_at,page_path,page_title,referrer,source,medium,campaign,content,term,product_code,sku,value,quantity,properties,trusted)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,0)
			ON CONFLICT(event_id) DO NOTHING`, event.EventID, event.Name, event.VisitorID, event.SessionID, customerID, event.OccurredAt, event.PagePath, event.PageTitle, event.Referrer, event.Source, event.Medium, event.Campaign, event.Content, event.Term, event.ProductCode, event.SKU, event.Value, event.Quantity, properties)
	}
	results := store.pool.SendBatch(ctx, batch)
	for range events {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("record analytics event: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close analytics batch: %w", err)
	}
	return nil
}

func (store *Store) RecordOrder(ctx context.Context, orderNumber string, total float64, attribution Attribution) error {
	visitor := attribution.VisitorID
	if !validUUID(visitor) {
		visitor = newUUID()
	}
	session := attribution.SessionID
	if !validUUID(session) {
		session = newUUID()
	}
	attribution.VisitorID = visitor
	attribution.SessionID = session
	data, _ := json.Marshal(attribution)
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin order attribution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE orders SET analytics_visitor_id=$2,analytics_session_id=$3,attribution=$4 WHERE order_number=$1`, orderNumber, visitor, session, data); err != nil {
		return fmt.Errorf("attach order attribution: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO analytics_events(event_id,event_name,visitor_id,session_id,occurred_at,page_path,referrer,source,medium,campaign,content,term,order_number,value,properties,trusted)
		VALUES($1,'order_created',$2,$3,CURRENT_TIMESTAMP,'/checkout',$4,$5,$6,$7,$8,$9,$10,$11,'{}'::jsonb,1)
		ON CONFLICT(event_id) DO NOTHING`, newUUID(), visitor, session, clean(attribution.Referrer, 1000), clean(attribution.Source, 120), clean(attribution.Medium, 120), clean(attribution.Campaign, 200), clean(attribution.Content, 200), clean(attribution.Term, 200), orderNumber, total)
	if err != nil {
		return fmt.Errorf("record order attribution: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO analytics_events(event_id,event_name,visitor_id,session_id,occurred_at,page_path,source,medium,campaign,content,term,product_code,sku,order_number,value,quantity,properties,trusted)
		SELECT gen_random_uuid(),'order_item_purchased',$2,$3,CURRENT_TIMESTAMP,'/checkout',$4,$5,$6,$7,$8,p.product_code::text,oi.sku,$1,(oi.unit_price*oi.quantity),oi.quantity,jsonb_build_object('name',oi.product_name),1
		FROM orders o JOIN order_items oi ON oi.order_id=o.id JOIN products p ON p.id=oi.product_id WHERE o.order_number=$1`, orderNumber, visitor, session, clean(attribution.Source, 120), clean(attribution.Medium, 120), clean(attribution.Campaign, 200), clean(attribution.Content, 200), clean(attribution.Term, 200))
	if err != nil {
		return fmt.Errorf("record purchased items: %w", err)
	}
	return tx.Commit(ctx)
}

func (store *Store) Summary(ctx context.Context, days int) (Summary, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	result := Summary{Period: days, Funnel: []FunnelStep{}, Sources: []SourceRow{}, Products: []ProductRow{}, Searches: []SearchRow{}, Daily: []DailyRow{}}
	since := time.Now().AddDate(0, 0, -days)
	err := store.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT visitor_id)::int,COUNT(DISTINCT session_id)::int,COUNT(*) FILTER(WHERE event_name='view_item')::int,COUNT(*) FILTER(WHERE event_name='add_to_cart')::int,COUNT(DISTINCT session_id) FILTER(WHERE event_name='begin_checkout')::int,COUNT(*) FILTER(WHERE event_name='order_created' AND trusted=1)::int,COALESCE(SUM(value) FILTER(WHERE event_name='order_created' AND trusted=1),0)::double precision FROM analytics_events WHERE occurred_at >= $1`, since).Scan(&result.Visitors, &result.Sessions, &result.ProductViews, &result.CartAdds, &result.Checkouts, &result.Orders, &result.Revenue)
	if err != nil {
		return result, fmt.Errorf("analytics totals: %w", err)
	}
	err = store.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE has_cart AND NOT has_order)::int,COALESCE(SUM(errors),0)::int FROM (SELECT session_id,BOOL_OR(event_name='add_to_cart') has_cart,BOOL_OR(event_name='order_created' AND trusted=1) has_order,COUNT(*) FILTER(WHERE event_name='checkout_error') errors FROM analytics_events WHERE occurred_at >= $1 GROUP BY session_id) sessions`, since).Scan(&result.AbandonedCarts, &result.CheckoutErrors)
	if err != nil {
		return result, fmt.Errorf("analytics losses: %w", err)
	}
	rows, err := store.pool.Query(ctx, `SELECT step,COUNT(DISTINCT session_id)::int FROM analytics_events CROSS JOIN LATERAL (VALUES(CASE event_name WHEN 'page_view' THEN 'Посетили сайт' WHEN 'view_item' THEN 'Открыли товар' WHEN 'add_to_cart' THEN 'Добавили в корзину' WHEN 'begin_checkout' THEN 'Начали оформление' WHEN 'order_created' THEN 'Создали заказ' END)) s(step) WHERE occurred_at >= $1 AND step IS NOT NULL GROUP BY step ORDER BY MIN(CASE step WHEN 'Посетили сайт' THEN 1 WHEN 'Открыли товар' THEN 2 WHEN 'Добавили в корзину' THEN 3 WHEN 'Начали оформление' THEN 4 ELSE 5 END)`, since)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item FunnelStep
		if err := rows.Scan(&item.Name, &item.Sessions); err != nil {
			return result, err
		}
		result.Funnel = append(result.Funnel, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows, err = store.pool.Query(ctx, `SELECT COALESCE(NULLIF(source,''),'Прямые заходы'),COUNT(DISTINCT session_id)::int,COUNT(*) FILTER(WHERE event_name='order_created' AND trusted=1)::int,COALESCE(SUM(value) FILTER(WHERE event_name='order_created' AND trusted=1),0)::double precision FROM analytics_events WHERE occurred_at >= $1 GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, since)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SourceRow
		if err := rows.Scan(&item.Source, &item.Sessions, &item.Orders, &item.Revenue); err != nil {
			return result, err
		}
		result.Sources = append(result.Sources, item)
	}
	rows, err = store.pool.Query(ctx, `SELECT product_code,COUNT(*) FILTER(WHERE event_name='view_item')::int,COUNT(*) FILTER(WHERE event_name='add_to_cart')::int,COUNT(*) FILTER(WHERE event_name='order_item_purchased')::int,COALESCE(SUM(value) FILTER(WHERE event_name='order_item_purchased'),0)::double precision FROM analytics_events WHERE occurred_at >= $1 AND product_code<>'' GROUP BY product_code ORDER BY 2 DESC LIMIT 20`, since)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ProductRow
		if err := rows.Scan(&item.ProductCode, &item.Views, &item.CartAdds, &item.Orders, &item.Revenue); err != nil {
			return result, err
		}
		result.Products = append(result.Products, item)
	}
	rows, err = store.pool.Query(ctx, `SELECT COALESCE(properties->>'query',''),COUNT(*)::int,COUNT(*) FILTER(WHERE CASE WHEN COALESCE(properties->>'results','') ~ '^[0-9]+$' THEN (properties->>'results')::int ELSE 0 END=0)::int FROM analytics_events WHERE occurred_at >= $1 AND event_name='search' GROUP BY 1 HAVING COALESCE(properties->>'query','')<>'' ORDER BY 2 DESC LIMIT 20`, since)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SearchRow
		if err := rows.Scan(&item.Query, &item.Searches, &item.ZeroResults); err != nil {
			return result, err
		}
		result.Searches = append(result.Searches, item)
	}
	rows, err = store.pool.Query(ctx, `SELECT TO_CHAR(DATE_TRUNC('day',occurred_at),'YYYY-MM-DD'),COUNT(DISTINCT session_id)::int,COUNT(*) FILTER(WHERE event_name='order_created' AND trusted=1)::int,COALESCE(SUM(value) FILTER(WHERE event_name='order_created' AND trusted=1),0)::double precision FROM analytics_events WHERE occurred_at >= $1 GROUP BY DATE_TRUNC('day',occurred_at) ORDER BY DATE_TRUNC('day',occurred_at)`, since)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item DailyRow
		if err := rows.Scan(&item.Date, &item.Sessions, &item.Orders, &item.Revenue); err != nil {
			return result, err
		}
		result.Daily = append(result.Daily, item)
	}
	return result, rows.Err()
}
