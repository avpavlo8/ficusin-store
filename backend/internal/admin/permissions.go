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
	if err := validateVariantEdits(actor, update.Variants); err != nil {
		return err
	}
	if update.SabyFields != nil {
		for _, field := range *update.SabyFields {
			if field != "stock" {
				return fmt.Errorf("%w: из СБИС поступает только остаток; контент и цена хранятся на сайте", ErrInvalidInput)
			}
		}
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
   ('package_length_cm','package_width_cm','package_height_cm','package_weight_grams','package_winter_length_cm','package_winter_width_cm','package_winter_height_cm')))`, strings.TrimSpace(code)).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
	}
	return nil
}
