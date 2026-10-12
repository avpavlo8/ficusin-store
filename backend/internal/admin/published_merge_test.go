package admin

import (
	"reflect"
	"testing"
)

func TestPublishedMergeRequestRequiresReviewedIdentityAndExactSKUs(t *testing.T) {
	tests := []struct {
		name  string
		input PublishedMergeRequest
		valid bool
	}{
		{"reviewed", PublishedMergeRequest{TargetProductID: 112, SourceProductIDs: []int64{113}, ExpectedSKUs: []string{"112", "113"}, ReviewedIdentity: "Замиокулькас Raven"}, true},
		{"missing identity", PublishedMergeRequest{TargetProductID: 112, SourceProductIDs: []int64{113}, ExpectedSKUs: []string{"112", "113"}}, false},
		{"self merge", PublishedMergeRequest{TargetProductID: 112, SourceProductIDs: []int64{112}, ExpectedSKUs: []string{"112", "113"}, ReviewedIdentity: "Замиокулькас Raven"}, false},
		{"duplicate source", PublishedMergeRequest{TargetProductID: 112, SourceProductIDs: []int64{113, 113}, ExpectedSKUs: []string{"112", "113"}, ReviewedIdentity: "Замиокулькас Raven"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ids, err := validatePublishedMergeRequest(test.input)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, err=%v", test.valid, err)
			}
			if test.valid && !reflect.DeepEqual(ids, []int64{112, 113}) {
				t.Fatalf("unexpected locked IDs: %v", ids)
			}
		})
	}
}

func TestPublishedMergeNameAndSKUComparison(t *testing.T) {
	if got := stripVariantSize("Дипсис Арека 55 см · D17"); got != "Дипсис Арека" {
		t.Fatalf("unexpected stripped title: %q", got)
	}
	if normalizeMergeName("Дипсис Арека 40 см · D10") != normalizeMergeName("Дипсис Арека 55 см · D17") {
		t.Fatal("sizes should be SKU facts, not botanical differences")
	}
	if normalizeMergeName("Замиокулькас «Зензи»") != normalizeMergeName("Замиокулькас Зензи") {
		t.Fatal("typographic quotes should not separate the same reviewed cultivar")
	}
	if normalizeMergeName("Замиокулькас Raven") == normalizeMergeName("Замиокулькас Zenzi") {
		t.Fatal("different cultivars must remain distinct")
	}
	if !sameUniqueStrings([]string{"110", "111", "798"}, []string{"798", "110", "111"}) {
		t.Fatal("exact SKU set should be order independent")
	}
	for _, expected := range [][]string{{"110", "111"}, {"110", "110", "798"}, {"110", "111", "999"}} {
		if sameUniqueStrings([]string{"110", "111", "798"}, expected) {
			t.Fatalf("stale or duplicate SKU set was accepted: %v", expected)
		}
	}
}

func TestPublishedMergeRequiresExternalIDsScopedToSKU(t *testing.T) {
	for _, test := range []struct {
		variants, unscoped int
		reject             bool
	}{
		{1, 2, false}, // All product-level IDs can be assigned to the only SKU.
		{2, 0, false}, // Every ID is already assigned to a specific size.
		{2, 1, true},  // A product-level ID cannot be guessed among sizes.
		{3, 4, true},
	} {
		if got := unscopedIDsAmbiguous(test.variants, test.unscoped); got != test.reject {
			t.Fatalf("variants=%d unscoped=%d: reject=%v want %v", test.variants, test.unscoped, got, test.reject)
		}
	}
}

func TestPublishedMergeRejectsGenusOnlyLatinIdentity(t *testing.T) {
	for _, item := range []struct {
		latin string
		valid bool
	}{
		{"Zamioculcas", false},
		{"Ficus", false},
		{"Ficus sp.", false},
		{"Zamioculcas zamiifolia 'Raven'", true},
		{"Ficus benjamina", true},
		{"Nephrolepis 'Green Lady'", true},
	} {
		if got := completeMergeLatinIdentity(item.latin); got != item.valid {
			t.Fatalf("latin=%q: valid=%v, want %v", item.latin, got, item.valid)
		}
	}
}
