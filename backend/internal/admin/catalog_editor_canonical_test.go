package admin

import (
	"encoding/json"
	"testing"
)

func TestCanonicalEditableEditorPlanPreservesEffectiveValues(t *testing.T) {
	const source = `{"variants":[
	 {"variantId":1,"wbPrice":"123.45","marketplace":{"article":" old ","wb":{"article":null},"ozon":{"article":""}}},
	 {"variantId":2,"priceMode":"auto","ozonPrice":"500","marketplace":{"article":"unused","wb":{"article":" WB "},"ozon":{"article":"OZ"}}},
	 {"variantId":3,"marketplace":{"article":" no-prices "}},
	 {"variantId":4,"priceMode":"manual","marketplace":{}}
	]}`
	var original EditorPlan
	if err := json.Unmarshal([]byte(source), &original); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(original)
	got := canonicalEditableEditorPlan(original)
	afterOriginal, _ := json.Marshal(original)
	if string(before) != string(afterOriginal) {
		t.Fatal("canonicalization modified caller's draft")
	}
	for i, v := range got.Variants {
		for _, channel := range []string{"wb", "ozon"} {
			if v.Marketplace.SellerArticle(channel) != original.Variants[i].Marketplace.SellerArticle(channel) {
				t.Fatalf("changed effective article: variant %d, channel %s", i, channel)
			}
		}
		if v.WBPrice != original.Variants[i].WBPrice || v.OzonPrice != original.Variants[i].OzonPrice {
			t.Fatal("changed saved price")
		}
		if v.Marketplace.Article != "" || v.Marketplace.WB.Article == nil || v.Marketplace.Ozon.Article == nil {
			t.Fatal("shared article not materialized")
		}
	}
	if *got.Variants[0].Marketplace.Ozon.Article != "" || got.Variants[0].PriceMode != "manual" || got.Variants[1].PriceMode != "auto" || got.Variants[2].PriceMode != "" || got.Variants[3].PriceMode != "manual" {
		t.Fatalf("presence/mode semantics lost: %+v", got)
	}
	once, _ := json.Marshal(got)
	twice, _ := json.Marshal(canonicalEditableEditorPlan(got))
	if string(once) != string(twice) {
		t.Fatal("canonicalization is not idempotent")
	}
}
