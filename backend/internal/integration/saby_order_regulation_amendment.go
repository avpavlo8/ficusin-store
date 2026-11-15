package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

// VerifyRegulationAmendment only reads an already acknowledged empty draft.
// It neither changes its operation nor creates/replays a native command.
func (p *SabyOrderRealizationProvider) VerifyRegulationAmendment(ctx context.Context, before, effective ordersale.Request, progress ordersale.Progress) (ordersale.RegulationAmendmentVerification, error) {
	var out ordersale.RegulationAmendmentVerification
	fail := func() (ordersale.RegulationAmendmentVerification, error) {
		return out, errors.New("Не подтверждено безопасное исправление регламента пустой реализации")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if p == nil || !p.Client.Configured() || before.ExecutionMode != "live" || effective.ExecutionMode != "live" || before.RegulationID <= 0 || effective.RegulationID <= 0 || before.RegulationID == effective.RegulationID || !sabyOrderRealizationUUID.MatchString(p.PublicRegulationUUID) {
		return fail()
	}
	comparison := effective
	comparison.RegulationID = before.RegulationID
	if !reflect.DeepEqual(comparison, before) || progress.Stage != "discover" || !progress.CreateAttempted || progress.PostAttempted || progress.Unknown || progress.IdentityConflict || progress.DocumentID != "" || progress.DocumentUUID != "" || len(progress.OwnedRowIDs) > 0 || len(progress.RevisionNative) > 0 || progress.ActiveRevisionID > 0 || progress.TargetRevisionID > 0 {
		return fail()
	}
	oldHash := sabyRevisionHash(before)
	newHash := sabyRevisionHash(effective)
	var receipt sabyRealizationProviderReceipt
	if json.Unmarshal(progress.Native, &receipt) != nil || receipt.Version != 1 || receipt.RequestHash != oldHash || receipt.Phase != "discover" || receipt.Polls < 0 || receipt.Polls > 10 || receipt.LineIndex != 0 || receipt.Prepared != nil || len(receipt.KnownLines) > 0 || receipt.SavedRowID != 0 || receipt.PostingVerified || receipt.CancelSawPosted || receipt.CancelPolls != 0 || !sabyRealizationProviderPreparedCreateMatches(receipt.CreatePayload, before, p.PublicRegulationUUID) {
		return fail()
	}
	var ack struct {
		ID string `json:"Идентификатор"`
	}
	if json.Unmarshal(receipt.LastReceipt, &ack) != nil || !strings.EqualFold(ack.ID, before.OperationKey) {
		return fail()
	}
	public, exists, err := p.readPublic(ctx, before.OperationKey)
	if err != nil || !exists || !p.publicMatches(public, before, receipt.CreatePayload) {
		return fail()
	}
	snapshot, found, err := p.discover(ctx, effective, receipt.CreatePayload)
	if err != nil || !found {
		return fail()
	}
	identity := SabyOrderRealizationIdentity{ID: snapshot.ID, UUID: snapshot.UUID, WarehouseID: effective.NativeWarehouseID, MovementWarehouseID: effective.MovementWarehouseID}
	snapshot, err = p.Client.ReadOrderRealizationWithLines(ctx, identity)
	if err != nil || p.validateHeader(snapshot, effective, receipt.CreatePayload) != nil || snapshot.BuyerID != 0 || len(snapshot.Lines) != 0 || snapshot.Posted || snapshot.HasEntries || snapshot.PostingErrors || snapshot.StaleEntries || snapshot.PostingQueued || snapshot.EntriesQueued {
		return fail()
	}
	prices, err := p.Client.ReadOrderRealizationPricedLines(ctx, identity)
	if err != nil || len(prices) != 0 {
		return fail()
	}
	rawProgress, err := json.Marshal(progress)
	if err != nil {
		return fail()
	}
	sum := sha256.Sum256(rawProgress)
	out.Progress = progress
	receipt.RequestHash = newHash
	out.Progress.Native, err = json.Marshal(receipt)
	if err != nil {
		return fail()
	}
	out.Proof = ordersale.RegulationAmendmentProof{Version: 1, VerifiedAt: time.Now().UTC(), BeforeHash: oldHash, EffectiveHash: newHash, ProgressHash: hex.EncodeToString(sum[:]),
		DocumentID: strconv.FormatInt(snapshot.ID, 10), DocumentUUID: snapshot.UUID, PublicUUID: snapshot.PublicUUID,
		PublicType: "ДокОтгрИсх", PublicRegulationUUID: p.PublicRegulationUUID, PublicRegulationName: "Реализация",
		OrganizationID: snapshot.OrganizationID, WarehouseID: snapshot.WarehouseID, MovementWarehouseID: effective.MovementWarehouseID, RegulationID: snapshot.RegulationID, BuyerID: snapshot.BuyerID,
		HeaderRead: true, PostingStateKnown: snapshot.PostingStateKnown, NullableDraftFlags: !snapshot.PostingStateKnown, LinesRead: true, EmptyLines: true, Posted: snapshot.Posted, HasEntries: snapshot.HasEntries, PostingErrors: snapshot.PostingErrors, StaleEntries: snapshot.StaleEntries, PostingQueued: snapshot.PostingQueued, EntriesQueued: snapshot.EntriesQueued}
	return out, nil
}
