package fileassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MigrationOptions struct {
	Apply bool
	Limit int
	Kinds []string
}
type MigrationRecord struct {
	Kind   string `json:"kind"`
	ID     int64  `json:"id"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Status string `json:"status"`
}
type migrationSource struct{ kind, table, key, data, mime, extra string }

var migrationSources = []migrationSource{
	{"avatars", "customers", "avatar_object_key", "avatar_image", "COALESCE(avatar_mime,'application/octet-stream')", ""},
	{"returns", "marketplace_return_photos", "object_key", "image", "content_type", ""},
	{"procurement", "procurement_documents", "object_key", "content", "content_type", ""},
	{"chat", "support_messages", "object_key", "image_data", "COALESCE(image_type,'application/octet-stream')", ",attachment_size=octet_length(image_data)"},
	{"reviews", "product_review_photos", "object_key", "image", "content_type", ",storage_scope='private'"},
}

// MigrateLegacy is dry-run by default. It never deletes source bytea. A compare-
// and-set reference update prevents overriding a concurrently replaced file.
func MigrateLegacy(ctx context.Context, pool *pgxpool.Pool, storage Storage, options MigrationOptions) ([]MigrationRecord, error) {
	if options.Limit <= 0 {
		options.Limit = 100
	}
	if options.Limit > 10000 {
		return nil, fmt.Errorf("limit must be at most 10000")
	}
	if options.Apply && (storage == nil || !storage.Configured()) {
		return nil, ErrUnavailable
	}
	wanted := map[string]bool{}
	for _, kind := range options.Kinds {
		wanted[kind] = true
	}
	for kind := range wanted {
		known := false
		for _, s := range migrationSources {
			if s.kind == kind {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown file kind %q", kind)
		}
	}
	result := []MigrationRecord{}
	for _, source := range migrationSources {
		if len(wanted) > 0 && !wanted[source.kind] {
			continue
		}
		query := fmt.Sprintf("SELECT id,%s,%s FROM %s WHERE NULLIF(%s,'') IS NULL AND %s IS NOT NULL ORDER BY id LIMIT $1", source.mime, source.data, source.table, source.key, source.data)
		rows, err := pool.Query(ctx, query, options.Limit)
		if err != nil {
			return result, err
		}
		type file struct {
			id   int64
			mime string
			data []byte
		}
		files := []file{}
		for rows.Next() {
			var f file
			if err = rows.Scan(&f.id, &f.mime, &f.data); err != nil {
				rows.Close()
				return result, err
			}
			files = append(files, f)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		for _, f := range files {
			hash := sha256.Sum256(f.data)
			record := MigrationRecord{source.kind, f.id, len(f.data), hex.EncodeToString(hash[:]), "would-copy"}
			if options.Apply {
				key, err := Save(ctx, storage, fmt.Sprintf("legacy/%s/%d", source.kind, f.id), f.mime, f.data)
				if err != nil {
					return result, fmt.Errorf("%s/%d: %w", source.kind, f.id, err)
				}
				update := fmt.Sprintf("UPDATE %s SET %s=$1%s WHERE id=$2 AND NULLIF(%s,'') IS NULL AND %s=$3", source.table, source.key, source.extra, source.key, source.data)
				changed, err := commitMigrationReference(ctx, pool, record, "bytea", nil, key, update, key, f.id, f.data)
				if err != nil {
					// Autocommit acknowledgement may be lost after the reference commits.
					// Keep the verified object; a later inventory can find any true orphan.
					return result, err
				}
				if changed {
					record.Status = "copied-verified-source-retained"
				} else {
					record.Status = "changed-concurrently-skipped"
					Discard(storage, key)
				}
			}
			result = append(result, record)
		}
	}
	return result, nil
}

// Reference and evidence commit together. On any uncertain commit result the
// caller keeps the verified object: deleting it could break a committed link.
func commitMigrationReference(ctx context.Context, pool *pgxpool.Pool, record MigrationRecord, scope string, sourceKey *string, key, update string, args ...any) (bool, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, update, args...)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO file_asset_migration_history(kind,source_id,source_scope,source_key,destination_key,byte_count,sha256) VALUES($1,$2,$3,$4,$5,$6,$7)`, record.Kind, record.ID, scope, sourceKey, key, record.Bytes, record.SHA256)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// MigrateLegacyPublicReviews copies the current public object, not potentially
// stale retained bytea. It preserves the original key in the evidence journal
// and never deletes the public object. Dry-run performs source GETs only.
func MigrateLegacyPublicReviews(ctx context.Context, pool *pgxpool.Pool, source, destination Storage, options MigrationOptions) ([]MigrationRecord, error) {
	if source == nil || !source.Configured() || (options.Apply && (destination == nil || !destination.Configured())) {
		return nil, ErrUnavailable
	}
	if options.Limit <= 0 {
		options.Limit = 100
	}
	if options.Limit > 10000 {
		return nil, fmt.Errorf("limit must be at most 10000")
	}
	rows, err := pool.Query(ctx, `SELECT id,object_key,content_type FROM product_review_photos WHERE storage_scope='legacy-public' AND NULLIF(object_key,'') IS NOT NULL ORDER BY id LIMIT $1`, options.Limit)
	if err != nil {
		return nil, err
	}
	type entry struct {
		id        int64
		key, mime string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.key, &e.mime); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []MigrationRecord{}
	for _, e := range entries {
		data, err := source.Get(ctx, e.key)
		if err != nil {
			return result, fmt.Errorf("read legacy review %d: %w", e.id, err)
		}
		if len(data) == 0 || len(data) > 64<<20 {
			return result, fmt.Errorf("invalid legacy review %d size", e.id)
		}
		hash := sha256.Sum256(data)
		record := MigrationRecord{"reviews", e.id, len(data), hex.EncodeToString(hash[:]), "would-copy-public-object"}
		if options.Apply {
			key, err := Save(ctx, destination, fmt.Sprintf("legacy/reviews/%d", e.id), e.mime, data)
			if err != nil {
				return result, err
			}
			changed, err := commitMigrationReference(ctx, pool, record, "legacy-public", &e.key, key, `UPDATE product_review_photos SET object_key=$1,storage_scope='private' WHERE id=$2 AND object_key=$3 AND storage_scope='legacy-public'`, key, e.id, e.key)
			if err != nil {
				return result, err
			}
			if changed {
				record.Status = "copied-verified-source-retained"
			} else {
				record.Status = "changed-concurrently-skipped"
				Discard(destination, key)
			}
		}
		result = append(result, record)
	}
	return result, nil
}
func ParseMigrationKinds(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := []string{}
	for _, k := range strings.Split(value, ",") {
		out = append(out, strings.TrimSpace(k))
	}
	return out
}
