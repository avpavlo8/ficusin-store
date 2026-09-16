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
		{SabyName: "Accepted", MatchStatus: "confirmed", ReconciliationStatus: "added", ComparisonAccepted: true, InvoicedQuantity: &qty, ProposedRetailRUB: &price},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Accepted" {
		t.Fatalf("rows = %#v", rows)
	}
}
