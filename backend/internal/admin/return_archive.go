package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type ReturnArchive struct {
	Items     []MarketplaceReturn `json:"items"`
	Documents []ReturnIntake      `json:"documents"`
}

func (r *PostgresRepository) ArchivedReturns(ctx context.Context, actor Actor) (ReturnArchive, error) {
	result := ReturnArchive{Items: []MarketplaceReturn{}, Documents: []ReturnIntake{}}
	if !Can(actor.Role, PermissionReturnsDelete) {
		return result, ErrForbidden
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM marketplace_returns WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC,id DESC`)
	if err != nil {
		return result, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		item, err := r.marketplaceReturn(ctx, id, true)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	result.Documents, err = r.returnDocuments(ctx, true)
	return result, err
}

// Restore journal evidence, keeping existing Saby identities. Only an action
// cancelled before its first attempt can be detached; it was never sent.
func (r *PostgresRepository) RestoreReturns(ctx context.Context, actor Actor, ids []int64, documentID string, all bool) error {
	if !Can(actor.Role, PermissionReturnsDelete) {
		return ErrForbidden
	}
	if !all && documentID == "" {
		var err error
		ids, err = normalizedReturnIDs(ids)
		if err != nil {
			return err
		}
	}
	if documentID != "" && !validIntakeID(documentID) {
		return errors.New("некорректный документ")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE marketplace_returns r SET deleted_at=NULL,deleted_by=NULL,updated_by=$4,updated_at=CURRENT_TIMESTAMP,
 receipt_status=CASE WHEN EXISTS(SELECT 1 FROM procurement_action_items a WHERE a.id=r.receipt_action_id AND a.status='skipped' AND a.attempts=0 AND a.external_operation_id='' AND a.error_message='Записи возврата удалены') THEN 'none' ELSE receipt_status END,
 receipt_action_id=CASE WHEN EXISTS(SELECT 1 FROM procurement_action_items a WHERE a.id=r.receipt_action_id AND a.status='skipped' AND a.attempts=0 AND a.external_operation_id='' AND a.error_message='Записи возврата удалены') THEN NULL ELSE receipt_action_id END
 WHERE deleted_at IS NOT NULL AND ($1 OR id=ANY($2::BIGINT[]) OR intake_id::TEXT=$3)`, all, ids, documentID, actor.CustomerID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE return_intakes i SET deleted_at=NULL,deleted_by=NULL,updated_at=CURRENT_TIMESTAMP,
 receipt_action_id=CASE WHEN EXISTS(SELECT 1 FROM procurement_action_items a WHERE a.id=i.receipt_action_id AND a.status='skipped' AND a.attempts=0 AND a.external_operation_id='' AND a.error_message='Записи возврата удалены') THEN NULL ELSE receipt_action_id END
 WHERE deleted_at IS NOT NULL AND ($1 OR id::TEXT=$2 OR EXISTS(SELECT 1 FROM marketplace_returns r WHERE r.intake_id=i.id AND r.deleted_at IS NULL))`, all, documentID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) SetReturnPhotoArchived(ctx context.Context, actor Actor, id, photoID int64, archived bool) (MarketplaceReturn, error) {
	if !Can(actor.Role, PermissionReturnsEdit) {
		return MarketplaceReturn{}, ErrForbidden
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return MarketplaceReturn{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if err = tx.QueryRow(ctx, `SELECT id FROM marketplace_returns WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&locked); err != nil {
		return MarketplaceReturn{}, err
	}
	if !archived {
		var count int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM marketplace_return_photos WHERE marketplace_return_id=$1 AND deleted_at IS NULL`, id).Scan(&count); err != nil {
			return MarketplaceReturn{}, err
		}
		if count >= 6 {
			return MarketplaceReturn{}, errors.New("сначала удалите одно из шести фото")
		}
	}
	command, err := tx.Exec(ctx, `UPDATE marketplace_return_photos SET deleted_at=CASE WHEN $3 THEN CURRENT_TIMESTAMP ELSE NULL END WHERE id=$1 AND marketplace_return_id=$2 AND (deleted_at IS NOT NULL)<>$3`, photoID, id, archived)
	if err != nil {
		return MarketplaceReturn{}, err
	}
	if command.RowsAffected() == 0 {
		return MarketplaceReturn{}, pgx.ErrNoRows
	}
	if err = tx.Commit(ctx); err != nil {
		return MarketplaceReturn{}, err
	}
	return r.MarketplaceReturn(ctx, id)
}
