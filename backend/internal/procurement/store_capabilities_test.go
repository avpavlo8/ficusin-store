package procurement

import (
	"context"
	"encoding/json"
	"time"
)

// Fixtures opt into the operation under test. Any other required capability
// must fail immediately rather than quietly simulate an incomplete old store.
func (*storeStub) FirstSabyPriceExportLines(context.Context, Actor, int64, []SabyFirstPrice) ([]OrderLine, error) {
	panic("unexpected first-price export")
}
func (*storeStub) ListSabyCatalogFolders(context.Context) ([]SabyCatalogFolder, error) {
	panic("unexpected catalogue folders")
}
func (*storeStub) PreviewPlan(context.Context, PlanCreate) (PlanPreview, error) {
	panic("unexpected plan preview")
}
func (*storeStub) PreviewUnitPrice(context.Context, UnitPriceInput) (UnitPricePreview, error) {
	panic("unexpected unit-price preview")
}
func (*storeStub) PricingExample(context.Context, PricingSettings) (PricingExample, error) {
	panic("unexpected pricing example")
}
func (*storeStub) UpdateSupplier(context.Context, Actor, int64, SupplierUpdate) (Supplier, error) {
	panic("unexpected supplier update")
}
func (*storeStub) DocumentPDF(context.Context, int64) ([]byte, error) {
	panic("unexpected document download")
}
func (*storeStub) ProductPage(context.Context, int64, string, string, int, int) (ProductPage, error) {
	panic("unexpected product page")
}
func (*storeStub) RecheckReceipt(context.Context, Actor, int64) (ActionBatch, error) {
	panic("unexpected receipt recheck")
}
func (*storeStub) LoadPlanDraft(context.Context, int64) (*PlanDraft, error) {
	panic("unexpected plan draft load")
}
func (*storeStub) SavePlanDraft(context.Context, int64, json.RawMessage) (PlanDraft, error) {
	panic("unexpected plan draft save")
}
func (*storeStub) DeletePlanDraft(context.Context, int64) error {
	panic("unexpected plan draft delete")
}
func (*storeStub) ListNamedPlanDrafts(context.Context) ([]NamedPlanDraft, error) {
	panic("unexpected named drafts list")
}
func (*storeStub) LoadNamedPlanDraft(context.Context, int64) (NamedPlanDraft, error) {
	panic("unexpected named draft load")
}
func (*storeStub) CreateNamedPlanDraft(context.Context, Actor, string, json.RawMessage) (NamedPlanDraft, error) {
	panic("unexpected named draft create")
}
func (*storeStub) UpdateNamedPlanDraft(context.Context, Actor, int64, string, json.RawMessage) (NamedPlanDraft, error) {
	panic("unexpected named draft update")
}
func (*storeStub) DeleteNamedPlanDraft(context.Context, int64) error {
	panic("unexpected named draft delete")
}
func (*storeStub) ClaimIntegrationSync(context.Context, string, string, string, time.Duration) (*SyncClaim, error) {
	panic("unexpected queue claim")
}
func (*storeStub) FinishIntegrationSync(context.Context, SyncClaim, int, time.Time, time.Time, *time.Time, time.Duration, error) (bool, error) {
	panic("unexpected queue finish")
}
func (*storeStub) ListIntegrationSync(context.Context) ([]IntegrationSyncStatus, error) {
	panic("unexpected queue list")
}
func (s *storeStub) RequestIntegrationSync(_ context.Context, channel, resource string) (IntegrationSyncStatus, error) {
	if s.syncRequests == nil {
		panic("unexpected queue request")
	}
	s.syncRequests = append(s.syncRequests, channel+"/"+resource)
	return IntegrationSyncStatus{Status: "queued"}, nil
}
func (s *storeStub) CachedChannelProducts(_ context.Context, channel string) ([]ChannelProduct, error) {
	if s.catalogCache == nil {
		panic("unexpected catalogue mirror read")
	}
	return s.catalogCache[channel], nil
}
