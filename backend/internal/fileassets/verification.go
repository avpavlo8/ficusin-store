package fileassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type VerificationReport struct {
	Files           []MigrationRecord `json:"files"`
	Referenced      int64             `json:"referenced"`
	RemainingLegacy int64             `json:"remainingLegacy"`
	Failed          int               `json:"failed"`
	Complete        bool              `json:"complete"`
	AllKinds        bool              `json:"allKinds"`
	Ready           bool              `json:"ready"`
}

// VerifyPrivateReferences performs GETs only, never writes objects or database
// rows. A bounded sample is explicitly incomplete and cannot permit retirement.
// Retained bytes or a migration receipt supply the expected original digest.
func VerifyPrivateReferences(ctx context.Context, pool *pgxpool.Pool, storage Storage, options MigrationOptions) (VerificationReport, error) {
	report := VerificationReport{Files: []MigrationRecord{}}
	if options.Apply {
		return report, fmt.Errorf("verification cannot apply changes")
	}
	if storage == nil || !storage.Configured() {
		return report, ErrUnavailable
	}
	if options.Limit <= 0 {
		options.Limit = 100
	}
	if options.Limit > 10000 {
		return report, fmt.Errorf("limit must be at most 10000")
	}
	wanted := map[string]bool{}
	var archived bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('legacy_archive.file_binary_payloads') IS NOT NULL`).Scan(&archived); err != nil {
		return report, err
	}
	for _, kind := range options.Kinds {
		known := false
		for _, source := range migrationSources {
			if source.kind == kind {
				known = true
			}
		}
		if !known {
			return report, fmt.Errorf("unknown file kind %q", kind)
		}
		wanted[kind] = true
	}
	report.AllKinds = len(wanted) == 0 || len(wanted) == len(migrationSources)
	for _, source := range migrationSources {
		if len(wanted) > 0 && !wanted[source.kind] {
			continue
		}
		private := ""
		legacy := ""
		if source.kind == "reviews" {
			private = " AND storage_scope='private'"
			legacy = " OR (NULLIF(object_key,'') IS NOT NULL AND storage_scope<>'private')"
		}
		var referenced, remaining int64
		query := fmt.Sprintf("SELECT COUNT(*) FILTER(WHERE NULLIF(%s,'') IS NOT NULL%s),COUNT(*) FILTER(WHERE (NULLIF(%s,'') IS NULL AND %s IS NOT NULL)%s) FROM %s", source.key, private, source.key, source.data, legacy, source.table)
		if err := pool.QueryRow(ctx, query).Scan(&referenced, &remaining); err != nil {
			return report, err
		}
		report.Referenced += referenced
		report.RemainingLegacy += remaining
		expected := "h.sha256"
		archiveJoin := ""
		if archived {
			expected = "COALESCE(h.sha256,a.sha256)"
			archiveJoin = fmt.Sprintf(" LEFT JOIN legacy_archive.file_binary_payloads a ON a.kind=$2 AND a.source_id=f.id AND a.destination_key=f.%s", source.key)
		}
		query = fmt.Sprintf(`SELECT f.id,f.%s,f.%s,%s FROM %s f LEFT JOIN file_asset_migration_history h ON h.kind=$2 AND h.source_id=f.id AND h.destination_key=f.%s%s WHERE NULLIF(f.%s,'') IS NOT NULL%s ORDER BY f.id LIMIT $1`, source.key, source.data, expected, source.table, source.key, archiveJoin, source.key, private)
		rows, err := pool.Query(ctx, query, options.Limit, source.kind)
		if err != nil {
			return report, err
		}
		type entry struct {
			id       int64
			key      string
			legacy   []byte
			expected *string
		}
		var entries []entry
		for rows.Next() {
			var e entry
			if err = rows.Scan(&e.id, &e.key, &e.legacy, &e.expected); err != nil {
				rows.Close()
				return report, err
			}
			entries = append(entries, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return report, err
		}
		for _, e := range entries {
			record := MigrationRecord{Kind: source.kind, ID: e.id, Status: "readable-no-retained-source"}
			data, err := storage.Get(ctx, e.key)
			if err != nil || len(data) == 0 || len(data) > 64<<20 {
				record.Status = "unavailable-or-invalid"
				report.Failed++
			} else {
				hash := sha256.Sum256(data)
				record.SHA256 = hex.EncodeToString(hash[:])
				record.Bytes = len(data)
				expected := ""
				if e.expected != nil {
					expected = *e.expected
				} else if e.legacy != nil {
					original := sha256.Sum256(e.legacy)
					expected = hex.EncodeToString(original[:])
				}
				if expected != "" {
					if record.SHA256 == expected {
						record.Status = "checksum-verified"
					} else {
						record.Status = "checksum-mismatch"
						report.Failed++
					}
				}
			}
			// A reference replaced during GET is not proof about the new file.
			var unchanged bool
			check := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id=$1 AND %s=$2 AND %s IS NOT DISTINCT FROM $3::BYTEA%s)", source.table, source.key, source.data, private)
			if err := pool.QueryRow(ctx, check, e.id, e.key, e.legacy).Scan(&unchanged); err != nil {
				return report, err
			}
			if !unchanged {
				if record.Status != "checksum-mismatch" && record.Status != "unavailable-or-invalid" {
					report.Failed++
				}
				record.Status = "changed-concurrently-recheck-required"
			}
			report.Files = append(report.Files, record)
		}
	}
	report.Complete = int64(len(report.Files)) == report.Referenced
	report.Ready = report.AllKinds && report.Complete && report.Failed == 0 && report.RemainingLegacy == 0
	return report, nil
}
