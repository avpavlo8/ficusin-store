package ordersale

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegulationAmendmentProof contains only independent document/line readback,
// never personal or banking fields. Missing schema/flag labels fail; observed
// nullable virgin-draft flags are recorded explicitly before any post attempt.
type RegulationAmendmentProof struct {
	Version              int       `json:"version"`
	VerifiedAt           time.Time `json:"verifiedAt"`
	BeforeHash           string    `json:"beforeHash"`
	EffectiveHash        string    `json:"effectiveHash"`
	ProgressHash         string    `json:"progressHash"`
	DocumentID           string    `json:"documentId"`
	DocumentUUID         string    `json:"documentUUID"`
	PublicUUID           string    `json:"publicUUID"`
	PublicType           string    `json:"publicType"`
	PublicRegulationUUID string    `json:"publicRegulationUUID"`
	PublicRegulationName string    `json:"publicRegulationName"`
	OrganizationID       int64     `json:"organizationId"`
	WarehouseID          int64     `json:"warehouseId"`
	MovementWarehouseID  int64     `json:"movementWarehouseId"`
	RegulationID         int64     `json:"regulationId"`
	BuyerID              int64     `json:"buyerId"`
	HeaderRead           bool      `json:"headerRead"`
	PostingStateKnown    bool      `json:"postingStateKnown"`
	NullableDraftFlags   bool      `json:"nullableDraftFlags"`
	LinesRead            bool      `json:"linesRead"`
	EmptyLines           bool      `json:"emptyLines"`
	Posted               bool      `json:"posted"`
	HasEntries           bool      `json:"hasEntries"`
	PostingErrors        bool      `json:"postingErrors"`
	StaleEntries         bool      `json:"staleEntries"`
	PostingQueued        bool      `json:"postingQueued"`
	EntriesQueued        bool      `json:"entriesQueued"`
}

type RegulationAmendmentVerification struct {
	Progress Progress
	Proof    RegulationAmendmentProof
}

// Implementations must only read Saby; the returned progress may change only
// the receipt's request hash. No document or line can be created or updated.
type RegulationAmendmentVerifier interface {
	VerifyRegulationAmendment(context.Context, Request, Request, Progress) (RegulationAmendmentVerification, error)
}

type RegulationAmendmentPlan struct {
	PublicationID  int64
	IntentID       int64
	Before         Request
	Effective      Request
	BeforeHash     string
	EffectiveHash  string
	BeforeProgress Progress
	Verification   RegulationAmendmentVerification
}

type RegulationAmendmentApproval struct {
	ID              int64  `json:"id"`
	PublicationID   int64  `json:"publicationId"`
	IntentID        int64  `json:"intentId"`
	BeforeHash      string `json:"beforeHash"`
	EffectiveHash   string `json:"effectiveHash"`
	DocumentID      string `json:"documentId"`
	DocumentUUID    string `json:"documentUUID"`
	AlreadyApproved bool   `json:"alreadyApproved"`
}

type amendmentSource struct {
	publicationID, intentID                                int64
	request                                                Request
	hash                                                   string
	progress                                               Progress
	rawProgress                                            []byte
	status, desired, mode, documentID, documentUUID, lease string
	leaseUntil                                             *time.Time
	created, posted, stock, cancelled, blocked             bool
	base, active, applied, desiredRevision                 int64
	control                                                DispatchControl
}

func readAmendmentSource(ctx context.Context, q queryer, id int64) (amendmentSource, error) {
	var s amendmentSource
	var request, progress []byte
	err := q.QueryRow(ctx, `SELECT p.id,p.intent_id,p.request,p.request_hash,p.progress,p.status,p.desired_state,p.execution_mode,
 p.document_id,p.document_uuid,p.lease_token,p.lease_until,p.create_attempted,p.post_attempted,p.stock_verified,p.cancellation_verified,p.cancellation_blocked,
 COALESCE(i.base_revision_id,0),COALESCE(i.active_revision_id,0),COALESCE(i.applied_revision_id,0),COALESCE(i.desired_revision_id,0)
 FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id WHERE p.id=$1`, id).Scan(
		&s.publicationID, &s.intentID, &request, &s.hash, &progress, &s.status, &s.desired, &s.mode, &s.documentID, &s.documentUUID, &s.lease, &s.leaseUntil,
		&s.created, &s.posted, &s.stock, &s.cancelled, &s.blocked, &s.base, &s.active, &s.applied, &s.desiredRevision)
	if err != nil {
		return s, err
	}
	if json.Unmarshal(request, &s.request) != nil || json.Unmarshal(progress, &s.progress) != nil {
		return s, ErrConflict
	}
	s.rawProgress = progress
	s.control, err = readDispatchControl(ctx, q, false)
	return s, err
}

