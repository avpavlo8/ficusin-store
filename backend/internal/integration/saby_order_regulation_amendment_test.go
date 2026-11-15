package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

func amendmentProviderFixture(t *testing.T) (*realizationProviderHarness, ordersale.Request, ordersale.Request, ordersale.Progress) {
	t.Helper()
	h := newRealizationProviderHarness(t)
	before := realizationProviderRequest()
	before.RegulationID = 122650
	progress := runProviderStep(t, h, before, ordersale.Progress{Stage: "inspect"}).Progress
	progress = runProviderStep(t, h, before, progress).Progress
	effective := before
	effective.RegulationID = 122498
	return h, before, effective, progress
}

func TestSabyRegulationAmendmentOnlyRehashesVerifiedEmptyDraft(t *testing.T) {
	h, before, effective, p := amendmentProviderFixture(t)
	original, _ := json.Marshal(p)
	writes := providerWriteCount(h)
	result, err := h.provider.VerifyRegulationAmendment(context.Background(), before, effective, p)
	if err != nil || providerWriteCount(h) != writes || !result.Proof.EmptyLines || result.Proof.RegulationID != 122498 || result.Proof.DocumentID != "42737" || result.Proof.PublicUUID != before.OperationKey {
		t.Fatal("empty acknowledged draft was not independently verified", result, err, h.writes)
	}
	raw, _ := json.Marshal(p)
	if string(raw) != string(original) {
		t.Fatal("source progress was edited")
	}
	var oldReceipt, newReceipt map[string]json.RawMessage
	json.Unmarshal(p.Native, &oldReceipt)
	json.Unmarshal(result.Progress.Native, &newReceipt)
	var hash string
	json.Unmarshal(newReceipt["requestHash"], &hash)
	if hash != sabyRevisionHash(effective) {
		t.Fatal("effective receipt hash not migrated")
	}
	delete(oldReceipt, "requestHash")
	delete(newReceipt, "requestHash")
	a, _ := json.Marshal(oldReceipt)
	b, _ := json.Marshal(newReceipt)
	if string(a) != string(b) {
		t.Fatal("metadata-only transition changed a native command")
	}
	next := runProviderStep(t, h, effective, result.Progress)
	if next.State != "queued" || next.Progress.Stage != "verify-fill" || providerWriteCount(h) != writes || !next.Progress.CreateAttempted || next.Progress.PostAttempted {
		t.Fatal("first amended step did not re-read the same native document", next, h.writes)
	}
}

func TestSabyRegulationAmendmentRejectsUnprovedDraftBeforeWriting(t *testing.T) {
	for _, mode := range []string{"wrong receipt hash", "unknown create", "post attempted", "foreign ACK", "other field change", "buyer already bound", "nonempty lines", "posted header", "posting queue", "wrong native regulation"} {
		t.Run(mode, func(t *testing.T) {
			h, before, effective, p := amendmentProviderFixture(t)
			switch mode {
			case "wrong receipt hash":
				var receipt sabyRealizationProviderReceipt
				json.Unmarshal(p.Native, &receipt)
				receipt.RequestHash = strings.Repeat("0", 64)
				p.Native, _ = json.Marshal(receipt)
			case "unknown create":
				p.Unknown = true
			case "post attempted":
				p.PostAttempted = true
			case "foreign ACK":
				var receipt sabyRealizationProviderReceipt
				json.Unmarshal(p.Native, &receipt)
				receipt.LastReceipt = json.RawMessage(`{"Идентификатор":"ffffffff-ffff-4fff-8fff-ffffffffffff"}`)
				p.Native, _ = json.Marshal(receipt)
			case "other field change":
				effective.BuyerID++
			case "buyer already bound":
				orderRealizationSet(h.header, "Лицо1", int64(41))
			case "nonempty lines":
				h.rows = []any{realizationProviderLine(true)["d"]}
			case "posted header":
				flags := sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)
				flags[10] = true
				flags[11] = true
			case "posting queue":
				sabyRecordField(h.header, "ДокументРасширение.Состояние").([]any)[18] = true
			case "wrong native regulation":
				orderRealizationSet(h.header, "Регламент", int64(122650))
			}
			writes := providerWriteCount(h)
			if _, err := h.provider.VerifyRegulationAmendment(context.Background(), before, effective, p); err == nil || providerWriteCount(h) != writes {
				t.Fatal("unsafe configuration correction accepted or wrote", mode, err, h.writes)
			}
		})
	}
}
