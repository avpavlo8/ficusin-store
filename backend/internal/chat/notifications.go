package chat

import (
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
	"github.com/jackc/pgx/v5"
)

// Push endpoints are browser-issued capabilities, never arbitrary HTTP targets.
func ValidPushEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 1000 || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "fcm.googleapis.com" || host == "updates.push.services.mozilla.com" || host == "push.services.mozilla.com" || host == "web.push.apple.com" || strings.HasSuffix(host, ".notify.windows.com")
}

func (s *Store) SubscribePush(ctx context.Context, who Identity, staff bool, sub notify.Subscription) error {
	key, err := base64.RawURLEncoding.DecodeString(sub.P256dh)
	if err != nil || len(key) != 65 || (who.CustomerID > 0 && who.SessionHash == "") {
		return ErrInvalid
	}
	x, _ := elliptic.Unmarshal(elliptic.P256(), key)
	authKey, err := base64.RawURLEncoding.DecodeString(sub.Auth)
	if x == nil || err != nil || len(authKey) != 16 || !ValidPushEndpoint(sub.Endpoint) || (who.CustomerID == 0 && (staff || who.GuestHash == "")) {
		return ErrInvalid
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO support_push_subscriptions(endpoint,p256dh,auth,customer_id,guest_hash,staff,session_hash) VALUES($1,$2,$3,NULLIF($4,0),$5,$6,$7) ON CONFLICT(endpoint,staff) DO UPDATE SET p256dh=EXCLUDED.p256dh,auth=EXCLUDED.auth,customer_id=EXCLUDED.customer_id,guest_hash=EXCLUDED.guest_hash,session_hash=EXCLUDED.session_hash`, sub.Endpoint, sub.P256dh, sub.Auth, who.CustomerID, func() string {
		if who.CustomerID > 0 {
			return ""
		}
		return who.GuestHash
	}(), staff, who.SessionHash)
	return err
}
func (s *Store) PushState(ctx context.Context, who Identity, staff bool, endpoint string) (bool, error) {
	if who.CustomerID > 0 {
		if _, err := s.pool.Exec(ctx, `UPDATE support_push_subscriptions SET session_hash=$4 WHERE endpoint=$1 AND staff=$2 AND customer_id=$3`, endpoint, staff, who.CustomerID, who.SessionHash); err != nil {
			return false, err
		}
	}
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM support_push_subscriptions WHERE endpoint=$1 AND staff=$2 AND (($3::bigint>0 AND customer_id=$3) OR ($3::bigint=0 AND customer_id IS NULL AND guest_hash=$4)))`, endpoint, staff, who.CustomerID, who.GuestHash).Scan(&enabled)
	return enabled, err
}
func (s *Store) UnsubscribePush(ctx context.Context, who Identity, staff bool, endpoint string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM support_push_subscriptions WHERE endpoint=$1 AND staff=$2 AND (($3::bigint>0 AND customer_id=$3) OR ($3::bigint=0 AND customer_id IS NULL AND guest_hash=$4))`, endpoint, staff, who.CustomerID, who.GuestHash)
	return err
}

const pushRecipientSQL = `(s.customer_id IS NULL OR EXISTS(SELECT 1 FROM auth_sessions sess WHERE sess.customer_id=s.customer_id AND sess.token_hash=s.session_hash AND sess.expires_at>CURRENT_TIMESTAMP)) AND ((m.sender='customer' AND s.staff AND EXISTS(SELECT 1 FROM admin_users a WHERE a.customer_id=s.customer_id AND a.is_active AND a.role IN ('owner','administrator','manager'))) OR (m.sender='staff' AND NOT s.staff AND ((c.customer_id IS NOT NULL AND s.customer_id=c.customer_id) OR (c.customer_id IS NULL AND s.customer_id IS NULL AND s.guest_hash=c.guest_hash))))`

type PushSender interface {
	Deliver(context.Context, notify.Subscription, notify.Message) error
}

// Each committed message creates one durable delivery per opted-in browser.
// Failed sends are retried; a repeated clientId never enqueues a second delivery.
func (s *Store) RunPush(ctx context.Context, sender PushSender, logger *slog.Logger) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			for range 20 {
				ok, err := s.deliverPush(ctx, sender)
				if err != nil {
					logger.Error("chat push delivery failed", "error", err)
					break
				}
				if !ok {
					break
				}
			}
		}
	}
}
func (s *Store) deliverPush(ctx context.Context, sender PushSender) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var delivery, subscription, message, conversation int64
	var sub notify.Subscription
	var staff, eligible, read, old bool
	var text, kind, name, customerName string
	err = tx.QueryRow(ctx, `SELECT d.id,s.id,s.endpoint,s.p256dh,s.auth,s.staff,m.id,c.id,COALESCE(NULLIF(u.full_name,''),'Посетитель №'||c.id::text),m.body,CASE WHEN m.image_type LIKE 'image/%' THEN 'image' ELSE m.attachment_kind END,m.attachment_name,`+pushRecipientSQL+`,CASE WHEN s.staff THEN c.staff_read_id>=m.id ELSE c.customer_read_id>=m.id END,d.created_at<CURRENT_TIMESTAMP-INTERVAL '48 hours' FROM support_push_deliveries d JOIN support_push_subscriptions s ON s.id=d.subscription_id JOIN support_messages m ON m.id=d.message_id JOIN support_conversations c ON c.id=m.conversation_id LEFT JOIN customers u ON u.id=c.customer_id WHERE d.completed_at IS NULL AND d.next_attempt_at<=CURRENT_TIMESTAMP ORDER BY d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&delivery, &subscription, &sub.Endpoint, &sub.P256dh, &sub.Auth, &staff, &message, &conversation, &customerName, &text, &kind, &name, &eligible, &read, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	outcome := "sent"
	switch {
	case !eligible:
		outcome = "inactive"
	case read:
		outcome = "read"
	case old:
		outcome = "expired"
	default:
		body := strings.TrimSpace(text)
		if body == "" {
			switch kind {
			case "image":
				body = "📷 Фотография"
			case "video":
				body = "🎥 Видео"
			default:
				body = "📎 " + name
			}
		}
		runes := []rune(body)
		if len(runes) > 180 {
			body = string(runes[:177]) + "…"
		}
		target := "/chat"
		title := "Фикусин"
		if staff {
			target = "/admin?section=chat&conversation=" + strconv.FormatInt(conversation, 10)
			title = customerName
		}
		err = sender.Deliver(ctx, sub, notify.Message{Title: title, Body: body, URL: target, Tag: "chat-" + strconv.FormatInt(conversation, 10)})
		if errors.Is(err, webpush.ErrGone) {
			_, err = tx.Exec(ctx, `DELETE FROM support_push_subscriptions WHERE id=$1`, subscription)
			if err != nil {
				return false, err
			}
			return true, tx.Commit(ctx)
		}
		if err != nil {
			_, dbErr := tx.Exec(ctx, `UPDATE support_push_deliveries SET attempts=attempts+1,next_attempt_at=CURRENT_TIMESTAMP+make_interval(secs=>LEAST(3600,5*(1<<LEAST(attempts,9)))) WHERE id=$1`, delivery)
			if dbErr != nil {
				return false, dbErr
			}
			return true, tx.Commit(ctx)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE support_push_deliveries SET completed_at=CURRENT_TIMESTAMP,outcome=$2 WHERE id=$1`, delivery, outcome)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
