// Package chat owns the private support inbox. It never sends message contents
// or customer data to external messengers.
package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("invalid chat message")
var ErrConsent = errors.New("chat consent required")

type Identity struct {
	CustomerID  int64
	GuestHash   string
	SessionHash string
}
type AttachmentInput struct {
	Name string `json:"name"`
	Data string `json:"data"`
}
type Input struct {
	Attachment     *AttachmentInput `json:"attachment,omitempty"`
	ClientID       string           `json:"clientId"`
	Text           string           `json:"text"`
	ContextURL     string           `json:"contextUrl"`
	ContextLabel   string           `json:"contextLabel"`
	Image          string           `json:"image"`
	Consent        bool             `json:"consent"`
	ConsentVersion string           `json:"-"`
	IP             string           `json:"-"`
	Agent          string           `json:"-"`
}
type Message struct {
	AttachmentName string    `json:"attachmentName,omitempty"`
	AttachmentKind string    `json:"attachmentKind,omitempty"`
	AttachmentSize int       `json:"attachmentSize,omitempty"`
	ID             int64     `json:"id"`
	Sender         string    `json:"sender"`
	Text           string    `json:"text"`
	ContextURL     string    `json:"contextUrl,omitempty"`
	ContextLabel   string    `json:"contextLabel,omitempty"`
	HasImage       bool      `json:"hasImage"`
	CreatedAt      time.Time `json:"createdAt"`
}
type Thread struct {
	ConsentRequired bool      `json:"consentRequired"`
	ID              int64     `json:"id"`
	Status          string    `json:"status"`
	CustomerReadID  int64     `json:"customerReadId"`
	StaffReadID     int64     `json:"staffReadId"`
	Messages        []Message `json:"messages"`
	HasMore         bool      `json:"hasMore"`
}
type Summary struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Preview   string    `json:"preview"`
	Unread    int       `json:"unread"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func Validate(input *Input) ([]byte, string, error) {
	input.Text = strings.TrimSpace(input.Text)
	if len(input.ClientID) < 16 || len(input.ClientID) > 80 || strings.ContainsRune(input.Text, 0) || utf8.RuneCountInString(input.Text) > 4000 || len(input.ContextURL) > 500 || utf8.RuneCountInString(input.ContextLabel) > 180 {
		return nil, "", ErrInvalid
	}
	if input.ContextURL != "" && !strings.HasPrefix(input.ContextURL, "/product/") && !strings.HasPrefix(input.ContextURL, "/care/") && !strings.HasPrefix(input.ContextURL, "/account/orders/") {
		return nil, "", ErrInvalid
	}
	if strings.ContainsAny(input.ContextURL, "\r\n\\") || strings.ContainsRune(input.ContextLabel, 0) {
		return nil, "", ErrInvalid
	}
	var data []byte
	kind := ""
	if input.Image != "" {
		parts := strings.SplitN(input.Image, ",", 2)
		if len(parts) != 2 || len(parts[1]) > 2800000 {
			return nil, "", ErrInvalid
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil || len(data) > 2*1024*1024 {
			return nil, "", ErrInvalid
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (format != "jpeg" && format != "png") || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
			return nil, "", ErrInvalid
		}
		kind = "image/" + format
	}
	if input.Attachment != nil {
		if input.Image != "" || len(input.Attachment.Data) > 28*1024*1024 {
			return nil, "", ErrInvalid
		}
		name := strings.TrimSpace(path.Base(strings.ReplaceAll(input.Attachment.Name, "\\", "/")))
		if name == "" || name == "." || utf8.RuneCountInString(name) > 180 || strings.ContainsAny(name, "\x00\r\n") {
			return nil, "", ErrInvalid
		}
		input.Attachment.Name = name
		var err error
		data, err = base64.StdEncoding.DecodeString(input.Attachment.Data)
		if err != nil || len(data) == 0 || len(data) > 20*1024*1024 {
			return nil, "", ErrInvalid
		}
		kind = attachmentContentType(data)
		if kind == "image/jpeg" || kind == "image/png" {
			cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16000000 || len(data) > 2*1024*1024 {
				return nil, "", ErrInvalid
			}
		} else if kind != "video/mp4" && kind != "video/webm" {
			kind = "application/octet-stream"
		}
	}
	if input.Text == "" && len(data) == 0 {
		return nil, "", ErrInvalid
	}
	return data, kind, nil
}

// DetectContentType recognizes mp4* brands, while phone/browser recordings also
// use ISO base media brands. Inspect only a complete, bounded ftyp box; never
// trust the supplied extension or MIME type.
func attachmentContentType(data []byte) string {
	kind := http.DetectContentType(data)
	if kind != "application/octet-stream" || len(data) < 16 || string(data[4:8]) != "ftyp" {
		return kind
	}
	size := int(binary.BigEndian.Uint32(data[:4]))
	if size < 16 || size > 512 || size > len(data) || size%4 != 0 {
		return kind
	}
	for offset := 8; offset+4 <= size; offset += 4 {
		if offset == 12 { // minor version, not a compatible brand
			continue
		}
		switch string(data[offset : offset+4]) {
		case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "iso7", "iso8", "iso9", "avc1", "mp41", "mp42", "M4V ", "dash", "cmfc", "cmfs":
			return "video/mp4"
		}
	}
	return kind
}

// Resolve binds a guest's own history on login, merging into the account's
// canonical thread. Customer-owned threads can never be reopened by a guest cookie.
func (s *Store) resolve(ctx context.Context, tx pgx.Tx, who Identity) (int64, error) {
	var id, guestID int64
	if who.CustomerID > 0 {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, who.CustomerID); err != nil {
			return 0, err
		}
		if who.GuestHash != "" {
			if _, err := tx.Exec(ctx, `UPDATE support_push_subscriptions SET customer_id=$1,guest_hash='',session_hash=$3 WHERE NOT staff AND customer_id IS NULL AND guest_hash=$2`, who.CustomerID, who.GuestHash, who.SessionHash); err != nil {
				return 0, err
			}
		}
		err := tx.QueryRow(ctx, `SELECT id FROM support_conversations WHERE customer_id=$1 FOR UPDATE`, who.CustomerID).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	if who.GuestHash != "" {
		err := tx.QueryRow(ctx, `SELECT id FROM support_conversations WHERE guest_hash=$1 AND customer_id IS NULL FOR UPDATE`, who.GuestHash).Scan(&guestID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	if who.CustomerID == 0 {
		return guestID, nil
	}
	if guestID == 0 {
		return id, nil
	}
	if id == 0 {
		_, err := tx.Exec(ctx, `UPDATE support_conversations SET customer_id=$2,guest_hash=NULL WHERE id=$1`, guestID, who.CustomerID)
		return guestID, err
	}
	if _, err := tx.Exec(ctx, `UPDATE support_messages SET conversation_id=$1 WHERE conversation_id=$2`, id, guestID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE support_consents SET conversation_id=$1 WHERE conversation_id=$2`, id, guestID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM support_conversations WHERE id=$1`, guestID); err != nil {
		return 0, err
	}
	_, err := tx.Exec(ctx, `UPDATE support_conversations SET status='open',updated_at=CURRENT_TIMESTAMP,customer_read_id=0,staff_read_id=0 WHERE id=$1`, id)
	return id, err
}
func (s *Store) Resolve(ctx context.Context, who Identity) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := s.resolve(ctx, tx, who)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}
func (s *Store) Read(ctx context.Context, id, before int64) (Thread, error) {
	return s.read(ctx, id, before, 0)
}
func (s *Store) ReadAfter(ctx context.Context, id, after int64) (Thread, error) {
	return s.read(ctx, id, 0, after)
}
func (s *Store) read(ctx context.Context, id, before, after int64) (Thread, error) {
	out := Thread{ID: id, Status: "open", Messages: []Message{}}
	if id == 0 {
		return out, nil
	}
	err := s.pool.QueryRow(ctx, `SELECT status,customer_read_id,staff_read_id FROM support_conversations WHERE id=$1`, id).Scan(&out.Status, &out.CustomerReadID, &out.StaffReadID)
	if err != nil {
		return out, err
	}
	query := `SELECT id,sender,body,context_url,context_label,image_type IN ('image/jpeg','image/png') AND image_data IS NOT NULL,created_at,attachment_name,attachment_kind,COALESCE(octet_length(image_data),0) FROM support_messages WHERE conversation_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 51`
	cursor := before
	if after > 0 {
		query = `SELECT id,sender,body,context_url,context_label,image_type IN ('image/jpeg','image/png') AND image_data IS NOT NULL,created_at,attachment_name,attachment_kind,COALESCE(octet_length(image_data),0) FROM support_messages WHERE conversation_id=$1 AND id>$2 ORDER BY id ASC LIMIT 51`
		cursor = after
	}
	rows, err := s.pool.Query(ctx, query, id, cursor)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Sender, &m.Text, &m.ContextURL, &m.ContextLabel, &m.HasImage, &m.CreatedAt, &m.AttachmentName, &m.AttachmentKind, &m.AttachmentSize); err != nil {
			return out, err
		}
		out.Messages = append(out.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Messages) > 50 {
		out.HasMore = true
		out.Messages = out.Messages[:50]
	}
	for i, j := 0, len(out.Messages)-1; after == 0 && i < j; i, j = i+1, j-1 {
		out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
	}
	return out, nil
}
func (s *Store) Send(ctx context.Context, who Identity, staffID, threadID int64, input Input) (int64, error) {
	data, kind, err := Validate(&input)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sender := "customer"
	if staffID > 0 {
		sender = "staff"
		if err = tx.QueryRow(ctx, `SELECT id FROM support_conversations WHERE id=$1 FOR UPDATE`, threadID).Scan(&threadID); err != nil {
			return 0, err
		}
	} else {
		// Serialize first sends too; otherwise two tabs could create competing conversations.
		if who.CustomerID == 0 {
			if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, who.GuestHash); err != nil {
				return 0, err
			}
		}
		threadID, err = s.resolve(ctx, tx, who)
		if err != nil {
			return 0, err
		}
		if threadID == 0 {
			if !input.Consent && who.CustomerID == 0 {
				return 0, ErrConsent
			}
			err = tx.QueryRow(ctx, `INSERT INTO support_conversations(customer_id,guest_hash) VALUES(NULLIF($1,0),NULLIF($2,'')) RETURNING id`, who.CustomerID, func() string {
				if who.CustomerID > 0 {
					return ""
				}
				return who.GuestHash
			}()).Scan(&threadID)
			if err != nil {
				return 0, err
			}
			if who.CustomerID == 0 {
				_, err := tx.Exec(ctx, `INSERT INTO support_consents(conversation_id,document_version,ip_address,user_agent) VALUES($1,$2,$3,$4)`, threadID, input.ConsentVersion, input.IP, input.Agent)
				if err != nil {
					return 0, err
				}
			}
		}
	}
	attachmentName, attachmentKind := "", ""
	if input.Attachment != nil {
		attachmentName = input.Attachment.Name
		attachmentKind = "file"
		if strings.HasPrefix(kind, "image/") {
			attachmentKind = "image"
		}
		if strings.HasPrefix(kind, "video/") {
			attachmentKind = "video"
		}
	}
	var messageID int64
	err = tx.QueryRow(ctx, `INSERT INTO support_messages(conversation_id,sender,staff_id,client_id,body,context_url,context_label,image_type,image_data,attachment_name,attachment_kind) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(conversation_id,sender,client_id) DO NOTHING RETURNING id`, threadID, sender, staffID, input.ClientID, input.Text, input.ContextURL, input.ContextLabel, kind, data, attachmentName, attachmentKind).Scan(&messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return threadID, tx.Commit(ctx)
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE support_conversations SET status='open',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, threadID)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO support_push_deliveries(message_id,subscription_id) SELECT m.id,s.id FROM support_messages m JOIN support_conversations c ON c.id=m.conversation_id CROSS JOIN support_push_subscriptions s WHERE m.id=$1 AND `+pushRecipientSQL+` ON CONFLICT DO NOTHING`, messageID)
	if err != nil {
		return 0, err
	}
	return threadID, tx.Commit(ctx)
}
func (s *Store) MarkRead(ctx context.Context, id, through int64, staff bool) error {
	column := "customer_read_id"
	if staff {
		column = "staff_read_id"
	}
	_, err := s.pool.Exec(ctx, `UPDATE support_conversations SET `+column+`=GREATEST(`+column+`,COALESCE((SELECT MAX(id) FROM support_messages WHERE conversation_id=$1 AND id<=$2),0)) WHERE id=$1`, id, through)
	return err
}
func (s *Store) Photo(ctx context.Context, threadID, messageID int64) (string, []byte, error) {
	var kind string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT image_type,image_data FROM support_messages WHERE conversation_id=$1 AND id=$2 AND image_data IS NOT NULL`, threadID, messageID).Scan(&kind, &data)
	return kind, data, err
}
func (s *Store) List(ctx context.Context, status, query string, offset int) ([]Summary, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id,COALESCE(NULLIF(u.full_name,''),'Посетитель №'||c.id),c.status,COALESCE(NULLIF(LEFT(m.body,160),''),NULLIF(m.attachment_name,''),'Фотография'),(SELECT COUNT(*) FROM support_messages WHERE conversation_id=c.id AND sender='customer' AND id>c.staff_read_id),c.updated_at FROM support_conversations c LEFT JOIN customers u ON u.id=c.customer_id LEFT JOIN LATERAL(SELECT body,attachment_name FROM support_messages WHERE conversation_id=c.id ORDER BY id DESC LIMIT 1)m ON true WHERE ($1='' OR c.status=$1) AND ($2='' OR COALESCE(u.full_name,'') ILIKE '%'||$2||'%' OR EXISTS(SELECT 1 FROM support_messages WHERE conversation_id=c.id AND body ILIKE '%'||$2||'%') OR c.id::text=$2) ORDER BY c.updated_at DESC,c.id DESC LIMIT 51 OFFSET $3`, status, query, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var item Summary
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Preview, &item.Unread, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Store) SetStatus(ctx context.Context, id int64, status string) error {
	if status != "open" && status != "closed" {
		return ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE support_conversations SET status=$2 WHERE id=$1`, id, status)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s *Store) Unread(ctx context.Context, id int64, staff bool) (int, error) {
	var count int
	if staff {
		err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM support_messages m JOIN support_conversations c ON c.id=m.conversation_id WHERE m.sender='customer' AND m.id>c.staff_read_id`).Scan(&count)
		return count, err
	}
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM support_messages m JOIN support_conversations c ON c.id=m.conversation_id WHERE c.id=$1 AND m.sender='staff' AND m.id>c.customer_read_id`, id).Scan(&count)
	return count, err
}

func (s *Store) Attachment(ctx context.Context, threadID, messageID int64) (string, string, string, []byte, error) {
	var mimeType, name, kind string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT image_type,attachment_name,attachment_kind,image_data FROM support_messages WHERE conversation_id=$1 AND id=$2 AND image_data IS NOT NULL`, threadID, messageID).Scan(&mimeType, &name, &kind, &data)
	if kind == "" && strings.HasPrefix(mimeType, "image/") {
		kind = "image"
	}
	if name == "" {
		name = "photo.jpg"
	}
	return mimeType, name, kind, data, err
}
