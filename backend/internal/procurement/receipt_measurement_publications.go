package procurement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

var sabyMeasurementNameSuffix = regexp.MustCompile(`(?i)\s+[dд]\s*\d+(?:[.,]\d+)?(?:\s*см)?$`)

type measurementPublication struct {
	channel    string
	status     string
	linkedID   string
	remoteName string
	payload    []byte
	pending    []byte
	progress   []byte
}

// A measurement update uses the catalogue editor's durable worker and its
// established identity, mode and price coordination. It never creates or links
// an external card, and never resets an unresolved external operation.
func queueProcurementMeasurements(ctx context.Context, tx pgx.Tx, m checkedProcurementMeasurements, potChanged, heightChanged bool, channel string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT p.channel, p.status, p.payload, p.pending_payload, p.progress,
  CASE p.channel
    WHEN 'saby' THEN CASE WHEN d.saby_id<>'' AND d.saby_id=link.remote_id
      AND (SELECT COUNT(*) FROM canonical_product_directory other WHERE other.active AND other.saby_id=d.saby_id)=1 THEN link.remote_id ELSE '' END
    WHEN 'wb' THEN CASE WHEN $2='' OR p.execution_mode='mock' OR link.remote_id=d.wb_nm_ids[1] THEN link.remote_id ELSE '' END
    WHEN 'ozon' THEN CASE WHEN $2='' OR p.execution_mode='mock' OR link.offer_id=d.ozon_articles[1] THEN link.offer_id ELSE '' END
    WHEN 'flowwow' THEN COALESCE(link.offer_id,'')
  END, COALESCE(saby.name,'')
FROM catalog_editor_publications p JOIN canonical_product_directory d ON d.variant_id=p.variant_id
JOIN catalog_editor_remote_links link ON link.variant_id=p.variant_id AND link.channel=p.channel AND link.execution_mode=p.execution_mode
LEFT JOIN saby_nomenclature saby ON p.channel='saby' AND saby.saby_id=link.remote_id AND saby.missing_since IS NULL
WHERE p.variant_id=$1 AND d.active AND p.channel IN ('saby','wb','ozon','flowwow') AND p.execution_mode IN ('mock','live')
  AND (p.channel<>'saby' OR p.execution_mode='live')
  AND ($2='' OR p.channel=$2)
  AND (p.channel='saby' OR EXISTS(SELECT 1 FROM product_variants v WHERE v.id=p.variant_id AND v.marketplace_scope<>'site_only'))
ORDER BY p.channel FOR UPDATE OF p`, m.variantID, channel)
	if err != nil {
		return nil, fmt.Errorf("load catalogue publication targets: %w", err)
	}
	publications, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (measurementPublication, error) {
		var p measurementPublication
		err := row.Scan(&p.channel, &p.status, &p.payload, &p.pending, &p.progress, &p.linkedID, &p.remoteName)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("read catalogue publication targets: %w", err)
	}
	queued := make([]string, 0, len(publications))
	for _, p := range publications {
		if p.linkedID == "" || p.channel == "saby" && !potChanged {
			continue
		}
		if channel == "saby" {
			remotePot := parseDimension(p.remoteName, potPattern)
			if remotePot != nil && *remotePot == m.pot && len(p.pending) == 0 &&
				oneOf(p.status, "created", "linked", "synced") {
				continue
			}
		}
		raw, pending, valid := p.nextMeasurements(m, potChanged, heightChanged)
		if !valid {
			continue
		}
		// Re-approval of the same captured dimensions must not restart a
		// successfully confirmed or already in-flight marketplace update.
		if sameMeasurementPublication(raw, p) {
			if channel == "" || !oneOf(p.status, "synced", "simulated") {
				queued = append(queued, p.channel)
			}
			continue
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(raw))
		if pending {
			_, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET pending_payload=$3,pending_fingerprint=$4,updated_at=CURRENT_TIMESTAMP WHERE variant_id=$1 AND channel=$2`, m.variantID, p.channel, raw, fingerprint)
		} else {
			// Carry the verified gallery baseline for a concurrent editor save.
			// The measurement operation itself never writes photographs.
			_, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET payload=$3,fingerprint=$4,
  pending_payload=NULL,pending_fingerprint=NULL,
  progress=jsonb_strip_nulls(jsonb_build_object('sitePhotos',progress->'sitePhotos')),
  status='prepared',message='Подтверждённые размеры из закупки ожидают отправки',attempts=0,
  next_attempt_at=CURRENT_TIMESTAMP,lease_until=NULL,updated_at=CURRENT_TIMESTAMP
WHERE variant_id=$1 AND channel=$2`, m.variantID, p.channel, raw, fingerprint)
		}
		if err != nil {
			return nil, fmt.Errorf("queue checked procurement measurements for %s: %w", p.channel, err)
		}
		queued = append(queued, p.channel)
	}
	return queued, nil
}

func (p measurementPublication) nextMeasurements(m checkedProcurementMeasurements, potChanged, heightChanged bool) ([]byte, bool, bool) {
	var state struct {
		Ambiguous bool   `json:"ambiguous"`
		Stage     string `json:"stage"`
		IDs       struct {
			ProductID string `json:"productId"`
			OfferID   string `json:"offerId"`
		} `json:"ids"`
	}
	if json.Unmarshal(p.progress, &state) != nil || state.Ambiguous {
		return nil, false, false
	}
	finished := p.status == "created" || p.status == "synced" || p.status == "linked" || p.status == "simulated"
	inProgress := p.status == "prepared" || p.status == "queued" || p.status == "processing"
	if !finished && !inProgress {
		return nil, false, false
	}
	target := state.IDs.ProductID
	if p.channel == "ozon" || p.channel == "flowwow" {
		target = state.IDs.OfferID
	}
	base := p.payload
	if len(p.pending) > 0 {
		base = p.pending
	}
	var fields map[string]json.RawMessage
	var identity struct {
		ProductID    int64  `json:"productId"`
		VariantID    int64  `json:"variantId"`
		Action       string `json:"action"`
		ExistingID   string `json:"existingId"`
		Measurements bool   `json:"procurementMeasurements"`
	}
	if json.Unmarshal(base, &identity) != nil || json.Unmarshal(base, &fields) != nil || fields == nil || identity.ProductID != m.productID || identity.VariantID != m.variantID {
		return nil, false, false
	}
	if identity.Action == "update" {
		if identity.ExistingID != p.linkedID || target != "" && target != p.linkedID {
			return nil, false, false
		}
	} else if !finished || target != p.linkedID || len(p.pending) > 0 {
		return nil, false, false
	}
	// Measured dimensions cannot overwrite an unpublished card edit. Merge
	// with an already scheduled measurement revision, or leave other work
	// owned by the editor and report the absence of an automatic sync.
	if (!finished || len(p.pending) > 0) && !identity.Measurements {
		return nil, false, false
	}
	put := func(key string, value any) { fields[key], _ = json.Marshal(value) }
	put("action", "update")
	put("existingId", p.linkedID)
	put("procurementMeasurements", true)
	dimensions := map[string]float64{}
	if identity.Measurements {
		_ = json.Unmarshal(fields["attributes"], &dimensions)
	}
	if dimensions == nil {
		dimensions = map[string]float64{}
	}
	if potChanged {
		dimensions["pot_diameter_cm"] = m.pot
	}
	if heightChanged && p.channel != "saby" {
		dimensions["height_cm"] = m.height
	}
	put("attributes", dimensions)
	if p.channel == "saby" {
		name := strings.TrimSpace(sabyMeasurementNameSuffix.ReplaceAllString(strings.TrimSpace(m.name), ""))
		put("name", name+" D"+strconv.FormatFloat(m.pot, 'f', -1, 64))
		put("sabyId", p.linkedID)
	}
	raw, err := json.Marshal(fields)
	pending := inProgress && (p.status != "prepared" || state.Stage != "")
	return raw, pending, err == nil
}

func sameMeasurementPublication(raw []byte, p measurementPublication) bool {
	current := p.payload
	if len(p.pending) > 0 {
		current = p.pending
	}
	// PostgreSQL JSONB changes spacing and key order when it returns a row.
	var fields map[string]json.RawMessage
	if json.Unmarshal(current, &fields) != nil {
		return false
	}
	canonical, err := json.Marshal(fields)
	return err == nil && string(canonical) == string(raw)
}
