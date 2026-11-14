package ordersale

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func bindRevisionIdentity(origin, content Request) Request {
	result := origin
	result.Event = content.Event
	result.RevisionID = content.RevisionID
	result.Event.IntentID = origin.IntentID
	result.Event.OrderID = origin.Event.OrderID
	result.Event.Key = origin.Event.Key
	result.Event.OrderNumber = origin.Event.OrderNumber
	return result
}
func readRevisionRequest(ctx context.Context, q queryer, id int64) (Request, error) {
	var r Request
	var raw []byte
	var hash string
	if err := q.QueryRow(ctx, `SELECT request,request_hash FROM order_sale_revisions WHERE id=$1`, id).Scan(&raw, &hash); err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &r) != nil {
		return r, ErrConflict
	}
	actual, err := requestHash(r)
	if err != nil || actual != hash {
		return r, ErrConflict
	}
	return r, nil
}

// A changed target never replaces the payload of an unresolved old write.
func revisionForClaim(ctx context.Context, tx pgx.Tx, origin Request, p *Progress, status string) (*RevisionCommand, error) {
	var base, desired, active, applied int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(base_revision_id,0),COALESCE(desired_revision_id,0),COALESCE(active_revision_id,0),COALESCE(applied_revision_id,0) FROM order_sale_intents WHERE id=$1`, origin.IntentID).Scan(&base, &desired, &active, &applied); err != nil {
		return nil, err
	}
	if base == 0 {
		return nil, nil
	}
	if active == 0 {
		if p.DesiredState == "cancelled" {
			if applied == 0 || applied == base || len(p.RevisionNative) == 0 {
				return nil, nil
			}
			active = applied
		} else {
			if desired == 0 || desired == base || desired == applied {
				return nil, nil
			}
			if p.DocumentID == "" || p.Unknown || status == "processing" && writeStage(p.Stage) || strings.HasPrefix(p.Stage, "reconcile-") {
				return nil, nil
			}
			if p.Stage == "verify-fill" {
				var barrier struct {
					Phase string `json:"phase"`
				}
				if len(p.Native) > 0 && json.Unmarshal(p.Native, &barrier) != nil {
					return nil, ErrConflict
				}
				if barrier.Phase == "saved" || barrier.Phase == "bound" {
					return nil, nil
				}
			}

			var originPosted bool
			if err := tx.QueryRow(ctx, `SELECT stock_verified FROM order_sale_publications WHERE intent_id=$1`, origin.IntentID).Scan(&originPosted); err != nil {
				return nil, err
			}
			if p.PostAttempted && !originPosted {
				return nil, nil
			}
			candidate, e := readRevisionRequest(ctx, tx, desired)
			if e != nil {
				return nil, e
			}
			if !candidate.Event.CanPrepare {
				return nil, ErrNotReady
			}
			active = desired
			before := applied
			if before == 0 {
				before = base
			}
			if _, err := tx.Exec(ctx, `UPDATE order_sale_revisions SET before_revision_id=$2,state='active' WHERE id=$1 AND before_revision_id IS NULL`, active, before); err != nil {
				return nil, err
			}
			p.RevisionNative = nil
			p.ActiveRevisionID = active
			p.TargetRevisionID = active
			p.RevisionPostAttempted = false
			p.RevisionUnpostAttempted = false
			p.RevisionUnpostVerified = false
			p.ActiveOwnedRowIDs = []int64{}
			retired := map[int64]bool{}
			for _, id := range p.RetiredRowIDs {
				retired[id] = true
			}
			for _, id := range p.OwnedRowIDs {
				if !retired[id] {
					p.ActiveOwnedRowIDs = append(p.ActiveOwnedRowIDs, id)
				}
			}
			p.Stage = "revision-inspect"
			p.Unknown = false
		}
		if _, err := tx.Exec(ctx, `UPDATE order_sale_intents SET active_revision_id=$2 WHERE id=$1`, origin.IntentID, active); err != nil {
			return nil, err
		}
	}
	var before int64
	var postAttempted, unpostAttempted, unpostVerified bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(before_revision_id,$2),post_attempted,unpost_attempted,unpost_verified FROM order_sale_revisions WHERE id=$1 AND intent_id=$3`, active, base, origin.IntentID).Scan(&before, &postAttempted, &unpostAttempted, &unpostVerified); err != nil {
		return nil, err
	}
	current, err := readRevisionRequest(ctx, tx, before)
	if err != nil {
		return nil, err
	}
	if before == base && current.RevisionID == 0 {
		current = origin
	} else {
		current = bindRevisionIdentity(origin, current)
	}
	target, err := readRevisionRequest(ctx, tx, active)
	if err != nil {
		return nil, err
	}
	target = bindRevisionIdentity(origin, target)
	if !target.Event.CanPrepare {
		return nil, ErrNotReady
	}
	p.ActiveRevisionID = active
	p.TargetRevisionID = active
	p.RevisionPostAttempted = postAttempted
	p.RevisionUnpostAttempted = unpostAttempted
	p.RevisionUnpostVerified = unpostVerified
	return &RevisionCommand{Origin: origin, Current: current, Target: target, RevisionID: active, CurrentRevisionID: before}, nil
}
func markRevisionAttempts(ctx context.Context, tx pgx.Tx, command *RevisionCommand, p *Progress) error {
	if command == nil {
		return nil
	}
	if p.Stage == "revision-post" && p.DesiredState != "cancelled" {
		p.RevisionPostAttempted = true
		p.PostAttempted = true
	}
	if p.Stage == "revision-unpost" {
		p.RevisionUnpostAttempted = true
	}
	_, err := tx.Exec(ctx, `UPDATE order_sale_revisions SET post_attempted=post_attempted OR $2,unpost_attempted=unpost_attempted OR $3 WHERE id=$1`, command.RevisionID, p.RevisionPostAttempted, p.RevisionUnpostAttempted)
	return err
}
func validateRevisionResult(command RevisionCommand, before Progress, result *Result) {
	result.Progress.ActiveRevisionID = before.ActiveRevisionID
	result.Progress.TargetRevisionID = before.TargetRevisionID
	result.Progress.RevisionPostAttempted = before.RevisionPostAttempted
	result.Progress.RevisionUnpostAttempted = before.RevisionUnpostAttempted
	if before.RevisionUnpostVerified {
		result.Progress.RevisionUnpostVerified = true
	}
	fail := func(message string) { *result = Result{Progress: before, State: "needs_attention", Message: message} }
	if !retainsOwnedRowIDs(before.RetiredRowIDs, result.Progress.RetiredRowIDs) {
		fail("Утрачена история удалённых строк; нужна сверка")
		return
	}
	if result.RevisionUnpostVerified {
		if !validRevisionUnpostProof(command, *result) {
			fail("Снятие прежнего проведения не подтверждено")
			return
		}
		result.Progress.RevisionUnpostVerified = true
	}
	if len(result.RevisionRetirementProof) > 0 {
		if !validRevisionRetirementProof(command, *result) {
			fail("Удаление собственных строк не подтверждено")
			return
		}
	}
	removed := false
	after := map[int64]bool{}
	for _, id := range result.Progress.ActiveOwnedRowIDs {
		after[id] = true
	}
	for _, id := range before.ActiveOwnedRowIDs {
		if !after[id] {
			removed = true
		}
	}
	if removed && len(result.RevisionRetirementProof) == 0 {
		fail("Активные строки утрачены без подтверждения удаления")
		return
	}
	if result.State == "posted" || result.RevisionVerified {
		if !validRevisionProof(command, *result) {
			fail("Проведение новой редакции не подтверждено")
			return
		}
		result.RevisionVerified = true
		result.StockVerified = true
		result.StockProof = result.RevisionProof
	}
	if result.State == "cancelled" {
		if !validRevisionCancellationProof(command, *result) {
			fail("Отмена текущей редакции не подтверждена")
			return
		}
	}
}
func persistRevisionResult(ctx context.Context, tx pgx.Tx, command *RevisionCommand, result *Result) error {
	if command == nil {
		return nil
	}
	id := command.RevisionID
	intent := command.Origin.IntentID
	if result.RevisionUnpostVerified {
		if _, err := tx.Exec(ctx, `UPDATE order_sale_revisions SET unpost_verified=TRUE,unpost_proof=CASE WHEN unpost_verified THEN unpost_proof ELSE $2 END,unpost_verified_at=COALESCE(unpost_verified_at,NOW()) WHERE id=$1`, id, result.RevisionUnpostProof); err != nil {
			return err
		}
		var currentWasPosted bool
		if err := tx.QueryRow(ctx, `SELECT post_verified FROM order_sale_revisions WHERE id=$1`, command.CurrentRevisionID).Scan(&currentWasPosted); err != nil {
			return err
		}
		lines := []Line{}
		if currentWasPosted {
			lines = command.Current.Event.Lines
		}
		if err := recordRevisionEffect(ctx, tx, intent, id, "revision_unpost", false, result.RevisionUnpostProof, time.Now(), lines); err != nil {
			return err
		}
	}
	if result.RevisionVerified {
		if _, err := tx.Exec(ctx, `UPDATE order_sale_revisions SET state='applied',post_verified=TRUE,stock_proof=$2,stock_verified_at=COALESCE(stock_verified_at,NOW()),applied_at=COALESCE(applied_at,NOW()) WHERE id=$1`, id, result.RevisionProof); err != nil {
			return err
		}
		if err := recordRevisionEffect(ctx, tx, intent, id, "revision_post", true, result.RevisionProof, time.Now(), command.Target.Event.Lines); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE order_sale_intents SET applied_revision_id=$2,active_revision_id=NULL WHERE id=$1`, intent, id); err != nil {
			return err
		}
	}
	if result.CancellationVerified {
		if _, err := tx.Exec(ctx, `UPDATE order_sale_intents SET active_revision_id=NULL WHERE id=$1`, intent); err != nil {
			return err
		}
		if err := recordRevisionEffect(ctx, tx, intent, id, "cancel_unpost", false, result.CancellationProof, time.Now(), nil); err != nil {
			return err
		}
	}
	if result.State == "needs_attention" || result.State == "failed" {
		if _, err := tx.Exec(ctx, `UPDATE order_sale_revisions SET state='needs_attention' WHERE id=$1 AND state<>'applied'`, id); err != nil {
			return err
		}
	}
	return nil
}
func persistOriginRevisionProof(ctx context.Context, tx pgx.Tx, intent int64, result *Result) error {
	if !result.StockVerified || result.RevisionVerified {
		return nil
	}
	var base int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(base_revision_id,0) FROM order_sale_intents WHERE id=$1`, intent).Scan(&base)
	if err != nil || base == 0 {
		return err
	}
	origin, err := readRevisionRequest(ctx, tx, base)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_revisions SET state='applied',post_verified=TRUE,stock_proof=$2,stock_verified_at=COALESCE(stock_verified_at,NOW()),applied_at=COALESCE(applied_at,NOW()) WHERE id=$1`, base, result.StockProof); err != nil {
		return err
	}
	if err = recordRevisionEffect(ctx, tx, intent, base, "origin_post", true, result.StockProof, time.Now(), origin.Event.Lines); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE order_sale_intents SET applied_revision_id=COALESCE(applied_revision_id,$2::BIGINT) WHERE id=$1`, intent, base)
	return err
}

func revisionPlanMappingsReady(ctx context.Context, q queryer, request Request) (bool, error) {
	seen := map[int64]string{}
	for _, line := range request.Event.Lines {
		if prior, ok := seen[line.VariantID]; ok {
			if prior != line.SabyID {
				return false, nil
			}
			continue
		}
		var current string
		if err := q.QueryRow(ctx, `SELECT COALESCE((SELECT CASE WHEN COUNT(DISTINCT external_id)=1 THEN MIN(external_id) ELSE '' END FROM product_external_ids WHERE variant_id=$1 AND provider='saby' AND id_type='id' AND status='active' HAVING COUNT(*)>0),(SELECT saby_id FROM product_variants WHERE id=$1),'')`, line.VariantID).Scan(&current); err != nil {
			return false, err
		}
		if current == "" || current != line.SabyID {
			return false, nil
		}
		seen[line.VariantID] = current
	}
	return len(seen) > 0, nil
}
