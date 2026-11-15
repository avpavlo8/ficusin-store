package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

func TestSabyRealizationPostingFlagsSettleThroughBoundedReadsOnly(t *testing.T) {
	for _, stage := range []string{"verify-post", "reconcile-post"} {
		for _, flags := range []struct {
			name          string
			errors, stale bool
		}{{"posting error", true, false}, {"stale entries", false, true}, {"both", true, true}} {
			t.Run(stage+"/"+flags.name, func(t *testing.T) {
				h := newRealizationProviderHarness(t)
				r := realizationProviderRequest()
				p := providerToStage(t, h, r, "verify-post")
				p.Stage, p.Unknown = stage, stage == "reconcile-post"
				h.postVerificationFlags = []struct{ errors, stale bool }{{flags.errors, flags.stale}, {flags.errors, flags.stale}, {false, false}}
				writes := providerWriteCount(h)
				for poll := 1; poll <= 2; poll++ {
					result := runProviderStep(t, h, r, p)
					var receipt sabyRealizationProviderReceipt
					json.Unmarshal(result.Progress.Native, &receipt)
					if result.State != "queued" || result.RetryAfter != 5*time.Second || result.Progress.Stage != stage ||
						!result.Progress.PostAttempted || result.StockVerified || len(result.StockProof) != 0 || receipt.Polls != poll ||
						result.Progress.DocumentID != p.DocumentID || result.Progress.DocumentUUID != p.DocumentUUID || providerWriteCount(h) != writes {
						t.Fatal("transient posting flag caused success, attention or another write", result, h.writes)
					}
					p = result.Progress
				}
				result := runProviderStep(t, h, r, p)
				var proof ordersale.StockProof
				if result.State != "posted" || !result.StockVerified || result.Progress.Unknown ||
					json.Unmarshal(result.StockProof, &proof) != nil || proof.PostingErrors || proof.StaleEntries ||
					!proof.Posted || !proof.HasEntries || providerWriteCount(h) != writes || h.writeCount("Документ.PostAsync") != 1 {
					t.Fatal("settled posting was not independently verified exactly once", result, h.writes)
				}
			})
		}
	}
}

func TestSabyRealizationPersistentPostingFlagsStopAfterTenReadsWithoutRepost(t *testing.T) {
	for _, stage := range []string{"verify-post", "reconcile-post"} {
		t.Run(stage, func(t *testing.T) {
			h := newRealizationProviderHarness(t)
			r := realizationProviderRequest()
			p := providerToStage(t, h, r, "verify-post")
			p.Stage, p.Unknown = stage, stage == "reconcile-post"
			for range 11 {
				h.postVerificationFlags = append(h.postVerificationFlags, struct{ errors, stale bool }{true, true})
			}
			writes := providerWriteCount(h)
			for poll := 1; poll <= 10; poll++ {
				result := runProviderStep(t, h, r, p)
				var receipt sabyRealizationProviderReceipt
				json.Unmarshal(result.Progress.Native, &receipt)
				wanted := "queued"
				if poll == 10 {
					wanted = "needs_attention"
				}
				if result.State != wanted || result.Progress.Stage != stage || !result.Progress.PostAttempted || receipt.Polls != poll ||
					result.StockVerified || len(result.StockProof) != 0 || providerWriteCount(h) != writes || h.writeCount("Документ.PostAsync") != 1 {
					t.Fatal("persistent posting error escaped read-only polling limit", poll, result, h.writes)
				}
				if poll == 10 && (!strings.Contains(result.Message, "10") || len(h.postVerificationFlags) != 1) {
					t.Fatal("polling bound was not persisted", result, len(h.postVerificationFlags))
				}
				p = result.Progress
			}
		})
	}
}
