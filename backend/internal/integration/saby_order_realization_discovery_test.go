package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

func TestSabyRealizationPinsPublicRegulationAndReadsLegacyFrozenPayload(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit UUID", true: "legacy name-only"}[legacy], func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			p := runProviderStep(t, h, r, ordersale.Progress{Stage: "inspect"}).Progress
			var receipt sabyRealizationProviderReceipt
			if json.Unmarshal(p.Native, &receipt) != nil {
				t.Fatal("missing frozen receipt")
			}
			var create map[string]any
			if json.Unmarshal(receipt.CreatePayload, &create) != nil {
				t.Fatal("missing frozen create request")
			}
			reg := create["Регламент"].(map[string]any)
			if reg["Идентификатор"] != h.provider.PublicRegulationUUID || reg["Название"] != "Реализация" {
				t.Fatal("operation was selected only by a mutable name", reg)
			}
			if legacy {
				delete(reg, "Идентификатор")
				receipt.CreatePayload, _ = json.Marshal(create)
				p.Native, _ = json.Marshal(receipt)
			}
			result := runProviderStep(t, h, r, p)
			if result.State != "queued" || result.Progress.Stage != "discover" || h.writeCount("СБИС.ЗаписатьДокумент") != 1 {
				t.Fatal("valid immutable create receipt did not advance once", result)
			}
		})
	}
}

func TestSabyRealizationDiscoveryReportsRegulationMismatchWithoutWriting(t *testing.T) {
	h := newRealizationProviderHarness(t)
	r := realizationProviderRequest()
	r.RegulationID = 122650 // Real mismatch: public ordinary realization is 122498.
	p := runProviderStep(t, h, r, ordersale.Progress{Stage: "inspect"}).Progress
	p = runProviderStep(t, h, r, p).Progress
	before := providerWriteCount(h)
	for range 2 {
		result := runProviderStep(t, h, r, p)
		if result.State != "needs_attention" || result.Progress.Stage != "discover" ||
			!strings.Contains(result.Message, "122498") || !strings.Contains(result.Message, "122650") ||
			result.Progress.DocumentID != "" || result.Progress.DocumentUUID != "" || result.Progress.PostAttempted ||
			providerWriteCount(h) != before {
			t.Fatal("regulation mismatch was hidden, adopted or mutated", result, h.writes)
		}
		var receipt sabyRealizationProviderReceipt
		if json.Unmarshal(result.Progress.Native, &receipt) != nil || receipt.Polls != 0 {
			t.Fatal("deterministic mismatch became eventual-consistency polling", result)
		}
		p = result.Progress
	}
}

func TestSabyAcknowledgedDiscoveryRevalidatesReceiptBeforeAnyRead(t *testing.T) {
	for _, mode := range []string{"missing ACK", "foreign ACK", "wrong request hash"} {
		t.Run(mode, func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			p := runProviderStep(t, h, r, ordersale.Progress{Stage: "inspect"}).Progress
			p = runProviderStep(t, h, r, p).Progress
			var receipt sabyRealizationProviderReceipt
			json.Unmarshal(p.Native, &receipt)
			switch mode {
			case "missing ACK":
				receipt.LastReceipt = nil
			case "foreign ACK":
				receipt.LastReceipt = json.RawMessage(`{"Идентификатор":"ffffffff-ffff-4fff-8fff-ffffffffffff"}`)
			case "wrong request hash":
				receipt.RequestHash = strings.Repeat("0", 64)
			}
			p.Native, _ = json.Marshal(receipt)
			before := len(h.calls)
			writes := providerWriteCount(h)
			result := runProviderStep(t, h, r, p)
			if result.State != "needs_attention" || len(h.calls) != before || providerWriteCount(h) != writes {
				t.Fatal("unproved discovery receipt reached provider", result, h.calls[before:])
			}
		})
	}
}