func requireAmendmentOwner(ctx context.Context, q queryer, actor int64) error {
	var owner bool
	if actor <= 0 {
		return ErrInput
	}
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE customer_id=$1 AND role='owner' AND is_active)`, actor).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return ErrInput
	}
	return nil
}

func amendmentProgressSafe(s amendmentSource) bool {
	p := s.progress
	return s.mode == "live" && s.request.ExecutionMode == "live" && s.status == "needs_attention" && s.desired == "posted" && s.created && !s.posted && !s.stock && !s.cancelled && !s.blocked &&
		s.documentID == "" && s.documentUUID == "" && s.lease == "" && s.leaseUntil == nil && s.base > 0 && s.active == 0 && s.applied == 0 && s.desiredRevision == s.base &&
		p.Stage == "discover" && !p.Unknown && !p.IdentityConflict && !p.PostAttempted && p.DocumentID == "" && p.DocumentUUID == "" && len(p.Native) > 0 &&
		len(p.OwnedRowIDs) == 0 && len(p.ActiveOwnedRowIDs) == 0 && len(p.RetiredRowIDs) == 0 && len(p.RevisionNative) == 0 && p.ActiveRevisionID == 0 && p.TargetRevisionID == 0 &&
		!p.RevisionPostAttempted && !p.RevisionUnpostAttempted && !p.RevisionUnpostVerified && len(p.ConflictingStockProof) == 0
}

func onlyRegulationDiff(before, effective Request) bool {
	if before.RegulationID <= 0 || effective.RegulationID <= 0 || before.RegulationID == effective.RegulationID {
		return false
	}
	copy := effective
	copy.RegulationID = before.RegulationID
	return reflect.DeepEqual(before, copy)
}

func hashProgress(p Progress) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validAmendmentProgress(before, after Progress, oldHash, newHash string) bool {
	// The adapter's opaque receipt is compared structurally, preserving all large
	// numbers as raw JSON. Only its top-level requestHash may change.
	var oldNative, newNative map[string]json.RawMessage
	if json.Unmarshal(before.Native, &oldNative) != nil || json.Unmarshal(after.Native, &newNative) != nil {
		return false
	}
	var oldValue, newValue string
	if json.Unmarshal(oldNative["requestHash"], &oldValue) != nil || json.Unmarshal(newNative["requestHash"], &newValue) != nil || oldValue != oldHash || newValue != newHash {
		return false
	}
	delete(oldNative, "requestHash")
	delete(newNative, "requestHash")
	a, _ := json.Marshal(oldNative)
	b, _ := json.Marshal(newNative)
	if string(a) != string(b) {
		return false
	}
	before.Native = nil
	after.Native = nil
	return reflect.DeepEqual(before, after)
}

func validAmendmentProof(plan RegulationAmendmentPlan, now time.Time) bool {
	p := plan.Verification.Proof
	r := plan.Effective
	ph, err := hashProgress(plan.BeforeProgress)
	if err != nil || p.Version != 1 || p.VerifiedAt.IsZero() || p.VerifiedAt.After(now.Add(5*time.Second)) || now.Sub(p.VerifiedAt) > 30*time.Second {
		return false
	}
	return onlyRegulationDiff(plan.Before, r) && p.BeforeHash == plan.BeforeHash && p.EffectiveHash == plan.EffectiveHash && p.ProgressHash == ph &&
		canonicalStockID(p.DocumentID) && stockProofUUID.MatchString(p.DocumentUUID) && strings.EqualFold(p.PublicUUID, r.OperationKey) &&
		p.PublicType == "ДокОтгрИсх" && p.PublicRegulationName == "Реализация" && stockProofUUID.MatchString(p.PublicRegulationUUID) &&
		p.OrganizationID == r.OrganizationID && p.WarehouseID == r.NativeWarehouseID && p.MovementWarehouseID == r.MovementWarehouseID && p.RegulationID == r.RegulationID && p.BuyerID == 0 &&
		p.HeaderRead && p.PostingStateKnown != p.NullableDraftFlags && p.LinesRead && p.EmptyLines && !p.Posted && !p.HasEntries && !p.PostingErrors && !p.StaleEntries && !p.PostingQueued && !p.EntriesQueued &&
		validAmendmentProgress(plan.BeforeProgress, plan.Verification.Progress, plan.BeforeHash, plan.EffectiveHash)
}

// InspectRegulationAmendment does not write either database or Saby. Approval
// repeats this inspection; callers cannot submit an old or substituted proof.
func InspectRegulationAmendment(ctx context.Context, pool *pgxpool.Pool, actor, id int64, verifier RegulationAmendmentVerifier) (RegulationAmendmentPlan, error) {
	var plan RegulationAmendmentPlan
	if verifier == nil {
		return plan, ErrInput
	}
	if err := requireAmendmentOwner(ctx, pool, actor); err != nil {
		return plan, err
	}
	s, err := readAmendmentSource(ctx, pool, id)
	if err != nil {
		return plan, err
	}
	if !amendmentProgressSafe(s) {
		return plan, ErrRetryUnsafe
	}
	if s.request.IntentID != s.intentID || s.request.Event.OrderID <= 0 || s.request.Event.Trigger != "created" ||
		s.request.Event.Key != eventKey(s.request.Event.OrderID, s.request.Event.ShipmentOfferID) || !s.request.Event.CanPrepare || !stockProofUUID.MatchString(s.request.OperationKey) {
		return plan, ErrConflict
	}
	if cashier, err := cashierOwnsSale(ctx, pool, s.request.Event.OrderID); err != nil {
		return plan, err
	} else if cashier {
		return plan, ErrRetryUnsafe
	}
	actual, err := requestHash(s.request)
	if err != nil || actual != s.hash {
		return plan, ErrConflict
	}
	c := s.control
	r := s.request
	if !c.Enabled || c.Mode != "live" || c.RegulationID <= 0 || c.SiteWarehouseID != r.SiteWarehouseID || c.NativeWarehouseID != r.NativeWarehouseID || c.MovementWarehouseID != r.MovementWarehouseID || c.OrganizationID != r.OrganizationID || c.BuyerID != r.BuyerID {
		return plan, ErrConflict
	}
	effective := r
	effective.RegulationID = c.RegulationID
	if !onlyRegulationDiff(r, effective) {
		return plan, ErrConflict
	}
	hash, err := requestHash(effective)
	if err != nil {
		return plan, err
	}
	plan = RegulationAmendmentPlan{PublicationID: id, IntentID: s.intentID, Before: r, Effective: effective, BeforeHash: s.hash, EffectiveHash: hash, BeforeProgress: s.progress}
	plan.Verification, err = verifier.VerifyRegulationAmendment(ctx, r, effective, s.progress)
	if err != nil {
		return plan, err
	}
	if !validAmendmentProof(plan, time.Now()) {
		return plan, ErrConflict
	}
	return plan, nil
}

func readAmendmentApproval(ctx context.Context, q queryer, id int64) (RegulationAmendmentApproval, error) {
	var a RegulationAmendmentApproval
	err := q.QueryRow(ctx, `SELECT id,publication_id,intent_id,before_hash,effective_hash,document_id::TEXT,document_uuid::TEXT FROM order_sale_regulation_amendments WHERE publication_id=$1`, id).Scan(&a.ID, &a.PublicationID, &a.IntentID, &a.BeforeHash, &a.EffectiveHash, &a.DocumentID, &a.DocumentUUID)
	return a, err
}

// ApproveRegulationAmendment is an owner-only internal recovery service. It
// writes one append-only approval and rehashes only local provider metadata.
// No native create/fill/post/cancel method is reachable from this function.
func ApproveRegulationAmendment(ctx context.Context, pool *pgxpool.Pool, actor, id int64, verifier RegulationAmendmentVerifier) (RegulationAmendmentApproval, error) {
	if err := requireAmendmentOwner(ctx, pool, actor); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if existing, err := readAmendmentApproval(ctx, pool, id); err == nil {
		existing.AlreadyApproved = true
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return existing, err
	}
	plan, err := InspectRegulationAmendment(ctx, pool, actor, id, verifier)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	defer tx.Rollback(ctx)
	c, err := readDispatchControl(ctx, tx, true)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if err = requireAmendmentOwner(ctx, tx, actor); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, plan.Before.Event.OrderID); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('order-sale:'||$1::BIGINT::TEXT,0))`, plan.Before.Event.OrderID); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM order_sale_intents WHERE id=$1 FOR UPDATE`, plan.IntentID); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM order_sale_publications WHERE id=$1 FOR UPDATE`, id); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	s, err := readAmendmentSource(ctx, tx, id)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	progressHash, err := hashProgress(s.progress)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	sameControl := c.Enabled && c.Mode == "live" && c.RegulationID == plan.Effective.RegulationID && c.SiteWarehouseID == plan.Effective.SiteWarehouseID && c.NativeWarehouseID == plan.Effective.NativeWarehouseID && c.MovementWarehouseID == plan.Effective.MovementWarehouseID && c.OrganizationID == plan.Effective.OrganizationID && c.BuyerID == plan.Effective.BuyerID
	if !amendmentProgressSafe(s) || !sameControl || s.hash != plan.BeforeHash || !reflect.DeepEqual(s.request, plan.Before) || progressHash != plan.Verification.Proof.ProgressHash || !validAmendmentProof(plan, time.Now()) {
		return RegulationAmendmentApproval{}, ErrConflict
	}
	if cashier, err := cashierOwnsSale(ctx, tx, plan.Before.Event.OrderID); err != nil {
		return RegulationAmendmentApproval{}, err
	} else if cashier {
		return RegulationAmendmentApproval{}, ErrRetryUnsafe
	}
	event, err := readEvent(ctx, tx, plan.Before.Event.OrderID, plan.Before.Event.ShipmentOfferID)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if !event.CanPrepare || event.Fingerprint != plan.Before.Event.Fingerprint || event.CancellationRequested {
		return RegulationAmendmentApproval{}, ErrConflict
	}
	var collision bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_sale_publications WHERE id<>$1 AND (document_id=$2 OR lower(document_uuid)=lower($3)))`, id, plan.Verification.Proof.DocumentID, plan.Verification.Proof.DocumentUUID).Scan(&collision); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if collision {
		return RegulationAmendmentApproval{}, ErrConflict
	}
	beforeRaw, _ := json.Marshal(plan.Before)
	effectiveRaw, _ := json.Marshal(plan.Effective)
	afterProgress, _ := json.Marshal(plan.Verification.Progress)
	proof, _ := json.Marshal(plan.Verification.Proof)
	documentID, _ := strconv.ParseInt(plan.Verification.Proof.DocumentID, 10, 64)
	var approvalID int64
	err = tx.QueryRow(ctx, `INSERT INTO order_sale_regulation_amendments(publication_id,intent_id,actor_customer_id,before_request,before_hash,effective_request,effective_hash,before_progress,effective_progress,proof,document_id,document_uuid)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::UUID) RETURNING id`, id, plan.IntentID, actor, beforeRaw, plan.BeforeHash, effectiveRaw, plan.EffectiveHash, s.rawProgress, afterProgress, proof, documentID, plan.Verification.Proof.DocumentUUID).Scan(&approvalID)
	if err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE order_sale_publications SET progress=$2,status='queued',message='Регламент согласован владельцем; повторно проверяем тот же документ',next_attempt_at=NOW(),updated_at=NOW() WHERE id=$1`, id, afterProgress); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return RegulationAmendmentApproval{}, err
	}
	return RegulationAmendmentApproval{ID: approvalID, PublicationID: id, IntentID: plan.IntentID, BeforeHash: plan.BeforeHash, EffectiveHash: plan.EffectiveHash, DocumentID: plan.Verification.Proof.DocumentID, DocumentUUID: plan.Verification.Proof.DocumentUUID}, nil
}

