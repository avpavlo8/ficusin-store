package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/png"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	var photo bytes.Buffer
	_ = png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	valid := Input{ClientID: "0123456789012345", Text: "  Помогите  ", Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(photo.Bytes())}
	data, kind, err := Validate(&valid)
	if err != nil || kind != "image/png" || len(data) == 0 || valid.Text != "Помогите" {
		t.Fatalf("validate: %s %v", kind, err)
	}
	for _, input := range []Input{{ClientID: valid.ClientID}, {ClientID: "short", Text: "hello"}, {ClientID: valid.ClientID, Text: strings.Repeat("я", 4001)}, {ClientID: valid.ClientID, Text: "x", ContextURL: "//evil.test/"}, {ClientID: valid.ClientID, Text: "x", ContextURL: "/product/\\evil"}, {ClientID: valid.ClientID, Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))}} {
		if _, _, err := Validate(&input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid message %#v", input)
		}
	}
}
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CHAT_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("CRM_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set CHAT_TEST_DATABASE_URL for PostgreSQL acceptance")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("chat_test_%d", time.Now().UnixNano())
	if _, err = root.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); _, _ = root.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); root.Close() })
	if _, err = pool.Exec(ctx, `CREATE TABLE customers(id BIGINT PRIMARY KEY,full_name TEXT); INSERT INTO customers VALUES(1,'Покупатель'),(2,'Сотрудник')`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../../timeweb/migrations/20261022_site_chat.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool)
}
func TestChatDurabilityIsolationAndLogin(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	guest := Identity{GuestHash: "guest-one"}
	other := Identity{GuestHash: "guest-two"}
	input := Input{ClientID: "a-message-client-id", Text: "У фикуса желтеют листья", ConsentVersion: "2026-10-22-chat", IP: "127.0.0.1"}
	if _, err := s.Send(ctx, guest, 0, 0, input); !errors.Is(err, ErrConsent) {
		t.Fatalf("consent: %v", err)
	}
	var count int
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM support_conversations`).Scan(&count)
	if count != 0 {
		t.Fatal("consent failure left a conversation")
	}
	input.Consent = true
	id, err := s.Send(ctx, guest, 0, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, guest, 0, 0, input); err != nil {
		t.Fatal(err)
	}
	thread, err := NewStore(s.pool).Read(ctx, id, 0)
	if err != nil || len(thread.Messages) != 1 {
		t.Fatalf("durability/idempotency: %#v %v", thread, err)
	}
	if otherID, err := s.Resolve(ctx, other); err != nil || otherID != 0 {
		t.Fatal("guest saw another conversation")
	}
	if n, err := s.Unread(ctx, 0, true); err != nil || n != 1 {
		t.Fatalf("staff unread: %d %v", n, err)
	}
	if err = s.MarkRead(ctx, id, thread.Messages[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Unread(ctx, 0, true); n != 0 {
		t.Fatal("read did not clear unread")
	}
	var imageData bytes.Buffer
	_ = png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	reply := Input{ClientID: "reply-client-identifier", Text: "Пришлите фото", Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData.Bytes())}
	if _, err = s.Send(ctx, Identity{}, 2, id, reply); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Unread(ctx, id, false); n != 1 {
		t.Fatal("customer missing unread reply")
	}
	thread, _ = s.Read(ctx, id, 0)
	kind, data, err := s.Photo(ctx, id, thread.Messages[1].ID)
	if err != nil || kind != "image/png" || !bytes.Equal(data, imageData.Bytes()) {
		t.Fatalf("photo not durable %v", err)
	}
	if _, _, err = s.Photo(ctx, id+100, thread.Messages[1].ID); err == nil {
		t.Fatal("cross-conversation photo accepted")
	}
	if err = s.SetStatus(ctx, id, "closed"); err != nil {
		t.Fatal(err)
	}
	input.ClientID = "another-client-identifier"
	if _, err = s.Send(ctx, guest, 0, 0, input); err != nil {
		t.Fatal(err)
	}
	thread, _ = s.Read(ctx, id, 0)
	if thread.Status != "open" {
		t.Fatal("customer did not reopen thread")
	}
	account := Identity{CustomerID: 1}
	input.ClientID = "account-client-identifier"
	accountID, err := s.Send(ctx, account, 0, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := s.Resolve(ctx, Identity{CustomerID: 1, GuestHash: guest.GuestHash})
	if err != nil || merged != accountID {
		t.Fatalf("merge: %d %v", merged, err)
	}
	thread, _ = s.Read(ctx, accountID, 0)
	if len(thread.Messages) != 4 {
		t.Fatalf("merged history: %d", len(thread.Messages))
	}
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM support_consents WHERE conversation_id=$1`, accountID).Scan(&count)
	if count != 2 {
		t.Fatalf("lost consent evidence: %d", count)
	}
	if guestID, err := s.Resolve(ctx, guest); err != nil || guestID != 0 {
		t.Fatalf("logout leaked account history: %d %v", guestID, err)
	}
	if crossDevice, err := s.Resolve(ctx, account); err != nil || crossDevice != accountID {
		t.Fatal("account not available across devices")
	}
}
func TestChatConcurrentFirstSendAndHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	who := Identity{GuestHash: "concurrent-guest"}
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Send(ctx, who, 0, 0, Input{ClientID: "same-message-identifier", Text: "Один вопрос", Consent: true})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.Resolve(ctx, who)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.Read(ctx, id, 0)
	if err != nil || len(thread.Messages) != 1 {
		t.Fatalf("duplicate concurrent send: %v %v", thread, err)
	}
	for i := 0; i < 55; i++ {
		if _, err = s.Send(ctx, who, 0, 0, Input{ClientID: fmt.Sprintf("history-message-%04d", i), Text: "История"}); err != nil {
			t.Fatal(err)
		}
	}
	thread, _ = s.Read(ctx, id, 0)
	if len(thread.Messages) != 50 || !thread.HasMore {
		t.Fatal("history page size")
	}
	older, err := s.Read(ctx, id, thread.Messages[0].ID)
	if err != nil || older.HasMore || len(older.Messages) != 6 {
		t.Fatalf("history missing: %d %v", len(older.Messages), err)
	}
	_ = s.MarkRead(ctx, id, 1<<60, true)
	if _, err = s.Send(ctx, who, 0, 0, Input{ClientID: "after-future-read-id", Text: "Новое"}); err != nil {
		t.Fatal(err)
	}
	if count, _ := s.Unread(ctx, 0, true); count != 1 {
		t.Fatal("future read swallowed new messages")
	}
}
