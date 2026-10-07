package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type NamedPlanDraft struct {
	ID        int64           `json:"id"`
	Title     string          `json:"title"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (store *PostgresStore) ListNamedPlanDrafts(ctx context.Context) ([]NamedPlanDraft, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,title,created_at,updated_at FROM procurement_named_plan_drafts WHERE deleted_at IS NULL ORDER BY updated_at DESC,id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	drafts := make([]NamedPlanDraft, 0)
	for rows.Next() {
		var d NamedPlanDraft
		if err := rows.Scan(&d.ID, &d.Title, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	return drafts, rows.Err()
}

func (store *PostgresStore) LoadNamedPlanDraft(ctx context.Context, id int64) (NamedPlanDraft, error) {
	var d NamedPlanDraft
	err := store.pool.QueryRow(ctx, `SELECT id,title,payload,created_at,updated_at FROM procurement_named_plan_drafts WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&d.ID, &d.Title, &d.Payload, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NamedPlanDraft{}, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	return d, store.applyPlanProductDefaults(ctx, &d)
}

func (store *PostgresStore) applyPlanProductDefaults(ctx context.Context, draft *NamedPlanDraft) error {
	var document planDocument
	if err := json.Unmarshal(draft.Payload, &document); err != nil {
		return err
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(draft.Payload, &original); err != nil {
		return err
	}
	var lines []map[string]json.RawMessage
	if err := json.Unmarshal(original["items"], &lines); err != nil {
		return err
	}
	for index, line := range document.Items {
		if line.SabyID == "" {
			continue
		}
		pot, err := planSize(line.PotDiameterCM)
		if err != nil {
			continue
		}
		height, err := planSize(line.HeightCM)
		if err != nil {
			continue
		}
		var category, article, price, multiple string
		err = store.pool.QueryRow(ctx, `SELECT defaults.category,defaults.supplier_article,COALESCE(defaults.unit_price::TEXT,''),defaults.units_per_package::TEXT
			FROM procurement_product_defaults defaults JOIN procurement_suppliers supplier ON supplier.id=defaults.supplier_id
			WHERE defaults.supplier_id=$1 AND defaults.saby_id=$2 AND defaults.pot_diameter_cm=$3 AND defaults.height_cm=$4
				AND defaults.currency=supplier.default_currency`, document.SupplierID, line.SabyID, pot, height).Scan(&category, &article, &price, &multiple)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		set := func(key, value string) error {
			raw, err := json.Marshal(value)
			if err == nil {
				lines[index][key] = raw
			}
			return err
		}
		if err := set("category", category); err != nil {
			return err
		}
		if err := set("article", article); err != nil {
			return err
		}
		if price != "" {
			if err := set("expectedUnitPrice", price); err != nil {
				return err
			}
		}
		if err := set("unitsPerPackage", multiple); err != nil {
			return err
		}
	}
	items, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	original["items"] = items
	payload, err := json.Marshal(original)
	if err != nil {
		return err
	}
	draft.Payload = payload
	return nil
}

type planDocument struct {
	SupplierID int64               `json:"supplierId"`
	Items      []planProductFields `json:"items"`
}

type planProductFields struct {
	SabyID            string `json:"sabyId"`
	Article           string `json:"article"`
	Category          string `json:"category"`
	ExpectedUnitPrice string `json:"expectedUnitPrice"`
	PotDiameterCM     string `json:"potDiameterCm"`
	HeightCM          string `json:"heightCm"`
	UnitsPerPackage   string `json:"unitsPerPackage"`
}

func parsePlanDocument(payload json.RawMessage) (planDocument, error) {
	var document planDocument
	if err := json.Unmarshal(payload, &document); err != nil || document.SupplierID <= 0 || len(document.Items) > 200 {
		return document, ErrInvalidInput
	}
	return document, nil
}

func planDecimal(value string) (any, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	if value == "" {
		return nil, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 9999999999 {
		return nil, ErrInvalidInput
	}
	return value, nil
}

func planSize(value string) (any, error) {
	parsed, err := planDecimal(value)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return -1, nil
	}
	amount, _ := strconv.ParseFloat(parsed.(string), 64)
	if amount > 99999999.99 {
		return nil, ErrInvalidInput
	}
	return parsed, nil
}

func (store *PostgresStore) savePlanProductFields(ctx context.Context, tx pgx.Tx, actor Actor, previous, current planDocument) error {
	old := make(map[string]planProductFields, len(previous.Items))
	if previous.SupplierID == current.SupplierID {
		for _, line := range previous.Items {
			old[line.SabyID+"\x00"+line.PotDiameterCM+"\x00"+line.HeightCM] = line
		}
	}
	for _, line := range current.Items {
		line.SabyID = strings.TrimSpace(line.SabyID)
		previousLine, existed := old[line.SabyID+"\x00"+line.PotDiameterCM+"\x00"+line.HeightCM]
		if line.SabyID == "" || (existed && previousLine == line) {
			continue
		}
		if len(line.SabyID) > 200 || len(line.Category) > 500 || len(line.Article) > 500 {
			return ErrInvalidInput
		}
		price, err := planDecimal(line.ExpectedUnitPrice)
		if err != nil {
			return err
		}
		pot, err := planSize(line.PotDiameterCM)
		if err != nil {
			return err
		}
		height, err := planSize(line.HeightCM)
		if err != nil {
			return err
		}
		multiple := 1
		if strings.TrimSpace(line.UnitsPerPackage) != "" {
			multiple, err = strconv.Atoi(line.UnitsPerPackage)
			if err != nil || multiple <= 0 || multiple > 1000000 {
				return ErrInvalidInput
			}
		}
		priceChanged := (existed && previousLine.ExpectedUnitPrice != line.ExpectedUnitPrice) || (!existed && price != nil)
		categoryChanged := (existed && previousLine.Category != line.Category) || (!existed && strings.TrimSpace(line.Category) != "")
		articleChanged := (existed && previousLine.Article != line.Article) || (!existed && strings.TrimSpace(line.Article) != "")
		multipleChanged := !existed || previousLine.UnitsPerPackage != line.UnitsPerPackage
		_, err = tx.Exec(ctx, `
			INSERT INTO procurement_product_defaults (supplier_id,saby_id,currency,supplier_article,category,unit_price,pot_diameter_cm,height_cm,units_per_package,price_source,updated_by)
			SELECT s.id,n.saby_id,s.default_currency,$3,$4,$5,$6,$7,$8,'plan',$9
   FROM procurement_suppliers s CROSS JOIN saby_nomenclature n
   WHERE s.id=$1 AND s.active AND n.saby_id=$2 AND n.missing_since IS NULL
   ON CONFLICT (supplier_id,saby_id,pot_diameter_cm,height_cm) DO UPDATE SET
    supplier_article=CASE WHEN $12 THEN EXCLUDED.supplier_article ELSE procurement_product_defaults.supplier_article END,
			currency=EXCLUDED.currency,
			category=CASE WHEN $11 THEN EXCLUDED.category ELSE procurement_product_defaults.category END,
    unit_price=CASE WHEN $10 THEN EXCLUDED.unit_price ELSE procurement_product_defaults.unit_price END,
    units_per_package=CASE WHEN $13 THEN EXCLUDED.units_per_package ELSE procurement_product_defaults.units_per_package END,
    price_source=CASE WHEN $10 THEN 'plan' ELSE procurement_product_defaults.price_source END,
    updated_by=EXCLUDED.updated_by,updated_at=now()
  `, current.SupplierID, line.SabyID, strings.TrimSpace(line.Article), strings.TrimSpace(line.Category), price, pot, height, multiple, actor.CustomerID,
			priceChanged, categoryChanged, articleChanged, multipleChanged)
		if err != nil {
			return fmt.Errorf("save plan product fields: %w", err)
		}
	}
	return nil
}

func (store *PostgresStore) CreateNamedPlanDraft(ctx context.Context, actor Actor, title string, payload json.RawMessage) (NamedPlanDraft, error) {
	var d NamedPlanDraft
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 120 || len(payload) == 0 || !json.Valid(payload) {
		return d, ErrInvalidInput
	}
	current, err := parsePlanDocument(payload)
	if err != nil {
		return d, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	err = tx.QueryRow(ctx, `INSERT INTO procurement_named_plan_drafts(title,payload,created_by,updated_by) VALUES($1,$2,$3,$3) RETURNING id,title,payload,created_at,updated_at`, title, payload, actor.CustomerID).Scan(&d.ID, &d.Title, &d.Payload, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	if err = store.savePlanProductFields(ctx, tx, actor, planDocument{}, current); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (store *PostgresStore) UpdateNamedPlanDraft(ctx context.Context, actor Actor, id int64, title string, payload json.RawMessage) (NamedPlanDraft, error) {
	var d NamedPlanDraft
	title = strings.TrimSpace(title)
	if id <= 0 || title == "" || len(title) > 120 || len(payload) == 0 || !json.Valid(payload) {
		return d, ErrInvalidInput
	}
	current, err := parsePlanDocument(payload)
	if err != nil {
		return d, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var oldPayload json.RawMessage
	err = tx.QueryRow(ctx, `SELECT payload FROM procurement_named_plan_drafts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&oldPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	previous, err := parsePlanDocument(oldPayload)
	if err != nil {
		return d, err
	}
	err = tx.QueryRow(ctx, `UPDATE procurement_named_plan_drafts SET title=$2,payload=$3,updated_by=$4,updated_at=now() WHERE id=$1 AND deleted_at IS NULL RETURNING id,title,payload,created_at,updated_at`, id, title, payload, actor.CustomerID).Scan(&d.ID, &d.Title, &d.Payload, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	if err = store.savePlanProductFields(ctx, tx, actor, previous, current); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (store *PostgresStore) DeleteNamedPlanDraft(ctx context.Context, id int64) error {
	tag, err := store.pool.Exec(ctx, `UPDATE procurement_named_plan_drafts SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
