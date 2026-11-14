package ordersale

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMissingBuyerPreservesLegacyMockRequestHash(t *testing.T) {
	original := Request{IntentID: 1, ExecutionMode: "mock", Event: Event{Lines: []Line{}}, OperationKey: "11111111-2222-4333-8444-555555555555"}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	// Previously persisted mock requests have no buyer field. New optional
	// targeting metadata cannot invalidate their durable receipt hash.
	if strings.Contains(string(raw), "buyerId") {
		t.Fatal("legacy zero-buyer mock shape changed", string(raw))
	}
	oldHash, err := requestHash(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Request
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	restoredHash, err := requestHash(restored)
	if err != nil || oldHash != restoredHash {
		t.Fatal("legacy request hash changed", err)
	}
	bound := original
	bound.BuyerID = 41
	newHash, err := requestHash(bound)
	if err != nil || oldHash == newHash {
		t.Fatal("verified buyer not bound into immutable request", err)
	}
}
