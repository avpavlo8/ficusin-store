package admin

import (
	"bytes"
	"os"
	"testing"
)

func TestReturnLabelsPDF(t *testing.T) {
	var font string
	for _, path := range []string{os.Getenv("PROCUREMENT_PDF_FONT"), "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "/System/Library/Fonts/Supplemental/Arial.ttf"} {
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				font = path
				break
			}
		}
	}
	if font == "" {
		t.Skip("no PDF TTF font installed")
	}
	t.Setenv("PROCUREMENT_PDF_FONT", font)
	labels := []MarketplaceReturn{{ProductName: "Аглаонема Вайт Ленс", Label: "D12", Article: "RET-000001", ReturnedAt: "2026-10-14", Price: 1000, DiscountPercent: 30, DiscountedPrice: 700}, {ProductName: "Филодендрон Пинк Принцесс Вариегатный", Label: "D17", Article: "RET-000002", ReturnedAt: "2026-10-14", Price: 2500, DiscountPercent: 20, DiscountedPrice: 2000}}
	content, err := BuildReturnLabels(labels, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		t.Fatal("invalid PDF")
	}
	if output := os.Getenv("RETURN_LABEL_TEST_OUTPUT"); output != "" {
		if err = os.WriteFile(output, content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = BuildReturnLabels(nil, true); err == nil {
		t.Fatal("empty labels accepted")
	}
}
