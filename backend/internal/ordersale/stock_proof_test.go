package ordersale

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func liveStockProofFixture() (Request, StockProof) {
	r := Request{IntentID: 117, ExecutionMode: "live", OperationKey: "00000000-0000-4000-8000-000000000117", SiteWarehouseID: 7, NativeWarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 31, RegulationID: 122650, BuyerID: 41,
		Event: Event{Key: "shipment:301", OrderID: 17, ShipmentOfferID: 301, DeliveryMethod: "cdek", Status: "shipped", CDEKStatus: "IN_TRANSIT", WarehouseID: 7, Lines: []Line{
			{OrderItemID: 1, VariantID: 10, SabyID: "1101", Quantity: 2, UnitPrice: "10.20"}, {OrderItemID: 2, VariantID: 11, SabyID: "1101", Quantity: 1, UnitPrice: "10.20"}, {OrderItemID: 3, VariantID: 12, SabyID: "1102", Quantity: 2, UnitPrice: "7.50"},
		}}}
	finish(&r.Event)
	p := StockProof{Version: 1, Test: false, ExecutionMode: "live", IntentID: r.IntentID, EventKey: r.Event.Key, EventFingerprint: r.Event.Fingerprint, OperationKey: r.OperationKey,
		DocumentID: "2357", DocumentUUID: "00000000-0000-4000-8000-000000002357", SiteWarehouseID: 7, NativeWarehouseID: 283, MovementWarehouseID: 284, OrganizationID: 31, RegulationID: 122650, BuyerID: 41, Posted: true, HasEntries: true,
		Lines: []StockProofLine{{ID: 1, DocumentID: "2357", NomenclatureID: "1101", WarehouseID: 284, Direction: 1, Quantity: "2", UnitPrice: "10.20", LineSum: "20.40", Closed: true}, {ID: 2, DocumentID: "2357", NomenclatureID: "1101", WarehouseID: 284, Direction: 1, Quantity: "1", UnitPrice: "10.20", LineSum: "10.20", Closed: true}, {ID: 3, DocumentID: "2357", NomenclatureID: "1102", WarehouseID: 284, Direction: 1, Quantity: "2", UnitPrice: "7.50", LineSum: "15.00", Closed: true}}}
	return r, p
}
func stockProofResult(t *testing.T, p StockProof) Result {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return Result{State: "posted", StockVerified: true, Progress: Progress{Stage: "verify-post", DocumentID: p.DocumentID, DocumentUUID: p.DocumentUUID}, StockProof: raw}
}

func TestStockProofBindsNativeHeaderAndExactOutgoingQuantities(t *testing.T) {
	r, p := liveStockProofFixture()
	if !validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("verified native proof rejected")
	}
	// Two order lines for one nomenclature may be split into different native
	// ledger rows. Compare exact aggregate quantities, never float64/row count.
	p.Lines[0].Quantity, p.Lines[1].Quantity = "1.25", "1.75"
	p.Lines[0].LineSum, p.Lines[1].LineSum = "12.75", "17.85"
	if !validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("exact split ledger quantities rejected")
	}
	p.Lines[1].Quantity = "1.750000001"
	if validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("fractional overstatement accepted")
	}
}

