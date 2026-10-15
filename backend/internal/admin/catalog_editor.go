package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// VariantEdit is a patch, including for managers: omitted commercial fields
// stay unchanged. Non-positive IDs identify new local drafts.
type VariantEdit struct {
	ID              int64          `json:"id"`
	Label           *string        `json:"label"`
	PriceMinor      *int64         `json:"priceMinor"`
	WholesaleMinQty *int           `json:"wholesaleMinQty"`
	Archived        *bool          `json:"archived"`
	Active          *bool          `json:"active"`
	Attributes      map[string]any `json:"attributes"`
	ExternalIDs     *[]ExternalID  `json:"externalIds"`
}

func validateVariantEdits(actor Actor, edits []VariantEdit) error {
	if len(edits) > 100 {
		return fmt.Errorf("%w: за раз можно сохранить не более 100 размеров", ErrInvalidInput)
	}
	seen := map[int64]bool{}
	for _, edit := range edits {
		if seen[edit.ID] {
			return fmt.Errorf("%w: размер повторяется в запросе", ErrInvalidInput)
		}
		seen[edit.ID] = true
		if actor.Role != RoleOwner && (edit.ID <= 0 || edit.PriceMinor != nil || edit.WholesaleMinQty != nil || edit.Active != nil || edit.Archived != nil || edit.ExternalIDs != nil) {
			return ErrForbidden
		}
		if edit.Archived != nil && *edit.Archived && (edit.Active != nil && *edit.Active || edit.ID <= 0) {
			return fmt.Errorf("%w: архивный размер не может быть новым или активным", ErrInvalidInput)
		}
		if edit.Label != nil && strings.TrimSpace(*edit.Label) == "" {
			return fmt.Errorf("%w: укажите название размера", ErrInvalidInput)
		}
		if edit.PriceMinor != nil && *edit.PriceMinor < 0 || edit.WholesaleMinQty != nil && *edit.WholesaleMinQty < 1 {
			return fmt.Errorf("%w: неверная цена или оптовый минимум", ErrInvalidInput)
		}
	}
	return nil
}

// All product and size changes share UpdateProduct's transaction. A rejected
// size rolls back the content, other sizes and outgoing price proposals too.
func saveVariantEdits(ctx context.Context, tx pgx.Tx, actor Actor, productID int64, edits []VariantEdit) error {
	for _, edit := range edits {
		if err := validateManagerAttributes(ctx, tx, actor, edit.Attributes); err != nil {
			return err
		}
		id := edit.ID
		var oldPrice int64
		if id <= 0 {
			if edit.Label == nil {
				return fmt.Errorf("%w: укажите название нового размера", ErrInvalidInput)
			}
			err := tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,wholesale_min_qty,is_active,updated_at)
    VALUES($1,BTRIM($2),COALESCE($3,0),COALESCE($4,1),COALESCE($5,FALSE)::int,CURRENT_TIMESTAMP) RETURNING id`, productID, edit.Label, edit.PriceMinor, edit.WholesaleMinQty, edit.Active).Scan(&id)
			if err != nil {
				return err
			}
			// No stock is created or copied: only the Saby inventory exchange owns it.
		} else {
			if err := tx.QueryRow(ctx, `SELECT base_price_minor FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, id, productID).Scan(&oldPrice); err != nil {
				return fmt.Errorf("%w: размер не принадлежит этой карточке", ErrInvalidInput)
			}
			if _, err := tx.Exec(ctx, `UPDATE product_variants SET label=COALESCE(BTRIM($2),label),base_price_minor=COALESCE($3,base_price_minor),wholesale_min_qty=COALESCE($4,wholesale_min_qty),is_active=COALESCE($5,is_active<>0)::int,archived_at=CASE WHEN $6 THEN CURRENT_TIMESTAMP WHEN $5 THEN NULL WHEN $6=FALSE THEN NULL ELSE archived_at END,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, edit.Label, edit.PriceMinor, edit.WholesaleMinQty, edit.Active, edit.Archived); err != nil {
				return err
			}
		}
		if err := saveVariantPIMValues(ctx, tx, productID, id, edit.Attributes); err != nil {
			return err
		}
		var active bool
		var currentPrice int64
		var status string
		if err := tx.QueryRow(ctx, `SELECT v.is_active<>0,v.base_price_minor,p.status FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, id).Scan(&active, &currentPrice, &status); err != nil {
			return err
		}
		if active {
			if status == "published" && currentPrice <= 0 {
				return fmt.Errorf("%w: цена активного размера должна быть больше нуля", ErrInvalidInput)
			}
			if err := validateRequiredVariantAttributes(ctx, tx, productID, id); err != nil {
				return err
			}
		}
		if edit.ExternalIDs != nil {
			if err := replaceVariantExternalIDs(ctx, tx, productID, id, *edit.ExternalIDs); err != nil {
				return err
			}
		}
		if edit.ID > 0 && edit.PriceMinor != nil && oldPrice != *edit.PriceMinor {
			if err := createManualPriceProposals(ctx, tx, actor, id, oldPrice, *edit.PriceMinor); err != nil {
				return err
			}
		}
		if err := insertAudit(ctx, tx, actor, "variant.editor.save", "variant", fmt.Sprint(id), nil, edit); err != nil {
			return err
		}
	}
	return nil
}

// Explicit publication uses the same invariant from individual and bulk editors.
func validatePublishedSizes(ctx context.Context, tx pgx.Tx, productID int64) error {
	rows, err := tx.Query(ctx, `SELECT id,label,base_price_minor FROM product_variants WHERE product_id=$1 AND is_active<>0 AND archived_at IS NULL ORDER BY id`, productID)
	if err != nil {
		return err
	}
	type size struct {
		id    int64
		label string
		price int64
	}
	sizes := []size{}
	for rows.Next() {
		var item size
		if err := rows.Scan(&item.id, &item.label, &item.price); err != nil {
			rows.Close()
			return err
		}
		sizes = append(sizes, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(sizes) == 0 {
		return fmt.Errorf("%w: добавьте активный размер с ценой", ErrInvalidInput)
	}
	for _, item := range sizes {
		if item.price <= 0 {
			return fmt.Errorf("%w: укажите цену размера «%s»", ErrInvalidInput, item.label)
		}
		if err := validateRequiredVariantAttributes(ctx, tx, productID, item.id); err != nil {
			return fmt.Errorf("размер «%s»: %w", item.label, err)
		}
	}
	return nil
}
