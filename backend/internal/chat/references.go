package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Only identity travels from the client. All displayed facts are resolved server-side.
type ReferenceInput struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Reference struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Label     string `json:"label"`
	Image     string `json:"image,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Number    string `json:"number,omitempty"`
	Status    string `json:"status,omitempty"`
	Available bool   `json:"available"`
}
type ReferenceOptions struct {
	Items         []Reference `json:"items"`
	LoginRequired bool        `json:"loginRequired,omitempty"`
}

func validReference(input *ReferenceInput) bool {
	if input == nil {
		return true
	}
	id, err := strconv.ParseInt(input.ID, 10, 64)
	return (input.Kind == "product" || input.Kind == "order") && err == nil && id > 0 && strconv.FormatInt(id, 10) == input.ID
}

// Order attachment access follows the recipient thread, never an arbitrary
// order picked from another customer's account by a staff member.
func (s *Store) ReferenceOptions(ctx context.Context, who Identity, staffID, threadID int64, canOrders bool, kind, query string) (ReferenceOptions, error) {
	out := ReferenceOptions{Items: []Reference{}}
	query = strings.TrimSpace(query)
	if (kind != "product" && kind != "order") || len(query) > 200 {
		return out, ErrInvalid
	}
	customerID := who.CustomerID
	if staffID > 0 {
		var owner *int64
		if err := s.pool.QueryRow(ctx, `SELECT customer_id FROM support_conversations WHERE id=$1`, threadID).Scan(&owner); err != nil {
			return out, err
		}
		customerID = 0
		if owner != nil {
			customerID = *owner
		}
		if kind == "order" && !canOrders {
			return out, ErrInvalid
		}
	}
	if kind == "order" && customerID == 0 {
		out.LoginRequired = staffID == 0
		return out, nil
	}
	sql := `SELECT p.product_code::TEXT,p.name,COALESCE((SELECT COALESCE(NULLIF(m.card_url,''),NULLIF(m.large_url,''),pm.object_key) FROM product_media pm LEFT JOIN media_mirror m ON m.source_url=pm.object_key WHERE pm.product_id=p.id AND pm.variant_id IS NULL AND pm.object_key !~* '\.(mp4|mov|webm)(\?|$)' ORDER BY pm.is_primary DESC,pm.sort_order,pm.id LIMIT 1),''),''::TEXT,''::TEXT FROM products p WHERE p.status='published' AND ($1='' OR p.name ILIKE '%'||$1||'%' OR p.product_code::TEXT=$1) ORDER BY p.name,p.id LIMIT 30`
	args := []any{query}
	if kind == "order" {
		sql = `SELECT o.id::TEXT,'Заказ '||o.order_number,''::TEXT,o.order_number,o.status FROM orders o WHERE o.customer_id=$1 AND ($2='' OR o.order_number ILIKE '%'||$2||'%' OR EXISTS(SELECT 1 FROM order_items i WHERE i.order_id=o.id AND i.product_name ILIKE '%'||$2||'%')) ORDER BY o.created_at DESC,o.id DESC LIMIT 30`
		args = []any{customerID, query}
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r := Reference{Kind: kind, Available: true}
		if err = rows.Scan(&r.ID, &r.Label, &r.Image, &r.Number, &r.Status); err != nil {
			return out, err
		}
		out.Items = append(out.Items, r)
	}
	return out, rows.Err()
}
func resolveReference(ctx context.Context, tx pgx.Tx, threadID int64, input *ReferenceInput, staff bool, canOrders bool) (*Reference, error) {
	if input == nil {
		return nil, nil
	}
	if !validReference(input) {
		return nil, ErrInvalid
	}
	out := &Reference{Kind: input.Kind, ID: input.ID, Available: true}
	var err error
	if input.Kind == "product" {
		err = tx.QueryRow(ctx, `SELECT p.name,COALESCE((SELECT COALESCE(NULLIF(m.card_url,''),NULLIF(m.large_url,''),pm.object_key) FROM product_media pm LEFT JOIN media_mirror m ON m.source_url=pm.object_key WHERE pm.product_id=p.id AND pm.variant_id IS NULL AND pm.object_key !~* '\.(mp4|mov|webm)(\?|$)' ORDER BY pm.is_primary DESC,pm.sort_order,pm.id LIMIT 1),'') FROM products p WHERE p.product_code::TEXT=$1 AND p.status='published'`, input.ID).Scan(&out.Label, &out.Image)
	} else {
		if staff && !canOrders {
			return nil, ErrInvalid
		}
		err = tx.QueryRow(ctx, `SELECT 'Заказ '||o.order_number,o.order_number,o.status FROM orders o JOIN support_conversations c ON c.customer_id=o.customer_id WHERE c.id=$1 AND o.id::TEXT=$2 AND c.customer_id IS NOT NULL`, threadID, input.ID).Scan(&out.Label, &out.Number, &out.Status)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalid
	}
	return out, err
}

const referenceReadSQL = `CASE WHEN reference IS NULL THEN NULL ELSE reference || jsonb_build_object('available',CASE reference->>'kind' WHEN 'product' THEN EXISTS(SELECT 1 FROM products p WHERE p.product_code::TEXT=reference->>'id' AND p.status='published') WHEN 'order' THEN EXISTS(SELECT 1 FROM orders o JOIN support_conversations c ON c.customer_id=o.customer_id WHERE c.id=support_messages.conversation_id AND o.id::TEXT=reference->>'id' AND c.customer_id IS NOT NULL) ELSE FALSE END) END`
