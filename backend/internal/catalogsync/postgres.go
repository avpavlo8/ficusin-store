package catalogsync

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var trailingPotLabel = regexp.MustCompile(`(?i)\s+[dд]\s*\d+(?:[.,]\d+)?(?:\s*см)?$`)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool} }

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func importCode(id int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("https://ficusin.ru/catalog-export/variant/%d", id)))
	h[6] = (h[6] & 15) | 80
	h[8] = (h[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func snapshot(ctx context.Context, q queryRow, id int64) (Snapshot, error) {
	var s Snapshot
	var persistent string
	err := q.QueryRow(ctx, `SELECT v.id,v.product_id,v.sku,p.name,COALESCE((SELECT (av.value#>>'{}')::float8 FROM variant_attribute_values av JOIN attribute_definitions d ON d.id=av.attribute_id WHERE av.variant_id=v.id AND d.code='height_cm' AND jsonb_typeof(av.value)='number'),v.height_cm,0)::float8,COALESCE((SELECT (av.value#>>'{}')::float8 FROM variant_attribute_values av JOIN attribute_definitions d ON d.id=av.attribute_id WHERE av.variant_id=v.id AND d.code='pot_diameter_cm' AND jsonb_typeof(av.value)='number'),v.pot_diameter_cm,0)::float8,v.base_price_minor,COALESCE(v.saby_id,''),COALESCE(n.code,''),COALESCE(n.article,''),COALESCE(n.name,''),COALESCE(e.import_code,'')
 FROM product_variants v JOIN products p ON p.id=v.product_id LEFT JOIN saby_nomenclature n ON n.saby_id=v.saby_id LEFT JOIN catalog_saby_exports e ON e.variant_id=v.id
 WHERE v.id=$1 AND v.archived_at IS NULL AND p.status<>'archived'`, id).Scan(&s.VariantID, &s.ProductID, &s.SKU, &s.Name, &s.HeightCM, &s.PotCM, &s.PriceMinor, &s.SabyID, &s.Code, &s.Article, &s.LookupName, &persistent)
	// PIM values take precedence over legacy typed dimensions. Never substitute
	// product-level dimensions from another size.
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.Name = strings.TrimSpace(trailingPotLabel.ReplaceAllString(strings.TrimSpace(s.Name), "")) + " D" + fmt.Sprintf("%g", s.PotCM)
	if s.SabyID == "" {
		s.Code = persistent
		if s.Code == "" {
			s.Code = importCode(id)
		}
		s.Article = s.SKU
	}
	return s, nil
}
func (s *PostgresStore) View(ctx context.Context, id int64) (View, error) {
	return view(ctx, s.pool, id)
}
func view(ctx context.Context, q queryRow, id int64) (View, error) {
	snap, e := snapshot(ctx, q, id)
	if e != nil {
		return View{}, e
	}
	v := View{Snapshot: snap, Fingerprint: snap.Hash(), State: "idle", Dirty: true}
	var applied string
	e = q.QueryRow(ctx, `SELECT state,message,applied_hash,verified_at FROM catalog_saby_exports WHERE variant_id=$1`, id).Scan(&v.State, &v.Message, &applied, &v.VerifiedAt)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, e
	}
	v.Dirty = applied != v.Fingerprint
	if err := snap.Validate(); err != nil {
		v.Validation = err.Error()
	}
	return v, nil
}
func (s *PostgresStore) Enqueue(ctx context.Context, id, actor int64, hash string, check bool) (View, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if e = tx.QueryRow(ctx, `SELECT id FROM product_variants WHERE id=$1 FOR UPDATE`, id).Scan(&locked); errors.Is(e, pgx.ErrNoRows) {
		return View{}, ErrNotFound
	} else if e != nil {
		return View{}, e
	}
	if !check {
		var editorBusy bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_editor_publications WHERE variant_id=$1 AND channel='saby' AND execution_mode='live' AND status IN ('prepared','queued','processing','needs_attention'))`, id).Scan(&editorBusy); e != nil {
			return View{}, e
		}
		if editorBusy {
			return View{}, fmt.Errorf("%w: дождитесь проверки отправки из редактора товара в СБИС", ErrConflict)
		}
	}
	current, e := snapshot(ctx, tx, id)
	if e != nil {
		return View{}, e
	}
	if current.SabyID != "" && current.Code != "" && !check {
		var matches int
		if e = tx.QueryRow(ctx, `SELECT COUNT(*) FROM saby_nomenclature WHERE code=$1`, current.Code).Scan(&matches); e != nil {
			return View{}, e
		}
		if matches > 1 {
			return View{}, fmt.Errorf("%w: код СБИС используется несколькими карточками; сначала устраните неоднозначность", ErrInvalid)
		}
	}
	if !check {
		if e = current.Validate(); e != nil {
			return View{}, e
		}
		if hash == "" || hash != current.Hash() {
			return View{}, fmt.Errorf("%w: обновите данные перед отправкой", ErrConflict)
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO catalog_saby_exports(variant_id,import_code) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, current.Code)
	if e != nil {
		return View{}, e
	}
	var state, applied string
	if e = tx.QueryRow(ctx, `SELECT state,applied_hash FROM catalog_saby_exports WHERE variant_id=$1 FOR UPDATE`, id).Scan(&state, &applied); e != nil {
		return View{}, e
	}
	busy := state == "queued" || state == "checking" || state == "verifying" || state == "unknown"
	if check {
		if state == "verifying" || state == "unknown" {
			_, e = tx.Exec(ctx, `UPDATE catalog_saby_exports SET next_check_at=NOW() WHERE variant_id=$1`, id)
		}
	} else if !busy && applied != hash {
		data, _ := json.Marshal(current)
		_, e = tx.Exec(ctx, `UPDATE catalog_saby_exports SET revision=revision+1,state='queued',payload=$2,payload_hash=$3,actor_id=$4,attempted_at=NULL,message='Ожидает отправки в СБИС',next_check_at=NOW(),lease_until=NULL,updated_at=NOW() WHERE variant_id=$1`, id, data, hash, actor)
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO admin_audit_log(actor_customer_id,actor_role,action,entity_type,entity_id,after_data) VALUES($1,'owner','variant.saby.export','variant',$2,$3)`, actor, fmt.Sprint(id), data)
		}
	} else if busy && !check {
		var queuedHash string
		_ = tx.QueryRow(ctx, `SELECT payload_hash FROM catalog_saby_exports WHERE variant_id=$1`, id).Scan(&queuedHash)
		if queuedHash != hash {
			return View{}, fmt.Errorf("%w: сначала дождитесь проверки предыдущей отправки в СБИС", ErrConflict)
		}
	}
	if e != nil {
		return View{}, e
	}
	v, e := view(ctx, tx, id)
	if e != nil {
		return v, e
	}
	e = tx.Commit(ctx)
	return v, e
}
func (s *PostgresStore) Claim(ctx context.Context) (*Command, error) {
	var c Command
	var raw []byte
	e := s.pool.QueryRow(ctx, `UPDATE catalog_saby_exports SET claim_token=gen_random_uuid()::text,lease_until=NOW()+INTERVAL '2 minutes',state=CASE WHEN attempted_at IS NULL THEN 'checking' ELSE state END,updated_at=NOW()
 WHERE variant_id=(SELECT variant_id FROM catalog_saby_exports WHERE state IN ('queued','checking','verifying','unknown') AND next_check_at<=NOW() AND (lease_until IS NULL OR lease_until<NOW()) ORDER BY next_check_at FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING revision,state,payload,attempted_at,claim_token`).Scan(&c.Revision, &c.State, &raw, &c.AttemptedAt, &c.ClaimToken)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &c.Snapshot); e != nil {
		return nil, e
	}
	return &c, nil
}
func (s *PostgresStore) Sent(ctx context.Context, c *Command) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if e = tx.QueryRow(ctx, `SELECT id FROM product_variants WHERE id=$1 FOR UPDATE`, c.Snapshot.VariantID).Scan(&id); e != nil {
		return e
	}
	live, e := snapshot(ctx, tx, id)
	if e != nil {
		return e
	}
	if live.Hash() != c.Snapshot.Hash() {
		return ErrConflict
	}
	var at time.Time
	e = tx.QueryRow(ctx, `UPDATE catalog_saby_exports SET state='verifying',attempted_at=NOW(),updated_at=NOW() WHERE variant_id=$1 AND revision=$2 AND attempted_at IS NULL AND state='checking' AND lease_until>NOW() AND claim_token=$3 RETURNING attempted_at`, id, c.Revision, c.ClaimToken).Scan(&at)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e == nil {
		c.AttemptedAt = &at
	}
	return e
}
func (s *PostgresStore) Release(ctx context.Context, c *Command, state, message string, delay time.Duration) error {
	_, e := s.pool.Exec(ctx, `UPDATE catalog_saby_exports SET state=$3,message=$4,lease_until=NULL,next_check_at=NOW()+$5::interval,attempted_at=CASE WHEN $3='error' THEN NULL ELSE attempted_at END,updated_at=NOW() WHERE variant_id=$1 AND revision=$2 AND claim_token=$6`, c.Snapshot.VariantID, c.Revision, state, message, fmt.Sprintf("%f seconds", delay.Seconds()), c.ClaimToken)
	return e
}
func (s *PostgresStore) Finish(ctx context.Context, c *Command, r *Remote) error {
	if !c.Snapshot.Matches(r) {
		return ErrConflict
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var productID int64
	var current string
	e = tx.QueryRow(ctx, `SELECT product_id,COALESCE(saby_id,'') FROM product_variants WHERE id=$1 FOR UPDATE`, c.Snapshot.VariantID).Scan(&productID, &current)
	if e != nil {
		return e
	}
	if current != "" && current != r.ID {
		return ErrConflict
	}
	var pID int64
	if e = tx.QueryRow(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&pID); e != nil {
		return e
	}
	var conflict bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants WHERE saby_id=$1 AND id<>$2) OR EXISTS(SELECT 1 FROM products WHERE saby_id=$1 AND id<>$3) OR EXISTS(SELECT 1 FROM product_external_ids WHERE provider='saby' AND id_type='id' AND external_id=$1 AND (product_id<>$3 OR (variant_id IS NOT NULL AND variant_id<>$2)))`, r.ID, c.Snapshot.VariantID, productID).Scan(&conflict)
	if e != nil {
		return e
	}
	if conflict {
		return ErrConflict
	}
	// This is identity verification, not inventory synchronization: do not trust
	// stock from a catalogue-write response or make existing stock payable.
	_, e = tx.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,article,name,price_minor) VALUES($1,$2,$3,$4,$5) ON CONFLICT(saby_id) DO UPDATE SET code=EXCLUDED.code,article=EXCLUDED.article,name=EXCLUDED.name,price_minor=EXCLUDED.price_minor`, r.ID, r.Code, r.Article, r.Name, r.PriceMinor)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE product_variants SET saby_id=$2,updated_at=NOW() WHERE id=$1`, c.Snapshot.VariantID, r.ID)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE products SET saby_id=COALESCE(NULLIF(saby_id,''),$2),saby_fields=CASE WHEN 'stock'=ANY(saby_fields) THEN saby_fields ELSE array_append(saby_fields,'stock') END,updated_at=NOW() WHERE id=$1`, productID, r.ID)
	if e != nil {
		return e
	}
	var mapped *int64
	e = tx.QueryRow(ctx, `SELECT variant_id FROM product_external_ids WHERE provider='saby' AND id_type='id' AND external_id=$1 FOR UPDATE`, r.ID).Scan(&mapped)
	if errors.Is(e, pgx.ErrNoRows) {
		_, e = tx.Exec(ctx, `INSERT INTO product_external_ids(product_id,variant_id,provider,id_type,external_id,status,source) VALUES($1,$2,'saby','id',$3,'active','catalog_export')`, productID, c.Snapshot.VariantID, r.ID)
	} else if e == nil && mapped == nil {
		_, e = tx.Exec(ctx, `UPDATE product_external_ids SET variant_id=$2,status='active',updated_at=NOW() WHERE provider='saby' AND id_type='id' AND external_id=$1`, r.ID, c.Snapshot.VariantID)
	}
	if e != nil {
		return e
	}
	if current == "" {
		_, e = tx.Exec(ctx, `UPDATE inventory SET available_qty=reserved_qty,synced_at=TIMESTAMPTZ '1970-01-01' WHERE variant_id=$1`, c.Snapshot.VariantID)
		if e != nil {
			return e
		}
	}
	tag, e := tx.Exec(ctx, `UPDATE catalog_saby_exports SET state='synced',remote_id=$3,applied_hash=payload_hash,verified_at=NOW(),lease_until=NULL,message='Данные подтверждены в СБИС',updated_at=NOW() WHERE variant_id=$1 AND revision=$2 AND claim_token=$4`, c.Snapshot.VariantID, c.Revision, r.ID, c.ClaimToken)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	_, e = tx.Exec(ctx, `INSERT INTO admin_audit_log(actor_customer_id,actor_role,action,entity_type,entity_id,after_data) SELECT actor_id,'owner','variant.saby.verified','variant',variant_id::text,jsonb_build_object('sabyId',$2::text,'fingerprint',payload_hash) FROM catalog_saby_exports WHERE variant_id=$1`, c.Snapshot.VariantID, r.ID)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