func TestStockProofRejectsMismatchedOrIncompleteNativeEvidence(t *testing.T) {
	cases := map[string]func(*StockProof){
		"wrong price same quantity": func(p *StockProof) { p.Lines[0].UnitPrice = "9.00"; p.Lines[0].LineSum = "18.00" },
		"wrong sum same quantity":   func(p *StockProof) { p.Lines[0].LineSum = "20.41" },
		"missing price":             func(p *StockProof) { p.Lines[0].UnitPrice = "" },
		"excess money precision":    func(p *StockProof) { p.Lines[0].UnitPrice = "10.201" },
		"negative sum":              func(p *StockProof) { p.Lines[0].LineSum = "-20.40" },
		"version":                   func(p *StockProof) { p.Version++ },
		"mode":                      func(p *StockProof) { p.ExecutionMode = "mock" },
		"test":                      func(p *StockProof) { p.Test = true },
		"intent":                    func(p *StockProof) { p.IntentID++ },
		"event":                     func(p *StockProof) { p.EventKey = "order:18" },
		"fingerprint":               func(p *StockProof) { p.EventFingerprint = strings.Repeat("b", 64) },
		"operation":                 func(p *StockProof) { p.OperationKey = "00000000-0000-4000-8000-000000000118" },
		"site warehouse":            func(p *StockProof) { p.SiteWarehouseID++ },
		"native warehouse":          func(p *StockProof) { p.NativeWarehouseID++ },
		"movement warehouse":        func(p *StockProof) { p.MovementWarehouseID++ },
		"organization":              func(p *StockProof) { p.OrganizationID++ },
		"regulation":                func(p *StockProof) { p.RegulationID++ },
		"buyer":                     func(p *StockProof) { p.BuyerID++ },
		"not posted":                func(p *StockProof) { p.Posted = false },
		"no entries":                func(p *StockProof) { p.HasEntries = false },
		"posting errors":            func(p *StockProof) { p.PostingErrors = true },
		"stale entries":             func(p *StockProof) { p.StaleEntries = true },
		"queued posting":            func(p *StockProof) { p.PostingQueued = true },
		"queued entries":            func(p *StockProof) { p.EntriesQueued = true },
		"missing goods":             func(p *StockProof) { p.Lines = p.Lines[:2] },
		"extra goods":               func(p *StockProof) { p.Lines[0].NomenclatureID = "1103" },
		"wrong quantity":            func(p *StockProof) { p.Lines[0].Quantity = "1" },
		"negative quantity":         func(p *StockProof) { p.Lines[0].Quantity = "-2" },
		"zero quantity":             func(p *StockProof) { p.Lines[0].Quantity = "0" },
		"unbounded exponent":        func(p *StockProof) { p.Lines[0].Quantity = "2e99999999" },
		"incoming":                  func(p *StockProof) { p.Lines[0].Direction = 0 },
		"open row":                  func(p *StockProof) { p.Lines[0].Closed = false },
		"duplicate row":             func(p *StockProof) { p.Lines[0].ID = p.Lines[1].ID },
		"missing row identity":      func(p *StockProof) { p.Lines[0].ID = 0 },
		"foreign row document":      func(p *StockProof) { p.Lines[0].DocumentID = "2358" },
		"foreign row warehouse":     func(p *StockProof) { p.Lines[0].WarehouseID = 285 },
		"mock native document":      func(p *StockProof) { p.DocumentID = "STG-SALE-117" },
		"empty UUID":                func(p *StockProof) { p.DocumentUUID = "" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r, p := liveStockProofFixture()
			change(&p)
			if validStockProof(r, stockProofResult(t, p)) {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	r, p := liveStockProofFixture()
	for _, change := range []func(*Result){
		func(v *Result) { v.Progress.DocumentID = "2358" },
		func(v *Result) { v.Progress.DocumentUUID = "00000000-0000-4000-8000-000000002358" },
		func(v *Result) { v.Progress.Unknown = true },
		func(v *Result) { v.Progress.IdentityConflict = true },
		func(v *Result) { v.StockVerified = false },
		func(v *Result) { v.State = "queued" },
	} {
		result := stockProofResult(t, p)
		change(&result)
		if validStockProof(r, result) {
			t.Fatal("receipt identity/state mismatch accepted")
		}
	}
}

func TestStockProofRejectsMissingFlagsAndArbitraryJSON(t *testing.T) {
	r, p := liveStockProofFixture()
	for _, raw := range []string{`{}`, `null`, `[]`, `{"unrelated":true}`, `{"test":false}`, `{"test":true}`} {
		result := stockProofResult(t, p)
		result.StockProof = json.RawMessage(raw)
		if validStockProof(r, result) {
			t.Fatal("arbitrary JSON accepted", raw)
		}
	}
	for _, field := range []string{"test", "postingErrors", "staleEntries", "postingQueued", "entriesQueued", "eventFingerprint"} {
		for _, null := range []bool{false, true} {
			result := stockProofResult(t, p)
			var values map[string]json.RawMessage
			_ = json.Unmarshal(result.StockProof, &values)
			delete(values, field)
			if null {
				values[field] = json.RawMessage(`null`)
			}
			result.StockProof, _ = json.Marshal(values)
			if validStockProof(r, result) {
				t.Fatal("missing/unknown field treated as known", field, null)
			}
		}
	}
	result := stockProofResult(t, p)
	result.StockProof = append(result.StockProof, []byte(` {}`)...)
	if validStockProof(r, result) {
		t.Fatal("trailing JSON accepted")
	}
	result.StockProof = json.RawMessage(fmt.Sprintf(`{"padding":%q}`, strings.Repeat("x", maxStockProofBytes)))
	if validStockProof(r, result) {
		t.Fatal("unbounded evidence accepted")
	}
}

func TestMockStockProofIsExplicitAndCannotMasqueradeAsLive(t *testing.T) {
	r, _ := liveStockProofFixture()
	r.ExecutionMode = "mock"
	r.NativeWarehouseID, r.MovementWarehouseID, r.OrganizationID, r.RegulationID, r.BuyerID = 0, 0, 0, 0, 0
	// New staging goods may deliberately carry synthetic IDs.
	r.Event.Lines[0].SabyID = "STG-GOODS-10"
	finish(&r.Event)
	progress := Progress{Stage: "verify-post", DocumentID: "STG-SALE-117", DocumentUUID: r.OperationKey}
	raw, err := mockStockProof(r, progress)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{State: "posted", StockVerified: true, Progress: progress, StockProof: raw}
	if !validStockProof(r, result) {
		t.Fatal("explicit staging proof rejected")
	}
	var proof StockProof
	_ = json.Unmarshal(raw, &proof)
	proof.Test = false
	result.StockProof, _ = json.Marshal(proof)
	if validStockProof(r, result) {
		t.Fatal("mock without explicit test flag accepted")
	}
	r.ExecutionMode = "live"
	if _, err := mockStockProof(r, progress); err == nil {
		t.Fatal("mock builder emitted live proof")
	}
	if validStockProof(r, Result{State: "posted", StockVerified: true, Progress: progress, StockProof: raw}) {
		t.Fatal("mock proof accepted as live")
	}
}

func TestStockProofPreservesCanonicalIDsAboveFloatPrecision(t *testing.T) {
	r, p := liveStockProofFixture()
	r.Event.Lines[2].SabyID, p.Lines[2].NomenclatureID = "9007199254740993", "9007199254740993"
	finish(&r.Event)
	p.EventFingerprint = r.Event.Fingerprint
	if !validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("large canonical identity lost precision")
	}
	p.Lines[2].NomenclatureID = "9007199254740992"
	if validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("adjacent large identity accepted")
	}
}

func TestStockProofSeparatesSameGoodsAtDifferentOrderPrices(t *testing.T) {
	r, p := liveStockProofFixture()
	r.Event.Lines[1].UnitPrice = "9.00"
	finish(&r.Event)
	p.EventFingerprint = r.Event.Fingerprint
	p.Lines[1].UnitPrice = "9.00"
	p.Lines[1].LineSum = "9.00"
	if !validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("correct price groups rejected")
	}
	// Same total canonical quantity and money remain insufficient when quantities
	// are shifted between unit prices. Each order price must retain its exact qty.
	p.Lines[0].Quantity = "1"
	p.Lines[0].LineSum = "10.20"
	p.Lines[1].Quantity = "2"
	p.Lines[1].LineSum = "18.00"
	if validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("canonical-only quantity match hid wrong price groups")
	}
}
func TestStockProofRequiresPriceAndSumFields(t *testing.T) {
	r, p := liveStockProofFixture()
	for _, field := range []string{"unitPrice", "lineSum"} {
		result := stockProofResult(t, p)
		var root map[string]json.RawMessage
		json.Unmarshal(result.StockProof, &root)
		var rows []map[string]json.RawMessage
		json.Unmarshal(root["lines"], &rows)
		delete(rows[0], field)
		root["lines"], _ = json.Marshal(rows)
		result.StockProof, _ = json.Marshal(root)
		if validStockProof(r, result) {
			t.Fatal("missing money field accepted", field)
		}
	}
	r.Event.Lines[0].UnitPrice = ""
	if _, err := mockStockProof(Request{ExecutionMode: "mock", Event: r.Event}, Progress{}); err == nil {
		t.Fatal("mock silently invented missing order price")
	}
}

func TestStockProofAcceptsExplicitZeroMoneyButRejectsQuotedNumbers(t *testing.T) {
	r, p := liveStockProofFixture()
	for i := range r.Event.Lines {
		r.Event.Lines[i].UnitPrice = "0.00"
		p.Lines[i].UnitPrice = "0"
		p.Lines[i].LineSum = "0.00"
	}
	finish(&r.Event)
	p.EventFingerprint = r.Event.Fingerprint
	if !validStockProof(r, stockProofResult(t, p)) {
		t.Fatal("explicit zero price was mistaken for missing price")
	}
	for _, field := range []string{"quantity", "unitPrice", "lineSum"} {
		result := stockProofResult(t, p)
		var root map[string]json.RawMessage
		json.Unmarshal(result.StockProof, &root)
		var rows []map[string]json.RawMessage
		json.Unmarshal(root["lines"], &rows)
		rows[0][field] = json.RawMessage(`"0"`)
		root["lines"], _ = json.Marshal(rows)
		result.StockProof, _ = json.Marshal(root)
		if validStockProof(r, result) {
			t.Fatal("string-valued native numeric proof accepted", field)
		}
	}
}
