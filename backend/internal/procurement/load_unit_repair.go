package procurement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// validInvoiceLoadUnit accepts the Box no values produced by the
// Holland packing-list parser. Planning placeholders such as "shelf"
// must never become physical trolleys in logistics allocation.
func validInvoiceLoadUnit(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, symbol := range value {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}
	return true
}

// repairStoredInvoiceLoadUnits heals invoices imported before the
// reconciliation code started copying Box no onto existing plan rows.
// The original extracted text is stored with the document, so the
// repair is deterministic and does not need to call Saby or read the
// PDF again. Explicit numeric/manual trolley assignments are preserved.
func repairStoredInvoiceLoadUnits(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var documentID int64
	var parserKind, extractedText string
	err := tx.QueryRow(ctx, `
		SELECT id, parser_kind, extracted_text
		FROM procurement_documents
		WHERE procurement_order_id=$1 AND superseded_at IS NULL
		ORDER BY created_at DESC,id DESC LIMIT 1
	`, orderID).Scan(&documentID, &parserKind, &extractedText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load invoice text for trolley repair: %w", err)
	}
	if parserKind != "holland_packing_list" || strings.TrimSpace(extractedText) == "" {
		return nil
	}
	parsed, err := ParseDocumentText(extractedText)
	if err != nil {
		return fmt.Errorf("reparse invoice trolley data: %w", err)
	}
	for _, line := range parsed.Lines {
		if !validInvoiceLoadUnit(line.LoadUnit) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE procurement_order_lines SET load_unit=$4,updated_at=CURRENT_TIMESTAMP
			WHERE procurement_order_id=$1 AND procurement_document_id=$2 AND source_line=$3
				AND reconciliation_status<>'superseded'
				AND (BTRIM(load_unit)='' OR LOWER(BTRIM(load_unit))='shelf')
		`, orderID, documentID, line.SourceLine, strings.TrimSpace(line.LoadUnit)); err != nil {
			return fmt.Errorf("repair invoice trolley for source line %d: %w", line.SourceLine, err)
		}
	}
	return nil
}
