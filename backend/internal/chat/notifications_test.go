package chat

import (
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/webpush"
)

type pushRecorder struct {
	messages []notify.Message
	fail     error
}

func (p *pushRecorder) Deliver(_ context.Context, _ notify.Subscription, message notify.Message) error {
	if p.fail != nil {
		return p.fail
	}
	p.messages = append(p.messages, message)
	return nil
}
func testSubscription(t *testing.T, suffix string) notify.Subscription {
	t.Helper()
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return notify.Subscription{Endpoint: "https://web.push.apple.com/" + suffix, P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16))}
}
func TestPushEndpointRejectsArbitraryServers(t *testing.T) {
	for _, endpoint := range []string{"http://web.push.apple.com/a", "https://127.0.0.1/a", "https://web.push.apple.com.evil.test/a", "https://evil.test/a", "https://user@web.push.apple.com/a", "https://web.push.apple.com:123/a"} {
		if ValidPushEndpoint(endpoint) {
			t.Fatalf("accepted %s", endpoint)
		}
	}
}
func TestChatPushDurabilityRetryPrivacyAndRead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	buyer := Identity{CustomerID: 1, SessionHash: "buyer-session"}
	staff := Identity{CustomerID: 2, SessionHash: "staff-session"}
	sub := testSubscription(t, "buyer")
	employee := testSubscription(t, "staff")
	for _, item := range []struct {
		who   Identity
		staff bool
		sub   notify.Subscription
	}{{buyer, false, sub}, {staff, true, employee}} {
		if err := s.SubscribePush(ctx, item.who, item.staff, item.sub); err != nil {
			t.Fatal(err)
		}
	}
	input := Input{ClientID: "notify-customer-0001", Text: "Здравствуйте, подскажите по уходу 🌿"}
	id, err := s.Send(ctx, buyer, 0, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, buyer, 0, 0, input); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM support_push_deliveries`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate delivery: %d %v", count, err)
	}
	receiver := &pushRecorder{fail: errors.New("temporary outage")}
	if ok, err := s.deliverPush(ctx, receiver); !ok || err != nil {
		t.Fatalf("retry: %v %v", ok, err)
	}
	var attempts int
	var completed bool
	if err = s.pool.QueryRow(ctx, `SELECT attempts,completed_at IS NOT NULL FROM support_push_deliveries`).Scan(&attempts, &completed); err != nil || attempts != 1 || completed {
		t.Fatalf("lost failed delivery: %d %v %v", attempts, completed, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE support_push_deliveries SET next_attempt_at=CURRENT_TIMESTAMP`); err != nil {
		t.Fatal(err)
	}
	receiver.fail = nil
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.messages) != 1 || receiver.messages[0].Body != input.Text || !strings.Contains(receiver.messages[0].URL, "conversation=") {
		t.Fatalf("staff payload: %+v", receiver.messages)
	}
	if _, err = s.Send(ctx, Identity{}, 2, id, Input{ClientID: "notify-staff-0001", Text: "Пришлите фото листьев"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.messages) != 2 || receiver.messages[1].Body != "Пришлите фото листьев" || receiver.messages[1].URL != "/chat" {
		t.Fatalf("customer payload: %+v", receiver.messages)
	}
	// Another visitor cannot inspect or turn off this browser's customer subscription.
	outsider := Identity{GuestHash: "stranger"}
	if enabled, err := s.PushState(ctx, outsider, false, sub.Endpoint); err != nil || enabled {
		t.Fatalf("foreign state: %v %v", enabled, err)
	}
	if err = s.UnsubscribePush(ctx, outsider, false, sub.Endpoint); err != nil {
		t.Fatal(err)
	}
	if enabled, err := s.PushState(ctx, buyer, false, sub.Endpoint); err != nil || !enabled {
		t.Fatalf("foreign delete: %v %v", enabled, err)
	}
	// Recheck role access after enqueueing, not just when subscribing.
	if _, err = s.Send(ctx, buyer, 0, 0, Input{ClientID: "notify-customer-0002", Text: "Ещё вопрос"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE admin_users SET is_active=FALSE`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.messages) != 2 {
		t.Fatal("revoked staff received push")
	}
	if _, err = s.pool.Exec(ctx, `UPDATE admin_users SET is_active=TRUE`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, buyer, 0, 0, Input{ClientID: "notify-customer-0003", Text: "Прочитанное сообщение"}); err != nil {
		t.Fatal(err)
	}
	thread, err := s.Read(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkRead(ctx, id, thread.Messages[len(thread.Messages)-1].ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.messages) != 2 {
		t.Fatal("already read message notified")
	}
	// Gone subscriptions are removed, while failed delivery stays retryable.
	if _, err = s.Send(ctx, Identity{}, 2, id, Input{ClientID: "notify-staff-0002", Text: "Проверка отключённого браузера"}); err != nil {
		t.Fatal(err)
	}
	receiver.fail = webpush.ErrGone
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if enabled, err := s.PushState(ctx, buyer, false, sub.Endpoint); err != nil || enabled {
		t.Fatalf("gone subscription survived: %v %v", enabled, err)
	}
	receiver.fail = nil
	if _, err = s.Send(ctx, buyer, 0, 0, Input{ClientID: "notify-attachment-0001", Attachment: &AttachmentInput{Name: "памятка.txt", Data: base64.StdEncoding.EncodeToString([]byte("instruction"))}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.messages) != 3 || receiver.messages[2].Body != "📎 памятка.txt" {
		t.Fatalf("attachment preview: %+v", receiver.messages)
	}
	if _, err = s.Send(ctx, buyer, 0, 0, Input{ClientID: "notify-logout-message1", Text: "После выхода push быть не должно"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `DELETE FROM auth_sessions WHERE customer_id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.deliverPush(ctx, receiver); err != nil {
		t.Fatal(err)
	}
	if len(receiver.messages) != 3 {
		t.Fatal("logged-out staff received text notification")
	}

}
func TestGuestPushBindsOnLogin(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	guest := Identity{GuestHash: "guest-push"}
	sub := testSubscription(t, "guest")
	if err := s.SubscribePush(ctx, guest, false, sub); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, guest, 0, 0, Input{ClientID: "guest-push-message-1", Text: "Вопрос гостя", Consent: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, Identity{CustomerID: 1, GuestHash: guest.GuestHash, SessionHash: "buyer-session"}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := s.PushState(ctx, Identity{CustomerID: 1, SessionHash: "buyer-session"}, false, sub.Endpoint); err != nil || !enabled {
		t.Fatalf("not bound: %v %v", enabled, err)
	}
	if enabled, err := s.PushState(ctx, guest, false, sub.Endpoint); err != nil || enabled {
		t.Fatalf("guest still owns account subscription: %v %v", enabled, err)
	}
}
