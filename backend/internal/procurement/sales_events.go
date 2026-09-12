package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type normalizedSalesEvent struct {
	SalesRecord
	Effect int
}

func normalizeSalesEvents(records []SalesRecord, from, to time.Time) ([]normalizedSalesEvent, error) {
	result := make([]normalizedSalesEvent, 0, len(records))
	for _, input := range records {
		input.ExternalID = strings.TrimSpace(input.ExternalID)
		input.SabyID = strings.TrimSpace(input.SabyID)
		input.SourceEventID = strings.TrimSpace(input.SourceEventID)
		input.SourceDocumentID = strings.TrimSpace(input.SourceDocumentID)
		input.SourceLineID = strings.TrimSpace(input.SourceLineID)
		input.CrossSourceKey = strings.TrimSpace(input.CrossSourceKey)
		input.EventType = strings.TrimSpace(input.EventType)
		input.EventStatus = strings.TrimSpace(input.EventStatus)
		date := day(input.Date)
		if input.ExternalID == "" || date.Before(from) || date.After(to) {
			return nil, ErrInvalidInput
		}
		if input.SourceEventID == "" {
			input.SourceEventID = "aggregate:" + date.Format("2006-01-02")
		}
		if input.SourceDocumentID == "" {
			input.SourceDocumentID = input.SourceEventID
		}
		if input.SourceLineID == "" {
			input.SourceLineID = input.ExternalID
		}
		effect := 1
		if input.Units < 0 || input.GrossRUB < 0 || input.EventType == "return" {
			effect = -1
		}
		if input.EventType == "" {
			if effect < 0 {
				input.EventType = "return"
			} else {
				input.EventType = "sale"
			}
		}
		if input.EventStatus == "" {
			input.EventStatus = "confirmed"
		}
		if !oneOf(input.EventType, "sale", "return", "cancellation", "correction") ||
			!oneOf(input.EventStatus, "confirmed", "pending", "cancelled") ||
			len(input.SourceEventID) > 500 || len(input.SourceDocumentID) > 500 ||
			len(input.SourceLineID) > 500 || len(input.CrossSourceKey) > 500 {
			return nil, ErrInvalidInput
		}
		input.Units = int(math.Abs(float64(input.Units)))
		input.GrossRUB = math.Abs(input.GrossRUB)
		result = append(result, normalizedSalesEvent{SalesRecord: input, Effect: effect})
	}
	return result, nil
}

