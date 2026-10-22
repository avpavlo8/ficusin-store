package analytics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Unknown delivery is quarantined: the JS goal API has no idempotency key.
// Never replay an ambiguous HTTP response or a claim interrupted by restart.
type MetrikaWorker struct {
	store                    *Store
	token, siteURL, endpoint string
	counter                  func() string
	client                   *http.Client
}

var metrikaID = regexp.MustCompile(`^[0-9]{1,20}$`)

func (s *Store) WithMetrika(token, siteURL string, counter func() string) *MetrikaWorker {
	w := &MetrikaWorker{store: s, token: strings.TrimSpace(token), siteURL: strings.TrimRight(siteURL, "/"), counter: counter, endpoint: "https://mc.yandex.ru/collect", client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	s.metrika = w
	return w
}
func (w *MetrikaWorker) configured() bool {
	return w != nil && w.token != "" && w.counter != nil && metrikaID.MatchString(w.counter()) && w.counter() != "0"
}
func (w *MetrikaWorker) orderAttribution(a Attribution) Attribution {
	a.MetrikaServer = false
	a.MetrikaCounterID = ""
	if !a.AnalyticsAllowed || !metrikaID.MatchString(a.MetrikaClientID) {
		a.MetrikaClientID = ""
		return a
	}
	if w.configured() {
		a.MetrikaServer = true
		a.MetrikaCounterID = w.counter()
	}
	return a
}

// Only the settled ledger counts. A provider redirect, hold, partial payment,
// cancelled order, pending refund or completed refund cannot create a purchase.
const metrikaSettlement = `o.status<>'cancelled' AND o.payment_status='paid'
 AND (o.delivery_payee='carrier' OR o.delivery_fee_pending=0)
 AND (CASE WHEN o.delivery_payee='carrier' THEN o.subtotal ELSE o.total END)>0
 AND COALESCE((SELECT SUM(p.amount) FROM payments p WHERE p.order_id=o.id AND p.status='paid'),0)>=(CASE WHEN o.delivery_payee='carrier' THEN o.subtotal ELSE o.total END)
 AND NOT EXISTS(SELECT 1 FROM payment_refunds r WHERE r.order_id=o.id AND r.status NOT IN ('failed','cancelled','canceled'))
 AND NOT EXISTS(SELECT 1 FROM payments p WHERE p.order_id=o.id AND p.status='pending')`

func (w *MetrikaWorker) enqueue(ctx context.Context) error {
	_, err := w.store.pool.Exec(ctx, `INSERT INTO metrika_paid_outbox(order_id,counter_id,client_id,amount)
 SELECT o.id,o.attribution->>'metrikaCounterId',o.attribution->>'metrikaClientId',CASE WHEN o.delivery_payee='carrier' THEN o.subtotal ELSE o.total END FROM orders o
 WHERE o.attribution->>'metrikaServer'='true' AND o.attribution->>'analyticsAllowed'='true'
 AND o.attribution->>'metrikaCounterId'=$1 AND o.attribution->>'metrikaClientId' ~ '^[0-9]{1,20}$'
 AND `+metrikaSettlement+` AND NOT EXISTS(SELECT 1 FROM metrika_paid_outbox b WHERE b.order_id=o.id)
 ORDER BY o.id LIMIT 100 ON CONFLICT(order_id) DO NOTHING`, w.counter())
	return err
}

type metrikaDelivery struct {
	OrderID                              int64
	EventID, CounterID, ClientID, Amount string
	Attempts                             int
}

func (w *MetrikaWorker) claim(ctx context.Context) (*metrikaDelivery, error) {
	if _, err := w.store.pool.Exec(ctx, `UPDATE metrika_paid_outbox SET status='uncertain',last_error='interrupted_delivery' WHERE status='sending' AND claimed_at<CURRENT_TIMESTAMP-INTERVAL '2 minutes'`); err != nil {
		return nil, err
	}
	if _, err := w.store.pool.Exec(ctx, `UPDATE metrika_paid_outbox b SET status='cancelled',last_error='settlement_changed' FROM orders o WHERE b.order_id=o.id AND b.status='pending' AND (NOT (`+metrikaSettlement+`) OR b.amount<>(CASE WHEN o.delivery_payee='carrier' THEN o.subtotal ELSE o.total END))`); err != nil {
		return nil, err
	}
	var d metrikaDelivery
	err := w.store.pool.QueryRow(ctx, `UPDATE metrika_paid_outbox b SET status='sending',claimed_at=CURRENT_TIMESTAMP,attempts=attempts+1 WHERE b.order_id=(
 SELECT q.order_id FROM metrika_paid_outbox q JOIN orders o ON o.id=q.order_id WHERE q.status='pending' AND q.counter_id=$1 AND q.next_attempt_at<=CURRENT_TIMESTAMP AND `+metrikaSettlement+` ORDER BY q.created_at FOR UPDATE OF q SKIP LOCKED LIMIT 1)
 RETURNING b.order_id,b.event_id::text,b.counter_id,b.client_id,b.amount::text,b.attempts`, w.counter()).Scan(&d.OrderID, &d.EventID, &d.CounterID, &d.ClientID, &d.Amount, &d.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}
func (w *MetrikaWorker) payload(d metrikaDelivery) string {
	hit := func(fields url.Values) string {
		fields.Set("cid", d.ClientID)
		fields.Set("dl", w.siteURL+"/checkout")
		fields.Set("cu", "RUB")
		return fields.Encode()
	}
	// A pageview creates a session for a visit older than 12h. The receipt date
	// is intentional: MP cannot backdate older events. No guest capability/PII.
	return strings.Join([]string{
		hit(url.Values{"t": {"pageview"}, "dt": {"Подтверждённая оплата (сервер)"}}),
		hit(url.Values{"t": {"event"}, "ea": {"paid_order"}, "ev": {d.Amount}}),
		hit(url.Values{"t": {"event"}, "pa": {"purchase"}, "ti": {d.EventID}, "tr": {d.Amount}}),
	}, "\n")
}
func (w *MetrikaWorker) send(ctx context.Context, d metrikaDelivery) (string, string) {
	endpoint, err := url.Parse(w.endpoint)
	if err != nil {
		return "failed", "invalid_endpoint"
	}
	q := endpoint.Query()
	q.Set("tid", d.CounterID)
	q.Set("ms", w.token)
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(w.payload(d)))
	if err != nil {
		return "failed", "invalid_request"
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	connected := false
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected = true }}))
	res, err := w.client.Do(req)
	// Transport error strings contain the URL and secret; never persist them.
	if err != nil {
		if !connected && d.Attempts < 8 {
			return "pending", "connection_failed"
		}
		return "uncertain", "transport_unknown"
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode == 200 && err == nil && strings.TrimSpace(string(body)) == "<!-- OK -->" {
		return "sent", ""
	}
	if res.StatusCode == 400 || res.StatusCode == 401 || res.StatusCode == 403 {
		return "failed", "rejected_" + strconv.Itoa(res.StatusCode)
	}
	return "uncertain", "response_unknown_" + strconv.Itoa(res.StatusCode)
}
func (w *MetrikaWorker) cycle(ctx context.Context) error {
	if !w.configured() {
		return nil
	}
	if err := w.enqueue(ctx); err != nil {
		return err
	}
	for range 25 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d, err := w.claim(ctx)
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		status, reason := w.send(ctx, *d)
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		delay := time.Duration(min(d.Attempts*d.Attempts, 360)) * time.Second
		_, err = w.store.pool.Exec(saveCtx, `UPDATE metrika_paid_outbox SET status=$2,last_error=$3,next_attempt_at=CURRENT_TIMESTAMP+$4::interval,sent_at=CASE WHEN $2='sent' THEN CURRENT_TIMESTAMP ELSE NULL END WHERE order_id=$1 AND status='sending'`, d.OrderID, status, reason, delay.String())
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
func (w *MetrikaWorker) Run(ctx context.Context, logger *slog.Logger) {
	if !w.configured() {
		logger.Info("metrika server payments disabled")
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := w.cycle(ctx); err != nil && ctx.Err() == nil {
			logger.Error("metrika outbox cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type MetrikaDeliveryStatus struct {
	Enabled bool           `json:"enabled"`
	Counts  map[string]int `json:"counts"`
}

func (s *Store) MetrikaStatus(ctx context.Context) (MetrikaDeliveryStatus, error) {
	result := MetrikaDeliveryStatus{Enabled: s.metrika.configured(), Counts: map[string]int{}}
	rows, err := s.pool.Query(ctx, `SELECT status,COUNT(*)::int FROM metrika_paid_outbox GROUP BY status`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return result, err
		}
		result.Counts[status] = count
	}
	return result, rows.Err()
}
