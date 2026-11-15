package flowwow

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PreviewLink struct {
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	RemoteID   string `json:"remoteId"`
	OfferID    string `json:"offerId"`
	PriceMinor int64  `json:"priceMinor"`
	Stock      int    `json:"stock"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}
type LinkSnapshot struct {
	ShopID     string        `json:"shopId"`
	CapturedAt time.Time     `json:"capturedAt"`
	Items      []PreviewLink `json:"items"`
}
type PreviewItem struct {
	PreviewLink
	ProductID int64 `json:"productId,omitempty"`
}
type LinkPreview struct {
	ShopID     string        `json:"shopId"`
	CapturedAt time.Time     `json:"capturedAt"`
	Items      []PreviewItem `json:"items"`
}

func validateSnapshot(input LinkSnapshot) error {
	shop, e := strconv.ParseUint(input.ShopID, 10, 32)
	if e != nil || shop == 0 || input.CapturedAt.IsZero() || input.CapturedAt.After(time.Now().Add(5*time.Minute)) || len(input.Items) > 1000 {
		return errors.New("invalid snapshot metadata")
	}
	skus, ids, offers := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, i := range input.Items {
		id, e := strconv.ParseUint(i.RemoteID, 10, 63)
		if e != nil || id == 0 || strings.TrimSpace(i.SKU) == "" || len(i.SKU) > 128 || strings.TrimSpace(i.Name) == "" || strings.TrimSpace(i.OfferID) == "" || len([]rune(i.OfferID)) > 50 || i.PriceMinor <= 0 || i.Stock < 0 || len(i.Message) > 4000 {
			return errors.New("invalid snapshot link")
		}
		if skus[i.SKU] || ids[i.RemoteID] || offers[i.OfferID] {
			return errors.New("duplicate snapshot identity")
		}
		skus[i.SKU], ids[i.RemoteID], offers[i.OfferID] = true, true, true
	}
	return nil
}

// ImportLinkPreview never touches publication plans, remote links or inventory.
// The exact database name and environment must both identify the isolated target.
func ImportLinkPreview(ctx context.Context, pool *pgxpool.Pool, environment, mode, expectedDB string, input LinkSnapshot, apply bool) error {
	if environment != "staging" || mode != "mock" || expectedDB == "" || (!strings.Contains(expectedDB, "staging") && !strings.HasSuffix(expectedDB, "_test")) {
		return errors.New("staging mock target required")
	}
	if e := validateSnapshot(input); e != nil {
		return e
	}
	var db string
	if e := pool.QueryRow(ctx, "SELECT current_database()").Scan(&db); e != nil {
		return e
	}
	if db != expectedDB {
		return errors.New("database guard mismatch")
	}
	if !apply {
		return nil
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return e
	}
	result, e := pool.Exec(ctx, `INSERT INTO flowwow_link_preview(singleton,captured_at,payload) VALUES(TRUE,$1,$2) ON CONFLICT(singleton) DO UPDATE SET captured_at=EXCLUDED.captured_at,payload=EXCLUDED.payload WHERE flowwow_link_preview.captured_at<=EXCLUDED.captured_at`, input.CapturedAt, raw)
	if e != nil {
		return e
	}
	if result.RowsAffected() != 1 {
		return errors.New("newer snapshot already exists")
	}
	return nil
}

func (s *Service) LinkPreview(ctx context.Context) (*LinkPreview, error) {
	if s.Mode != "mock" {
		return nil, nil
	}
	var raw []byte
	e := s.Pool.QueryRow(ctx, `SELECT payload FROM flowwow_link_preview WHERE singleton`).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var input LinkSnapshot
	if e = json.Unmarshal(raw, &input); e != nil {
		return nil, e
	}
	if e = validateSnapshot(input); e != nil {
		return nil, e
	}
	skus := make([]string, 0, len(input.Items))
	for _, i := range input.Items {
		skus = append(skus, i.SKU)
	}
	// Match the staging catalogue by SKU, never by production primary keys.
	rows, e := s.Pool.Query(ctx, `SELECT sku,MIN(product_id) FROM product_variants WHERE sku=ANY($1) GROUP BY sku HAVING COUNT(*)=1`, skus)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	local := map[string]int64{}
	for rows.Next() {
		var sku string
		var id int64
		if e = rows.Scan(&sku, &id); e != nil {
			return nil, e
		}
		local[sku] = id
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	out := &LinkPreview{ShopID: input.ShopID, CapturedAt: input.CapturedAt, Items: []PreviewItem{}}
	for _, i := range input.Items {
		out.Items = append(out.Items, PreviewItem{PreviewLink: i, ProductID: local[i.SKU]})
	}
	return out, nil
}