func replaceSalesEvents(ctx context.Context, tx pgx.Tx, channel string, from, to time.Time, records []SalesRecord) (int, error) {
	events, err := normalizeSalesEvents(records, day(from), day(to))
	if err != nil {
		return 0, err
	}
	var batchID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::TEXT`).Scan(&batchID); err != nil {
		return 0, err
	}
	inserted := 0
	for _, event := range events {
		raw, _ := json.Marshal(map[string]any{"sourceDocumentId": event.SourceDocumentID, "sourceLineId": event.SourceLineID})
		command, err := tx.Exec(ctx, `
			WITH resolved AS (
				SELECT directory.variant_id,directory.saby_id,external.id external_mapping_id,directory.product_id
				FROM canonical_product_directory directory
				LEFT JOIN LATERAL (
					SELECT mapping.id FROM product_external_ids mapping
					WHERE mapping.variant_id=directory.variant_id AND mapping.external_id=$4
						AND mapping.status IN ('active','legacy') AND (
							($1='saby' AND mapping.provider='saby' AND mapping.id_type IN ('id','code','alias')) OR
							($1='wb' AND mapping.provider='wildberries' AND mapping.id_type IN ('sku','nm_id')) OR
							($1='ozon' AND mapping.provider='ozon' AND mapping.id_type='offer_id'))
					ORDER BY (mapping.source='manual') DESC,(mapping.status='active') DESC,mapping.updated_at DESC LIMIT 1
				) external ON TRUE
				WHERE (($1='site' AND directory.saby_id=NULLIF($5,'')) OR
					($1='saby' AND (directory.saby_id=NULLIF($5,'') OR external.id IS NOT NULL)) OR
					($1 IN ('wb','ozon') AND external.id IS NOT NULL))
				ORDER BY (external.id IS NOT NULL) DESC,directory.variant_id LIMIT 1
			)
			INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,
				cross_source_key,event_type,event_status,event_at,external_product_id,saby_id,
				canonical_variant_id,external_mapping_id,units,gross_rub,effect,reconciliation_status,
				duplicate_of,import_batch_id,imported_at,raw_document)
			SELECT $1,$2,$3,$6,$7,$8,$9,$10,$4,resolved.saby_id,resolved.variant_id,
				resolved.external_mapping_id,$11,$12,$13,
				CASE WHEN $9<>'confirmed' OR $8='cancellation' THEN 'excluded'
					WHEN resolved.variant_id IS NULL AND NOT ($1='site' AND $4 IN ('__adjustment__','__delivery__')) THEN 'unmatched'
					ELSE 'counted' END,
				NULL,$14::UUID,CURRENT_TIMESTAMP,$15::JSONB
			FROM (SELECT 1) seed LEFT JOIN resolved ON TRUE
			WHERE $1<>'saby' OR resolved.product_id IS NULL OR EXISTS(
				SELECT 1 FROM products WHERE id=resolved.product_id AND catalog_section='plants')
			ON CONFLICT(channel,source_event_id,source_line_id) DO UPDATE SET
				source_document_id=EXCLUDED.source_document_id,cross_source_key=EXCLUDED.cross_source_key,
				event_type=EXCLUDED.event_type,event_status=EXCLUDED.event_status,event_at=EXCLUDED.event_at,
				external_product_id=EXCLUDED.external_product_id,saby_id=EXCLUDED.saby_id,
				canonical_variant_id=EXCLUDED.canonical_variant_id,external_mapping_id=EXCLUDED.external_mapping_id,
				units=EXCLUDED.units,gross_rub=EXCLUDED.gross_rub,effect=EXCLUDED.effect,
				reconciliation_status=EXCLUDED.reconciliation_status,duplicate_of=NULL,
				import_batch_id=EXCLUDED.import_batch_id,imported_at=CURRENT_TIMESTAMP,raw_document=EXCLUDED.raw_document
		`, channel, event.SourceEventID, event.SourceDocumentID, event.ExternalID, event.SabyID,
			event.SourceLineID, event.CrossSourceKey, event.EventType, event.EventStatus, event.Date,
			event.Units, event.GrossRUB, event.Effect, batchID, raw)
		if err != nil {
			return 0, fmt.Errorf("insert sales event: %w", err)
		}
		inserted += int(command.RowsAffected())
	}
	// A fetched window is a source snapshot. A row that disappeared is retained
	// for audit, but cannot keep contributing as a new negative sale.
	if _, err := tx.Exec(ctx, `UPDATE sales_events SET event_status='cancelled',
		reconciliation_status='excluded',duplicate_of=NULL,imported_at=CURRENT_TIMESTAMP
		WHERE channel=$1 AND (event_at AT TIME ZONE 'Europe/Moscow')::DATE BETWEEN $2::DATE AND $3::DATE
			AND import_batch_id<>$4::UUID`, channel, from, to, batchID); err != nil {
		return 0, err
	}
	if err := reconcileSalesEvents(ctx, tx); err != nil {
		return 0, err
	}
	if err := rebuildSalesDaily(ctx, tx, from, to); err != nil {
		return 0, err
	}
	return inserted, nil
}

func reconcileSalesEvents(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `UPDATE sales_events event SET reconciliation_status=CASE
		WHEN event_status<>'confirmed' OR event_type='cancellation' THEN 'excluded'
		WHEN EXISTS(SELECT 1 FROM procurement_ignored_sales_products ignored
			WHERE ignored.channel=event.channel AND ignored.external_product_id=event.external_product_id) THEN 'excluded'
		WHEN canonical_variant_id IS NULL AND NOT (channel='site' AND external_product_id IN ('__adjustment__','__delivery__'))
			THEN 'unmatched' ELSE 'counted' END,duplicate_of=NULL`); err != nil {
		return err
	}
	// A shared source key is the only basis for cross-channel collapse. Saby is
	// the retail source; when it mirrors a marketplace document, attribution
	// stays with that marketplace. Amount/time similarity is never used.
	if _, err := tx.Exec(ctx, `WITH groups AS (
		SELECT cross_source_key,canonical_variant_id,
			COUNT(DISTINCT channel) FILTER(WHERE channel IN ('wb','ozon')) marketplaces,
			MIN(id) FILTER(WHERE channel IN ('wb','ozon')) marketplace_id
		FROM sales_events WHERE cross_source_key<>'' AND event_status='confirmed'
			AND event_type<>'cancellation' AND canonical_variant_id IS NOT NULL
		GROUP BY cross_source_key,canonical_variant_id
	), ambiguous AS (
		UPDATE sales_events event SET reconciliation_status='ambiguous'
		FROM groups WHERE event.cross_source_key=groups.cross_source_key
			AND event.canonical_variant_id=groups.canonical_variant_id AND groups.marketplaces>1
		RETURNING event.id
	)
	UPDATE sales_events event SET reconciliation_status='duplicate',duplicate_of=groups.marketplace_id
	FROM groups WHERE event.cross_source_key=groups.cross_source_key
		AND event.canonical_variant_id=groups.canonical_variant_id AND groups.marketplaces=1
		AND event.channel='saby' AND event.reconciliation_status='counted'`); err != nil {
		return err
	}
	return nil
}

func rebuildSalesDaily(ctx context.Context, tx pgx.Tx, from, to time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM procurement_sales_daily WHERE sale_date BETWEEN $1::DATE AND $2::DATE`, from, to); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO procurement_sales_daily(channel,sale_date,external_product_id,
		saby_id,canonical_variant_id,external_mapping_id,units,gross_rub,synced_at)
	SELECT channel,(event_at AT TIME ZONE 'Europe/Moscow')::DATE,external_product_id,
		MAX(saby_id),MAX(canonical_variant_id),MAX(external_mapping_id),
		SUM(units*effect)::INTEGER,SUM(gross_rub*effect),CURRENT_TIMESTAMP
	FROM sales_events WHERE reconciliation_status='counted' AND event_status='confirmed'
		AND canonical_variant_id IS NOT NULL
		AND (event_at AT TIME ZONE 'Europe/Moscow')::DATE BETWEEN $1::DATE AND $2::DATE
	GROUP BY channel,(event_at AT TIME ZONE 'Europe/Moscow')::DATE,external_product_id
	HAVING SUM(units*effect)<>0 OR SUM(gross_rub*effect)<>0`, from, to)
	return err
}
