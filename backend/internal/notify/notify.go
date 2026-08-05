// Package notify keeps the browsers that agreed to receive notifications and
// delivers messages to them.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Subscription struct {
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	UserAgent string `json:"-"`
}

// Message is what a person sees on their lock screen.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

type Service struct {
	pool   *pgxpool.Pool
	client *webpush.Client
	logger *slog.Logger
}

// NewService returns nil when no VAPID keys are configured. Callers treat a
// nil service as "notifications are switched off" rather than an error, so
// the shop runs perfectly well without them.
func NewService(pool *pgxpool.Pool, cfgPublic, cfgPrivate, subject string, logger *slog.Logger) (*Service, error) {
	if cfgPublic == "" || cfgPrivate == "" {
		return nil, nil
	}
	client, err := webpush.NewClient(cfgPrivate, cfgPublic, subject)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, client: client, logger: logger}, nil
}

func (service *Service) PublicKey() string {
	if service == nil {
		return ""
	}
	return service.client.PublicKey()
}

// Subscribe stores a browser's subscription. The same browser re-subscribing
// updates the existing row instead of piling up duplicates, which is why the
// endpoint carries the unique index.
func (service *Service) Subscribe(ctx context.Context, customerID *int64, subscription Subscription) error {
	if service == nil {
		return errors.New("push notifications are not configured")
	}
	if subscription.Endpoint == "" || subscription.P256dh == "" || subscription.Auth == "" {
		return errors.New("subscription is incomplete")
	}
	_, err := service.pool.Exec(ctx, `
		INSERT INTO push_subscriptions (customer_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE SET
			customer_id = EXCLUDED.customer_id,
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			user_agent = EXCLUDED.user_agent,
			expired_at = NULL
	`, customerID, subscription.Endpoint, subscription.P256dh, subscription.Auth,
		truncate(subscription.UserAgent, 500))
	if err != nil {
		return fmt.Errorf("store push subscription: %w", err)
	}
	return nil
}

func (service *Service) Unsubscribe(ctx context.Context, endpoint string) error {
	if service == nil {
		return nil
	}
	if _, err := service.pool.Exec(ctx,
		"DELETE FROM push_subscriptions WHERE endpoint = $1", endpoint); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

// SendToCustomer delivers a message to every browser that customer signed in
// from. A dead subscription is marked expired and skipped from then on.
func (service *Service) SendToCustomer(ctx context.Context, customerID int64, message Message) error {
	if service == nil {
		return nil
	}
	rows, err := service.pool.Query(ctx, `
		SELECT id, endpoint, p256dh, auth
		FROM push_subscriptions
		WHERE customer_id = $1 AND expired_at IS NULL
	`, customerID)
	if err != nil {
		return fmt.Errorf("load push subscriptions: %w", err)
	}
	type row struct {
		id           int64
		subscription webpush.Subscription
	}
	targets := make([]row, 0, 4)
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.subscription.Endpoint,
			&item.subscription.P256dh, &item.subscription.Auth); err != nil {
			rows.Close()
			return fmt.Errorf("scan push subscription: %w", err)
		}
		targets = append(targets, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read push subscriptions: %w", err)
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	for _, target := range targets {
		err := service.client.Send(target.subscription, payload, 86400)
		switch {
		case errors.Is(err, webpush.ErrGone):
			if _, markErr := service.pool.Exec(ctx,
				"UPDATE push_subscriptions SET expired_at = CURRENT_TIMESTAMP WHERE id = $1",
				target.id); markErr != nil {
				service.logger.Error("mark push subscription expired", "error", markErr)
			}
		case err != nil:
			// One failing browser must not stop the others.
			service.logger.Error("send push", "subscription_id", target.id, "error", err)
		default:
			if _, touchErr := service.pool.Exec(ctx,
				"UPDATE push_subscriptions SET last_sent_at = CURRENT_TIMESTAMP WHERE id = $1",
				target.id); touchErr != nil {
				service.logger.Error("touch push subscription", "error", touchErr)
			}
		}
	}
	return nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
