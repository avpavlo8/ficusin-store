package procurement

import (
	"context"
	"encoding/json"
	"errors"
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
	return d, err
}

func (store *PostgresStore) CreateNamedPlanDraft(ctx context.Context, actor Actor, title string, payload json.RawMessage) (NamedPlanDraft, error) {
	var d NamedPlanDraft
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 120 || len(payload) == 0 || !json.Valid(payload) {
		return d, ErrInvalidInput
	}
	err := store.pool.QueryRow(ctx, `INSERT INTO procurement_named_plan_drafts(title,payload,created_by,updated_by) VALUES($1,$2,$3,$3) RETURNING id,title,payload,created_at,updated_at`, title, payload, actor.CustomerID).Scan(&d.ID, &d.Title, &d.Payload, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func (store *PostgresStore) UpdateNamedPlanDraft(ctx context.Context, actor Actor, id int64, title string, payload json.RawMessage) (NamedPlanDraft, error) {
	var d NamedPlanDraft
	title = strings.TrimSpace(title)
	if id <= 0 || title == "" || len(title) > 120 || len(payload) == 0 || !json.Valid(payload) {
		return d, ErrInvalidInput
	}
	err := store.pool.QueryRow(ctx, `UPDATE procurement_named_plan_drafts SET title=$2,payload=$3,updated_by=$4,updated_at=now() WHERE id=$1 AND deleted_at IS NULL RETURNING id,title,payload,created_at,updated_at`, id, title, payload, actor.CustomerID).Scan(&d.ID, &d.Title, &d.Payload, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
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
