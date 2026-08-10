package procurement

import "testing"

func TestParseHollandPackingList(t *testing.T) {
	text := `Packinglist
Date: 04-08-2026 Client no.: EXT759 List no.: 8207850 Page: 1 of 6
Amount Description Price: Total:
Box no: 1 CC-Kar (+ 5 plaat) *imi*
8 Acer Deshojo Bonsai 18,05 144,40
Pot Ø: 15 Cm Height: 30 Cm number of cuttings: 32
10 Aglao Green Mix 4,80 48,00
Pot Ø: 11 Cm Height: 30 Cm

Box no: 2 CC-Kar (+ 5 plaat) *imi*
10 Bonsai Ligustrum Sinense In Ceramic 12,25 122,50
Pot Ø: 15 Cm Height: 30 Cm
Quan Code Packing Fustprice Rent Total

1 CC5 CC-Kar (+ 5 plaat) *imi* 98,00 8,40 106,40
Subtotal product 314,90
Single Package 14,80
Returnable Package 196,00
Package Rent 16,80
Total Amount € 542,50`
	result, err := ParseDocumentText(text)
	if err != nil {
		t.Fatal(err)
	}
	if result.ParserKind != "holland_packing_list" || result.DocumentNumber != "8207850" || result.Currency != "EUR" {
		t.Fatalf("unexpected header: %+v", result)
	}
	if len(result.Lines) != 3 || result.Lines[0].LoadUnit != "1" || result.Lines[2].LoadUnit != "2" {
		t.Fatalf("unexpected lines: %+v", result.Lines)
	}
	if result.Lines[0].PotDiameterCM == nil || *result.Lines[0].PotDiameterCM != 15 || result.Lines[0].HeightCM == nil || *result.Lines[0].HeightCM != 30 {
		t.Fatalf("dimensions were not parsed: %+v", result.Lines[0])
	}
	if !result.ArithmeticOK || !almostEqual(result.PackageTotal, 227.6) {
		t.Fatalf("unexpected totals: %+v", result)
	}
}

func TestParseDomesticPaymentInvoice(t *testing.T) {
	text := `Счет на оплату № П3-11660 от 7 августа 2026 г.
№ Артикул Товары (работы, услуги) Количество Цена Ставка НДС Сумма НДС Сумма
1 Роза горшечная Кордана микс 44 шт 350,00 22% 2 777,05 15 400,00
2 Алоказия (микс, d 10) 33 шт 350,00 22% 2 082,79 11 550,00
3 Фикус Лирата d 10 22 шт 350,00 22% 1 388,52 7 700,00
Итого: 34 650,00
В т.ч. НДС (22%): 6 248,36`

	result, err := ParseDocumentText(text)
	if err != nil {
		t.Fatal(err)
	}
	if result.ParserKind != "domestic_payment_invoice" || result.DocumentNumber != "П3-11660" || result.Currency != "RUB" {
		t.Fatalf("unexpected header: %+v", result)
	}
	if len(result.Lines) != 3 || result.Lines[1].PotDiameterCM == nil || *result.Lines[1].PotDiameterCM != 10 {
		t.Fatalf("unexpected lines: %+v", result.Lines)
	}
	if !result.ArithmeticOK || result.DocumentTotal != 34650 {
		t.Fatalf("unexpected totals: %+v", result)
	}
}

func TestParserRejectsUnknownAndArithmeticMismatch(t *testing.T) {
	if _, err := ParseDocumentText("not a supported document"); err != ErrUnsupportedDocument {
		t.Fatalf("expected unsupported document, got %v", err)
	}
	result, err := ParseDocumentText(`Packinglist
Date: 04-08-2026 List no.: 1
Box no: 1
2 Ficus 10,00 25,00
Subtotal product 25,00
Total Amount € 25,00`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ArithmeticOK {
		t.Fatal("mismatched line arithmetic must require review")
	}
}
