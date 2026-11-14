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
	Value       float64        `json:"value"`
	Quantity    int            `json:"quantity"`
	Properties  map[string]any `json:"properties"`
}

type Attribution struct {
	AnalyticsAllowed bool   `json:"analyticsAllowed"`
	MetrikaClientID  string `json:"metrikaClientId,omitempty"`
	// These fields are always replaced by RecordOrder; the browser cannot claim server ownership.
	MetrikaServer    bool   `json:"metrikaServer,omitempty"`
	MetrikaCounterID string `json:"metrikaCounterId,omitempty"`
	VisitorID        string `json:"visitorId"`
	SessionID        string `json:"sessionId"`
	Source           string `json:"source"`
	Medium           string `json:"medium"`
	Campaign         string `json:"campaign"`
	Content          string `json:"content"`
	Term             string `json:"term"`
	Referrer         string `json:"referrer"`
}

// Summary is the sales-ledger view used by the current admin dashboard.
// Raw browser events remain a separate journal, not sales or paid revenue.
type Summary struct {
	Period          int               `json:"period"`
	Orders          int               `json:"orders"`
	Revenue         float64           `json:"revenue"`
	Daily           []DailyRow        `json:"daily"`
	SoldUnits       int               `json:"soldUnits"`
	AverageCheck    float64           `json:"averageCheck"`
	Returns         int               `json:"returns"`
	PreviousRevenue float64           `json:"previousRevenue"`
	PreviousOrders  int               `json:"previousOrders"`
	SalesChannels   []SalesChannelRow `json:"salesChannels"`
	SalesProducts   []SalesProductRow `json:"salesProducts"`
	DataQuality     DataQuality       `json:"dataQuality"`
	ExcludedSales   []ExcludedSaleRow `json:"excludedSales"`
	PeriodFrom      string            `json:"periodFrom"`
	PeriodTo        string            `json:"periodTo"`
	Channel         string            `json:"channel"`
}

type SalesChannelRow struct {
	Channel      string  `json:"channel"`
	Revenue      float64 `json:"revenue"`
	Orders       int     `json:"orders"`
	Units        int     `json:"units"`
	Returns      int     `json:"returns"`
	Availability string  `json:"availability"`
}
type SalesProductRow struct {
	ProductID int64    `json:"productId"`
	Name      string   `json:"name"`
	Units     int      `json:"units"`
	Revenue   float64  `json:"revenue"`
	Returns   int      `json:"returns"`
	Channels  []string `json:"channels"`
}
type DataQuality struct {
	Counted    int `json:"counted"`
	Duplicates int `json:"duplicates"`
	Unmatched  int `json:"unmatched"`
	Ambiguous  int `json:"ambiguous"`
	Excluded   int `json:"excluded"`
	Pending    int `json:"pending"`
}
type ExcludedSaleRow struct {
	Channel           string `json:"channel"`
	SourceDocumentID  string `json:"sourceDocumentId"`
	ExternalProductID string `json:"externalProductId"`
	Status            string `json:"status"`
	EventAt           string `json:"eventAt"`
	Reason            string `json:"reason"`
}

type DailyRow struct {
	Date    string  `json:"date"`
	Orders  int     `json:"orders"`
	Revenue float64 `json:"revenue"`
}

