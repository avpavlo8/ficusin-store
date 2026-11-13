package catalogeditor

import "testing"

func TestPendingPublicationDoesNotDiscardUnknownCreation(t *testing.T) {
	before := Request{ProductID: 11, VariantID: 12, Action: "create", Barcode: "2000000000015", Marketplace: Marketplace{Article: "same"}, Channel: Channel{Mode: "create"}}
	next := before
	next.Marketplace.TNVED = "0601109000"
	for _, scenario := range []struct {
		name, channel, status string
		progress              Progress
		want                  bool
	}{
		{"WB preflight rejected", "wb", "failed", Progress{Stage: "check"}, true},
		{"WB accepted upload unknown", "wb", "failed", Progress{Stage: "reconcile", CreateAttempted: true}, false},
		{"WB ambiguous", "wb", "failed", Progress{Stage: "reconcile", CreateAttempted: true, Ambiguous: true}, false},
		{"still processing", "wb", "queued", Progress{}, false},
		{"Ozon rejected draft", "ozon", "failed", Progress{Stage: "ozon-poll", CreateAttempted: true, TaskID: 123}, true},
		{"Ozon lost create response", "ozon", "failed", Progress{Stage: "reconcile", CreateAttempted: true, Ambiguous: true}, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			got, ok := PendingPublicationProgress(scenario.channel, scenario.status, before, next, scenario.progress)
			if ok != scenario.want {
				t.Fatalf("promotion=%t want %t", ok, scenario.want)
			}
			if ok && scenario.channel == "ozon" && (!got.CreateAttempted || !got.ReapplyPayload || got.Stage != "reconcile") {
				t.Fatal("pending Ozon revision lost read-before-update receipt")
			}
		})
	}
	other := next
	other.VariantID++
	if _, ok := PendingPublicationProgress("wb", "failed", before, other, Progress{}); ok {
		t.Fatal("pending revision moved to another variant")
	}
	native := Request{ProductID: 11, VariantID: 12}
	if _, ok := PendingPublicationProgress("saby", "synced", native, native, Progress{}); !ok {
		t.Fatal("native Saby revision incorrectly required a marketplace article")
	}
}
