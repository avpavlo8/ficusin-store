package procurement

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A supplier invoice describes the incoming plants. Only the operator's checked
// measurements are suitable for replacing the selling variant's dimensions.
func checkProcurementReceiptMeasurements(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var unchecked, conflicts int
	err := tx.QueryRow(ctx, `WITH receipt_lines AS (
  SELECT line.canonical_variant_id, line.pot_diameter_cm, line.height_cm,
    line.dimensions_reviewed, line.receiving_mode
  FROM procurement_order_lines line
  WHERE line.procurement_order_id=$1 AND line.receiving_parent_line_id IS NULL
    AND NOT line.invoice_excluded AND line.reconciliation_status NOT IN ('missing','superseded')
    AND line.match_status<>'ignored' AND line.invoiced_qty>0
    AND EXISTS (SELECT 1 FROM procurement_documents d WHERE d.procurement_order_id=$1 AND d.superseded_at IS NULL)
), conflicting_variants AS (
  SELECT canonical_variant_id FROM receipt_lines
  WHERE canonical_variant_id IS NOT NULL AND receiving_mode='catalog'
  GROUP BY canonical_variant_id
  HAVING COUNT(DISTINCT (pot_diameter_cm, height_cm))>1
)
SELECT COUNT(*) FILTER (WHERE NOT dimensions_reviewed OR COALESCE(pot_diameter_cm,0)<=0 OR COALESCE(height_cm,0)<=0),
  (SELECT COUNT(*) FROM conflicting_variants)
FROM receipt_lines`, orderID).Scan(&unchecked, &conflicts)
	if err != nil {
		return fmt.Errorf("check procurement receipt measurements: %w", err)
	}
	if unchecked > 0 {
		return &UserFacingError{Message: fmt.Sprintf("Подтвердите диаметр горшка и высоту в %d строках инвойса перед приёмкой.", unchecked)}
	}
	if conflicts > 0 {
		return &UserFacingError{Message: "У одной карточки сайта получились разные размеры. Сверьте замеры и при необходимости выберите разные варианты."}
	}
	return nil
}

func finalizeProcurementReceipt(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if err := checkProcurementReceiptMeasurements(ctx, tx, orderID); err != nil {
		return err
	}
	if _, err := saveProcurementReceiptMeasurements(ctx, tx, Actor{}, orderID, true); err != nil {
		return err
	}
	return fulfilOrderAllocations(ctx, tx, orderID)
}

type checkedProcurementMeasurements struct {
	productID           int64
	variantID           int64
	name                string
	label               string
	pot                 float64
	height              float64
	oldPot              *float64
	oldHeight           *float64
	orderApprovedPot    bool
	orderApprovedHeight bool
	avitoLinked         bool
}

func lockProcurementCatalogueProducts(ctx context.Context, tx pgx.Tx, orderID int64) error {
	// Match the product editor's product -> variant lock order, including
	// sibling variants of this procurement that share a product.
	if _, err := tx.Exec(ctx, `SELECT p.id FROM products p WHERE EXISTS (
  SELECT 1 FROM procurement_order_lines l JOIN product_variants v ON v.id=l.canonical_variant_id
  WHERE l.procurement_order_id=$1 AND v.product_id=p.id
) ORDER BY p.id FOR UPDATE`, orderID); err != nil {
		return fmt.Errorf("lock catalogue products for procurement: %w", err)
	}
	return nil
}

func loadProcurementReceiptMeasurements(ctx context.Context, tx pgx.Tx, orderID int64, readApproval bool) ([]checkedProcurementMeasurements, error) {
	rows, err := tx.Query(ctx, `WITH receipt_measurements AS (
  SELECT canonical_variant_id, MIN(pot_diameter_cm)::float8 AS pot, MIN(height_cm)::float8 AS height
  FROM procurement_order_lines
  WHERE procurement_order_id=$1 AND canonical_variant_id IS NOT NULL AND receiving_parent_line_id IS NULL
    AND match_status='confirmed' AND receiving_mode='catalog' AND invoiced_qty>0
    AND NOT invoice_excluded AND reconciliation_status NOT IN ('missing','superseded')
    AND dimensions_reviewed AND pot_diameter_cm>0 AND height_cm>0
  GROUP BY canonical_variant_id
  HAVING COUNT(DISTINCT (pot_diameter_cm,height_cm))=1
)
SELECT p.id, v.id, p.name, v.label, m.pot, m.height,
  variant_numeric_attribute(v.id,'pot_diameter_cm')::float8,
  variant_numeric_attribute(v.id,'height_cm')::float8,
  COALESCE(approved.pot,FALSE), COALESCE(approved.height,FALSE),
  $2 AND EXISTS(SELECT 1 FROM avito_listing_products lp WHERE lp.product_id=p.id)
FROM receipt_measurements m JOIN product_variants v ON v.id=m.canonical_variant_id
JOIN products p ON p.id=v.product_id
LEFT JOIN LATERAL (
 SELECT BOOL_OR((a.after_data->>'potDiameterCm')::float8=m.pot AND (a.after_data->>'previousPotDiameterCm')::float8 IS DISTINCT FROM m.pot) pot,
        BOOL_OR((a.after_data->>'heightCm')::float8=m.height AND (a.after_data->>'previousHeightCm')::float8 IS DISTINCT FROM m.height) height
 FROM admin_audit_log a
 WHERE $2 AND a.entity_type='variant' AND a.entity_id=v.id::text AND a.action='procurement.variant.measurements'
  AND a.after_data->>'orderId'=$1::bigint::text
) approved ON $2
WHERE v.archived_at IS NULL
ORDER BY v.id FOR UPDATE OF v`, orderID, readApproval)
	if err != nil {
		return nil, fmt.Errorf("lock measured catalogue variants: %w", err)
	}
	measurements, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (checkedProcurementMeasurements, error) {
		var m checkedProcurementMeasurements
		err := row.Scan(&m.productID, &m.variantID, &m.name, &m.label, &m.pot, &m.height, &m.oldPot, &m.oldHeight, &m.orderApprovedPot, &m.orderApprovedHeight, &m.avitoLinked)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("read measured catalogue variants: %w", err)
	}
	return measurements, nil
}