// effectivePublicationRequest centralizes approved configuration for all
// provider calls, revisions, cancellation and evidence validation. The original
// request/hash must already have been verified by the caller.
func effectivePublicationRequest(ctx context.Context, q queryer, id int64, before Request) (Request, error) {
	var originalHash, effectiveHash string
	var originalRaw, effectiveRaw []byte
	err := q.QueryRow(ctx, `SELECT before_request,before_hash,effective_request,effective_hash FROM order_sale_regulation_amendments WHERE publication_id=$1`, id).Scan(&originalRaw, &originalHash, &effectiveRaw, &effectiveHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return before, nil
	}
	if err != nil {
		return Request{}, err
	}
	var original, effective Request
	if json.Unmarshal(originalRaw, &original) != nil || json.Unmarshal(effectiveRaw, &effective) != nil || !reflect.DeepEqual(original, before) || !onlyRegulationDiff(original, effective) {
		return Request{}, ErrConflict
	}
	actualBefore, err := requestHash(before)
	if err != nil {
		return Request{}, err
	}
	actualAfter, err := requestHash(effective)
	if err != nil {
		return Request{}, err
	}
	if actualBefore != originalHash || actualAfter != effectiveHash {
		return Request{}, fmt.Errorf("configuration amendment request evidence does not match")
	}
	return effective, nil
}
