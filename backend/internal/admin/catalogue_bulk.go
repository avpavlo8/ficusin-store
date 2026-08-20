package admin

import (
	"context"
	"errors"
	"fmt"
)

const maxBulkSabyProducts = 5000

// ImportAllProducts imports every current row from the local Saby directory.
//
// The background Saby sync deliberately does not create storefront cards: it
// only refreshes saby_nomenclature. This explicit, permission-gated action is
// the bridge between that directory and the storefront. ImportProducts owns
// the actual creation/deduplication rules, so importing all and importing by
// article can never drift apart.
func (repository *PostgresRepository) ImportAllProducts(
	ctx context.Context,
	actor Actor,
	dryRun bool,
) (ImportResult, error) {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ImportResult{}, ErrForbidden
	}
	if repository == nil || repository.pool == nil {
		return ImportResult{}, errors.New("catalogue store is unavailable")
	}

	rows, err := repository.pool.Query(ctx, `
		SELECT saby_id
		FROM saby_nomenclature
		WHERE missing_since IS NULL
		ORDER BY name, saby_id
		LIMIT $1
	`, maxBulkSabyProducts+1)
	if err != nil {
		return ImportResult{}, fmt.Errorf("list current Saby nomenclature: %w", err)
	}
	defer rows.Close()

	codes := make([]string, 0, 512)
	for rows.Next() {
		var sabyID string
		if err := rows.Scan(&sabyID); err != nil {
			return ImportResult{}, fmt.Errorf("scan current Saby nomenclature: %w", err)
		}
		if sabyID != "" {
			codes = append(codes, sabyID)
		}
	}
	if err := rows.Err(); err != nil {
		return ImportResult{}, fmt.Errorf("read current Saby nomenclature: %w", err)
	}
	if len(codes) > maxBulkSabyProducts {
		return ImportResult{}, fmt.Errorf("%w: в справочнике больше %d актуальных позиций", ErrInvalidInput, maxBulkSabyProducts)
	}
	if len(codes) == 0 {
		return ImportResult{Entries: []ImportEntry{}}, nil
	}

	return repository.ImportProducts(ctx, actor, ImportRequest{Codes: codes, DryRun: dryRun})
}
