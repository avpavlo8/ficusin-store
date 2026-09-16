package procurement

import (
	"os"
	"strings"
	"testing"
)

func TestValidInvoiceLoadUnitRejectsPlanningShelf(t *testing.T) {
	for _, value := range []string{"", "shelf", "CC1", "1.0"} {
		if validInvoiceLoadUnit(value) {
			t.Fatalf("%q must not be an invoice trolley id", value)
		}
	}
	for _, value := range []string{"1", " 2 ", "12"} {
		if !validInvoiceLoadUnit(value) {
			t.Fatalf("%q must be an invoice trolley id", value)
		}
	}
}

func TestParseHollandKeepsBoxAcrossPagesUntilNextBox(t *testing.T) {
	doc, err := ParseDocumentText("Packinglist\nBox no: 1 CC-Kar\n20 Cactus Gem 0,72 14,40\nPot Ø: 5,5 Cm Height: 5 Cm\fPackinglist\n8 Yucca 2,91 23,28\nPot Ø: 14 Cm Height: 60 Cm\nBox no: 2 CC-Kar\n6 Anthu Black Love Plastic Free 11,27 67,62\nPot Ø: 17 Cm Height: 60 Cm\nSubtotal product 105,30\nTotal Amount € 105,30")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Lines) != 3 || doc.Lines[0].LoadUnit != "1" || doc.Lines[1].LoadUnit != "1" || doc.Lines[2].LoadUnit != "2" {
		t.Fatalf("load units = %#v", doc.Lines)
	}
}

func TestOrderDetailRepairsLoadUnitsBeforeValidationSourceGuard(t *testing.T) {
	source, err := os.ReadFile("postgres.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (store *PostgresStore) OrderDetail")
	end := strings.Index(text[start:], "func (store *PostgresStore) loadOrderValidation")
	if start < 0 || end < 0 {
		t.Fatal("OrderDetail source not found")
	}
	body := text[start : start+end]
	if !strings.Contains(body, "repairStoredInvoiceLoadUnits(ctx, repairTx, orderID)") {
		t.Fatal("OrderDetail must repair invoice load units before validation")
	}
}
