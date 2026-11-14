package admin

import "strings"

// New saves materialize each channel's effective article. Frozen publications
// retain their original bytes and continue through the narrow old-data reader.
func canonicalEditableEditorPlan(plan EditorPlan) EditorPlan {
	plan.Variants = append([]EditorVariantPlan(nil), plan.Variants...)
	for i := range plan.Variants {
		v := &plan.Variants[i]
		if v.PriceMode == "" && (strings.TrimSpace(v.WBPrice) != "" || strings.TrimSpace(v.OzonPrice) != "") {
			v.PriceMode = "manual"
		}
		common := strings.TrimSpace(v.Marketplace.Article)
		if v.Marketplace.WB.Article == nil {
			article := common
			v.Marketplace.WB.Article = &article
		}
		if v.Marketplace.Ozon.Article == nil {
			article := common
			v.Marketplace.Ozon.Article = &article
		}
		v.Marketplace.Article = ""
	}
	return plan
}