func saveProcurementReceiptMeasurements(ctx context.Context, tx pgx.Tx, actor Actor, orderID int64, forward bool) (int, error) {
	if err := lockProcurementCatalogueProducts(ctx, tx, orderID); err != nil {
		return 0, err
	}
	measurements, err := loadProcurementReceiptMeasurements(ctx, tx, orderID, false)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, m := range measurements {
		potChanged := m.oldPot == nil || *m.oldPot != m.pot
		heightChanged := m.oldHeight == nil || *m.oldHeight != m.height
		if !potChanged && !heightChanged {
			continue
		}
		tag, err := tx.Exec(ctx, `INSERT INTO variant_attribute_values(variant_id,attribute_id,value,source,updated_at)
SELECT $1,d.id,to_jsonb(a.value),'local',CURRENT_TIMESTAMP
FROM (VALUES ('pot_diameter_cm',$2::numeric),('height_cm',$3::numeric)) AS a(code,value)
JOIN attribute_definitions d ON d.code=a.code AND d.value_scope='variant' AND d.data_type='number' AND d.is_active
ON CONFLICT(variant_id,attribute_id) DO UPDATE
SET value=EXCLUDED.value,source=EXCLUDED.source,updated_at=EXCLUDED.updated_at`, m.variantID, m.pot, m.height)
		if err != nil {
			return 0, fmt.Errorf("save checked procurement measurements for site variant %d: %w", m.variantID, err)
		}
		if tag.RowsAffected() != 2 {
			return 0, fmt.Errorf("procurement measurement attributes are unavailable for site variant %d", m.variantID)
		}
		label := measuredVariantLabel(m.label, m.pot, m.height)
		if _, err = tx.Exec(ctx, `UPDATE product_variants v SET updated_at=CURRENT_TIMESTAMP,
  label=CASE WHEN NOT EXISTS(SELECT 1 FROM product_variants sibling WHERE sibling.product_id=v.product_id
      AND sibling.id<>v.id AND sibling.archived_at IS NULL AND LOWER(sibling.label)=LOWER($2)) THEN $2 ELSE v.label END
  WHERE v.id=$1`, m.variantID, label); err != nil {
			return 0, fmt.Errorf("refresh measured site variant %d: %w", m.variantID, err)
		}
		channels := []string{}
		if forward {
			channels, err = queueProcurementMeasurements(ctx, tx, m, potChanged, heightChanged, "")
			if err != nil {
				return 0, err
			}
		}
		if err = audit(ctx, tx, actor, "procurement.variant.measurements", "variant", m.variantID,
			map[string]any{"orderId": orderID, "potDiameterCm": m.pot, "heightCm": m.height, "previousPotDiameterCm": m.oldPot, "previousHeightCm": m.oldHeight, "queuedChannels": channels}); err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}

func approveChannelMeasurements(ctx context.Context, tx pgx.Tx, orderID int64, channel string) (int, error) {
	measurements, err := loadProcurementReceiptMeasurements(ctx, tx, orderID, true)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, m := range measurements {
		potIsCurrent := m.oldPot != nil && *m.oldPot == m.pot
		heightIsCurrent := m.oldHeight != nil && *m.oldHeight == m.height
		if !potIsCurrent || !heightIsCurrent {
			return 0, &UserFacingError{Message: "Сначала сохраните подтверждённые размеры в карточках сайта, затем отправьте изменения выбранному каналу."}
		}
		if channel == "avito" {
			if m.avitoLinked && (m.orderApprovedPot || m.orderApprovedHeight) {
				updated++ // Avito reads the approved site attributes at its next XML refresh.
			}
			continue
		}
		// The confirmed site values may already match the invoice while a sales
		// channel still has older dimensions. An explicit send also repairs that drift.
		channels, err := queueProcurementMeasurements(ctx, tx, m, true, true, channel)
		if err != nil {
			return 0, err
		}
		updated += len(channels)
	}
	return updated, nil
}

var simplePotLabel = regexp.MustCompile(`(?i)^[dд]\s*[0-9]+(?:[.,][0-9]+)?(?:\s*см)?$`)
var simpleSizeLabel = regexp.MustCompile(`(?i)^[0-9]+(?:[.,][0-9]+)?\s*см\s*·\s*[dд]\s*[0-9]+(?:[.,][0-9]+)?(?:\s*см)?$`)

func measuredVariantLabel(label string, pot, height float64) string {
	label = strings.TrimSpace(label)
	diameter := "D" + strconv.FormatFloat(pot, 'f', -1, 64)
	switch {
	case simplePotLabel.MatchString(label):
		return diameter
	case simpleSizeLabel.MatchString(label):
		return strconv.FormatFloat(height, 'f', -1, 64) + " см · " + diameter
	default:
		return label
	}
}
