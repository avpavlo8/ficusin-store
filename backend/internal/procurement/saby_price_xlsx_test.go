package procurement

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBuildSabyPriceXLSXUsesOfficialCodeAndHighestPrice(t *testing.T) {
	first, second, ignored := int64(2650), int64(2790), int64(9999)
	content, count, err := BuildSabyPriceXLSX([]OrderLine{
		{SabyID: "42", SabyCode: "X42", MatchStatus: "confirmed", ProposedRetailRUB: &first},
		{SabyID: "42", SabyCode: "X42", MatchStatus: "confirmed", ProposedRetailRUB: &second},
		{SabyID: "99", SabyCode: "X99", MatchStatus: "ignored", ProposedRetailRUB: &ignored},
	})
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	var sheet string
	for _, file := range reader.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		opened, openErr := file.Open()
		if openErr != nil { t.Fatal(openErr) }
		data, readErr := io.ReadAll(opened)
		_ = opened.Close()
		if readErr != nil { t.Fatal(readErr) }
		sheet = string(data)
	}
	for _, wanted := range []string{"Код", "Цена", "X42", "2790"} {
		if !strings.Contains(sheet, wanted) { t.Fatalf("sheet lacks %q: %s", wanted, sheet) }
	}
	if strings.Contains(sheet, "X99") { t.Fatalf("ignored product leaked into sheet: %s", sheet) }
}