type Store struct {
	metrika *MetrikaWorker
	pool    *pgxpool.Pool
	now     func() time.Time
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, now: time.Now} }

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
	attribution = store.metrika.orderAttribution(attribution)
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
		SELECT gen_random_uuid(),'order_item_requested',$2,$3,CURRENT_TIMESTAMP,'/checkout',$4,$5,$6,$7,$8,p.product_code::text,oi.sku,$1,(oi.unit_price*oi.quantity),oi.quantity,jsonb_build_object('name',oi.product_name),1
		FROM orders o JOIN order_items oi ON oi.order_id=o.id JOIN products p ON p.id=oi.product_id WHERE o.order_number=$1`, orderNumber, visitor, session, clean(attribution.Source, 120), clean(attribution.Medium, 120), clean(attribution.Campaign, 200), clean(attribution.Content, 200), clean(attribution.Term, 200))
	if err != nil {
		return fmt.Errorf("record requested items: %w", err)
	}
	return tx.Commit(ctx)
}

func (store *Store) Summary(ctx context.Context, days int, channel string) (Summary, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	if channel == "all" {
		channel = ""
	}
	if channel != "" && channel != "site" && channel != "saby" && channel != "wb" && channel != "ozon" && channel != "avito" {
		channel = ""
	}
	location, _ := time.LoadLocation("Europe/Moscow")
	now := store.now().In(location)
	endLocal := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
	since := endLocal.AddDate(0, 0, -days)
	previousSince := since.AddDate(0, 0, -days)
	result := Summary{Period: days, Daily: []DailyRow{}, SalesChannels: []SalesChannelRow{}, SalesProducts: []SalesProductRow{}, ExcludedSales: []ExcludedSaleRow{}, PeriodFrom: since.Format("2006-01-02"), PeriodTo: endLocal.Add(-time.Second).Format("2006-01-02"), Channel: channel}
	err := store.pool.QueryRow(ctx, `SELECT
		COUNT(DISTINCT source_document_id) FILTER(WHERE event_type='sale')::INTEGER,
		COALESCE(SUM(gross_rub*effect),0)::DOUBLE PRECISION,
		COALESCE(SUM(units*effect),0)::INTEGER,
		COALESCE(SUM(units) FILTER(WHERE effect<0),0)::INTEGER,
		COALESCE(SUM(gross_rub) FILTER(WHERE effect>0),0)::DOUBLE PRECISION
		FROM sales_events WHERE reconciliation_status='counted' AND event_status='confirmed'
			AND event_at >= $1 AND event_at < $2 AND ($3='' OR channel=$3)`, since, endLocal, channel).
		Scan(&result.Orders, &result.Revenue, &result.SoldUnits, &result.Returns, &result.AverageCheck)
	if err != nil {
		return result, fmt.Errorf("analytics paid orders: %w", err)
	}
	if result.Orders > 0 {
		result.AverageCheck /= float64(result.Orders)
	} else {
		result.AverageCheck = 0
	}
	err = store.pool.QueryRow(ctx, `SELECT COALESCE(SUM(gross_rub*effect),0)::DOUBLE PRECISION,
		COUNT(DISTINCT source_document_id) FILTER(WHERE event_type='sale')::INTEGER
		FROM sales_events WHERE reconciliation_status='counted' AND event_status='confirmed'
			AND event_at >= $1 AND event_at < $2 AND ($3='' OR channel=$3)`, previousSince, since, channel).
		Scan(&result.PreviousRevenue, &result.PreviousOrders)
	if err != nil {
		return result, fmt.Errorf("analytics comparison: %w", err)
	}
	rows, err := store.pool.Query(ctx, `WITH dates AS (
		SELECT generate_series($1::TIMESTAMPTZ AT TIME ZONE 'Europe/Moscow',
			($2::TIMESTAMPTZ-INTERVAL '1 day') AT TIME ZONE 'Europe/Moscow',INTERVAL '1 day')::DATE local_day
	), sales AS (
		SELECT (event_at AT TIME ZONE 'Europe/Moscow')::DATE local_day,
			COUNT(DISTINCT source_document_id) FILTER(WHERE event_type='sale')::INT orders,
			COALESCE(SUM(gross_rub*effect),0)::DOUBLE PRECISION revenue
		FROM sales_events WHERE reconciliation_status='counted' AND event_status='confirmed'
			AND event_at >= $1 AND event_at < $2 AND ($3='' OR channel=$3) GROUP BY 1
	) SELECT TO_CHAR(dates.local_day,'YYYY-MM-DD'),
		COALESCE(sales.orders,0),COALESCE(sales.revenue,0)
	FROM dates LEFT JOIN sales USING(local_day) ORDER BY dates.local_day`, since, endLocal, channel)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item DailyRow
		if err := rows.Scan(&item.Date, &item.Orders, &item.Revenue); err != nil {
			return result, err
		}
		result.Daily = append(result.Daily, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows, err = store.pool.Query(ctx, `WITH channels(channel,position) AS (VALUES
		('ozon',1),('wb',2),('site',3),('saby',4),('avito',5)), totals AS (
		SELECT channel,COALESCE(SUM(gross_rub*effect),0)::DOUBLE PRECISION revenue,
			COUNT(DISTINCT source_document_id) FILTER(WHERE event_type='sale')::INT orders,
			COALESCE(SUM(units*effect),0)::INT units,
			COALESCE(SUM(units) FILTER(WHERE effect<0),0)::INT returns
		FROM sales_events WHERE reconciliation_status='counted' AND event_status='confirmed'
			AND event_at >= $1 AND event_at < $2 GROUP BY channel
	) SELECT channels.channel,COALESCE(totals.revenue,0),COALESCE(totals.orders,0),
		COALESCE(totals.units,0),COALESCE(totals.returns,0),CASE
			WHEN channels.channel='avito' THEN 'no_data'
			WHEN channels.channel='site' THEN CASE WHEN totals.channel IS NULL THEN 'empty' ELSE 'data' END
			WHEN state.status='disabled' THEN 'unavailable'
			WHEN state.status='error' THEN 'unavailable'
			WHEN totals.channel IS NULL THEN 'empty' ELSE 'data' END
		FROM channels LEFT JOIN totals USING(channel)
		LEFT JOIN procurement_sales_sync_state state USING(channel)
		WHERE $3='' OR channels.channel=$3 ORDER BY channels.position`, since, endLocal, channel)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SalesChannelRow
		if err := rows.Scan(&item.Channel, &item.Revenue, &item.Orders, &item.Units, &item.Returns, &item.Availability); err != nil {
			return result, err
		}
		result.SalesChannels = append(result.SalesChannels, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows, err = store.pool.Query(ctx, `SELECT COALESCE(product.id,0),
		COALESCE(product.name,CASE event.external_product_id WHEN '__adjustment__' THEN 'Возвраты и корректировки' WHEN '__delivery__' THEN 'Доставка' ELSE event.external_product_id END),
		COALESCE(SUM(event.units*event.effect),0)::INT,
		COALESCE(SUM(event.gross_rub*event.effect),0)::DOUBLE PRECISION,
		COALESCE(SUM(event.units) FILTER(WHERE event.effect<0),0)::INT,
		ARRAY_AGG(DISTINCT event.channel ORDER BY event.channel)
		FROM sales_events event LEFT JOIN product_variants variant ON variant.id=event.canonical_variant_id
		LEFT JOIN products product ON product.id=variant.product_id
		WHERE event.reconciliation_status='counted' AND event.event_status='confirmed'
			AND event.event_at >= $1 AND event.event_at < $2 AND ($3='' OR event.channel=$3)
		GROUP BY product.id,COALESCE(product.name,CASE event.external_product_id WHEN '__adjustment__' THEN 'Возвраты и корректировки' WHEN '__delivery__' THEN 'Доставка' ELSE event.external_product_id END)
		ORDER BY SUM(event.gross_rub*event.effect) DESC LIMIT 50`, since, endLocal, channel)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SalesProductRow
		if err := rows.Scan(&item.ProductID, &item.Name, &item.Units, &item.Revenue, &item.Returns, &item.Channels); err != nil {
			return result, err
		}
		result.SalesProducts = append(result.SalesProducts, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	err = store.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE reconciliation_status='counted')::INT,
		COUNT(*) FILTER(WHERE reconciliation_status='duplicate')::INT,
		COUNT(*) FILTER(WHERE reconciliation_status='unmatched')::INT,
		COUNT(*) FILTER(WHERE reconciliation_status='ambiguous')::INT,
		COUNT(*) FILTER(WHERE reconciliation_status='excluded')::INT,
		COUNT(*) FILTER(WHERE event_status='pending')::INT
		FROM sales_events WHERE event_at >= $1 AND event_at < $2 AND ($3='' OR channel=$3)`, since, endLocal, channel).
		Scan(&result.DataQuality.Counted, &result.DataQuality.Duplicates, &result.DataQuality.Unmatched, &result.DataQuality.Ambiguous, &result.DataQuality.Excluded, &result.DataQuality.Pending)
	if err != nil {
		return result, err
	}
	rows, err = store.pool.Query(ctx, `SELECT channel,source_document_id,external_product_id,
		reconciliation_status,TO_CHAR(event_at AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD HH24:MI'),
		CASE reconciliation_status WHEN 'duplicate' THEN 'Повтор другого источника'
			WHEN 'unmatched' THEN 'Товар не сопоставлен' WHEN 'ambiguous' THEN 'Нужна ручная сверка'
			ELSE CASE WHEN event_status='pending' THEN 'Продажа не подтверждена' ELSE 'Исключено по статусу' END END
		FROM sales_events WHERE event_at >= $1 AND event_at < $2
			AND reconciliation_status<>'counted' AND ($3='' OR channel=$3)
		ORDER BY event_at DESC,id DESC LIMIT 50`, since, endLocal, channel)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ExcludedSaleRow
		if err := rows.Scan(&item.Channel, &item.SourceDocumentID, &item.ExternalProductID, &item.Status, &item.EventAt, &item.Reason); err != nil {
			return result, err
		}
		result.ExcludedSales = append(result.ExcludedSales, item)
	}
	return result, rows.Err()
}
