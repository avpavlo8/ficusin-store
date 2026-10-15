package procurement

import "testing"

func TestReceivingPDFRowsUseSabyNameCalculatedPriceAndInvoiceQuantity(t *testing.T) {
	qty := 12
	price := int64(1575)
	packages, units := 2, 6
	pot, height := 12.0, 25.0
	rows, err := receivingPDFRows(OrderDetail{Lines: []OrderLine{{
		SabyName: "Бонсай Кармона D12", InvoiceRawName: "Bonsai Carmona Macrophylla In Ceramic",
		MatchStatus: "confirmed", ReconciliationStatus: "changed", InvoicedQuantity: &qty,
		ProposedRetailRUB: &price, PackageCount: &packages, UnitsPerPackage: &units,
		PotDiameterCM: &pot, HeightCM: &height,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Бонсай Кармона D12" || rows[0].Price != 1575 || rows[0].Quantity != 12 {
		t.Fatalf("rows = %#v", rows)
	}
	if packagePDFText(rows[0]) != "6 × 2" {
		t.Fatalf("package = %q", packagePDFText(rows[0]))
	}
}

func TestReceivingPDFRowsIgnoreExcludedAndPendingSupplierRows(t *testing.T) {
	qty := 1
	price := int64(100)
	rows, err := receivingPDFRows(OrderDetail{Lines: []OrderLine{
		{SabyName: "Excluded", MatchStatus: "confirmed", ReconciliationStatus: "excluded", InvoiceExcluded: true, InvoicedQuantity: &qty, ProposedRetailRUB: &price},
		{SabyName: "Pending", MatchStatus: "confirmed", ReconciliationStatus: "added", InvoicedQuantity: &qty, ProposedRetailRUB: &price},
		{SabyName: "Ignored", MatchStatus: "ignored", ReconciliationStatus: "changed", ComparisonAccepted: true, InvoicedQuantity: &qty},
		{SabyName: "Accepted", MatchStatus: "confirmed", ReconciliationStatus: "added", ComparisonAccepted: true, InvoicedQuantity: &qty, ProposedRetailRUB: &price},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Accepted" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestReceivingPDFMarksOnlyApplicablePriceChangesForReprinting(t *testing.T) {
	quantity := 1
	for _, tc := range []struct {
		name     string
		current  float64
		proposed int64
		change   bool
		mode     string
		want     bool
	}{
		{"price increases", 1490, 1790, true, "catalog", true},
		{"price decreases", 1790, 1490, true, "catalog", true},
		{"same price", 1490, 1490, true, "catalog", false},
		{"below change threshold", 1490, 1590, false, "catalog", false},
		{"no previous label", 0, 1790, true, "catalog", false},
		{"customer handoff", 1490, 1790, true, "customer_handoff", false},
		{"unallocated mix", 1490, 1790, true, "assortment", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := receivingPDFRows(OrderDetail{Lines: []OrderLine{{RawName: tc.name, MatchStatus: "confirmed", ReconciliationStatus: "matched", ReceivingMode: tc.mode, InvoicedQuantity: &quantity, CurrentRetailRUB: tc.current, ProposedRetailRUB: &tc.proposed, PriceChangeNeeded: tc.change}}})
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].ReprintLabel != tc.want || rows[0].PreviousPrice != tc.current {
				t.Fatalf("row = %#v, want reprint %v", rows[0], tc.want)
			}
		})
	}
}

func TestReceivingPDFMarksAllocatedMixCardsIndividually(t *testing.T) {
	qty := 3
	price := int64(1790)
	rows, err := receivingPDFRows(OrderDetail{Lines: []OrderLine{{RawName: "Микс", MatchStatus: "deferred", ReceivingMode: "assortment", InvoicedQuantity: &qty, ProposedRetailRUB: &price, ReceivingAllocations: []ReceivingAllocation{
		{SabyID: "lemon", Name: "Лимон", Quantity: 1, CurrentRetailRUB: 1490, PriceChangeNeeded: true},
		{SabyID: "kumquat", Name: "Кумкват", Quantity: 1, CurrentRetailRUB: 1790, PriceChangeNeeded: false},
		{SabyID: "calamondin", Name: "Каламондин", Quantity: 1, CurrentRetailRUB: 0, PriceChangeNeeded: true},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || !rows[0].ReprintLabel || rows[0].PreviousPrice != 1490 || rows[1].ReprintLabel || rows[2].ReprintLabel {
		t.Fatalf("rows = %#v", rows)
	}
}
