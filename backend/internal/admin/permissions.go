package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ValidateProductUpdate also protects callers outside the HTTP layer. The HTTP
// guard checks key presence first (including nulls and unexpected field names).
func ValidateProductUpdate(actor Actor, update ProductUpdate) error {
	if !Can(actor.Role, PermissionProductsEdit) {
		return ErrForbidden
	}
	if actor.Role == RoleOwner {
		return nil
	}
	if update.PriceMinor != nil || update.Stock != nil || update.SabyFields != nil || update.ExternalIDs != nil || update.WholesaleMinQty != nil || update.Image != nil || update.Status != nil && *update.Status == "archived" {
		return ErrForbidden
	}
	return nil
}

// Technical integration attributes are not content. Packaging remains editable.
// Checking even null values prevents clearing a protected attribute through PATCH.
func validateManagerAttributes(ctx context.Context, tx pgx.Tx, actor Actor, values map[string]any) error {
	if actor.Role == RoleOwner {
		return nil
	}
	for code := range values {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attribute_definitions
   WHERE code=$1 AND (audience='customer' OR code IN
   ('package_length_cm','package_width_cm','package_height_cm','package_weight_grams')))`, strings.TrimSpace(code)).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
	}
	return nil
}

type VariantContentUpdate struct {
	Label      *string        `json:"label"`
	Attributes map[string]any `json:"attributes"`
}

// UpdateVariantContent cannot write prices, stock, identity, activation or mappings.
// Lock and validate the whole request inside one transaction before saving.
func (repository *PostgresRepository) UpdateVariantContent(ctx context.Context, actor Actor, variantID int64, input VariantContentUpdate) (AdminVariant, error) {
	if !Can(actor.Role, PermissionProductsEdit) {
		return AdminVariant{}, ErrForbidden
	}
	if input.Label != nil && strings.TrimSpace(*input.Label) == "" {
		return AdminVariant{}, fmt.Errorf("%w: укажите название варианта", ErrInvalidInput)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return AdminVariant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var productID int64
	var active bool
	if err = tx.QueryRow(ctx, `SELECT product_id,is_active<>0 FROM product_variants WHERE id=$1 FOR UPDATE`, variantID).Scan(&productID, &active); err != nil {
		return AdminVariant{}, err
	}
	if err = validateManagerAttributes(ctx, tx, actor, input.Attributes); err != nil {
		return AdminVariant{}, err
	}
	if err = saveVariantPIMValues(ctx, tx, productID, variantID, input.Attributes); err != nil {
		return AdminVariant{}, err
	}
	if active {
		if err = validateRequiredVariantAttributes(ctx, tx, productID, variantID); err != nil {
			return AdminVariant{}, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE product_variants SET label=COALESCE(BTRIM($2),label),updated_at=CURRENT_TIMESTAMP WHERE id=$1`, variantID, input.Label); err != nil {
		return AdminVariant{}, err
	}
	if err = insertAudit(ctx, tx, actor, "variant.content.update", "variant", fmt.Sprint(variantID), nil, input); err != nil {
		return AdminVariant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AdminVariant{}, err
	}
	items, err := repository.ListProductVariants(ctx, productID)
	if err != nil {
		return AdminVariant{}, err
	}
	for _, item := range items {
		if item.ID == variantID {
			return item, nil
		}
	}
	return AdminVariant{}, pgx.ErrNoRows
}
