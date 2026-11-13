package admin

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"

	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) CachedEditorReference(ctx context.Context, ch, kind, scope string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := r.pool.QueryRow(ctx, `SELECT payload FROM catalog_editor_references WHERE channel=$1 AND kind=$2 AND scope=$3 AND fetched_at>NOW()-INTERVAL '24 hours'`, ch, editorReferenceKind(r.editorPublicationMode, kind), scope).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return raw, err
}
func (r *PostgresRepository) CacheEditorReference(ctx context.Context, ch, kind, scope string, raw json.RawMessage) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO catalog_editor_references(channel,kind,scope,payload) VALUES($1,$2,$3,$4) ON CONFLICT(channel,kind,scope) DO UPDATE SET payload=EXCLUDED.payload,fetched_at=NOW()`, ch, editorReferenceKind(r.editorPublicationMode, kind), scope, raw)
	return err
}
func (r *PostgresRepository) SabyEditorReferences(ctx context.Context) (map[string]any, error) {
	if refs, ok := mockSabyEditorReferences(r.editorPublicationMode, os.Getenv("APP_ENV")); ok {
		return refs, nil
	}
	folders := []map[string]string{}
	nativeOnly := r.editorPublicationMode == "live" || r.editorEnvironment == "production"
	rows, err := r.pool.Query(ctx, `SELECT saby_id,CASE WHEN cardinality(path)>0 THEN array_to_string(path,' / ') ELSE name END FROM saby_catalog_folders WHERE missing_since IS NULL AND (NOT $1::boolean OR saby_id~'^[1-9][0-9]*$') ORDER BY path,name`, nativeOnly)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		folders = append(folders, map[string]string{"id": id, "name": name})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var categories any = []any{}
	cached, err := r.CachedEditorReference(ctx, "saby", "categories", "")
	if err != nil {
		return nil, err
	}
	if len(cached) > 0 {
		if err = json.Unmarshal(cached, &categories); err != nil {
			return nil, err
		}
	} else if r.editorSabyMetadata != nil && r.editorSabyMetadata.Configured() {
		if source, ok := r.editorSabyMetadata.(interface {
			ListSabyEditorCategories(context.Context) ([]catalogeditor.Category, error)
		}); ok {
			readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			loaded, e := source.ListSabyEditorCategories(readCtx)
			if e != nil {
				return nil, e
			}
			raw, e := json.Marshal(loaded)
			if e != nil {
				return nil, e
			}
			if e = r.CacheEditorReference(readCtx, "saby", "categories", "", raw); e != nil {
				return nil, e
			}
			categories = loaded
		}
	}
	return map[string]any{"folders": folders, "categories": categories}, nil
}

// Mock references deliberately bypass the database and its live reference cache.
// They are only selectable in an explicitly isolated preview environment; their
// IDs and names must never be mistaken for a fetched Saby catalogue.
func mockSabyEditorReferences(mode, environment string) (map[string]any, bool) {
	if mode != "mock" || (environment != "staging" && environment != "development") {
		return nil, false
	}
	return map[string]any{
		"categories": []map[string]string{
			{"id": "mock-saby-category-plants", "name": "Тест СБИС: Растения"},
			{"id": "mock-saby-category-soil", "name": "Тест СБИС: Грунты и удобрения"},
			{"id": "mock-saby-category-accessories", "name": "Тест СБИС: Кашпо и аксессуары"},
		},
		"folders": []map[string]string{
			{"id": "mock-saby-folder-plants", "name": "Тест СБИС / Растения"},
			{"id": "mock-saby-folder-soil", "name": "Тест СБИС / Грунты и удобрения"},
			{"id": "mock-saby-folder-accessories", "name": "Тест СБИС / Кашпо и аксессуары"},
		},
	}, true
}
