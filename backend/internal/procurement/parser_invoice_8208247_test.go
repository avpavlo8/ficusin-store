package procurement

import "testing"

// Regression fixture copied from pdftotext -layout output of the real
// 11.09.2026 Gale CargoInt packing list 8208247 (PL-FG 267). Keep this test
// because this was the first production invoice uploaded into purchase #18
// after invoice reconciliation was introduced.
func TestParseRealHollandInvoice8208247(t *testing.T) {
	text := `Packinglist
Date: 11-09-2026 Client no.: EXT759 List no.: 8208247 Page: 1 of 4
Order: PL-FG 267 260911
Amount Description Price: Total:
Box no: 1 CC-Kar (+ 6 plaat) *imi*
14 Beaucarnea Recur Bol 3,45 48,30
Pot Ø: 6 Cm Height: 20 Cm
5 Bonsai Buxus Harlandii 21,15 105,75
Pot Ø: 20 Cm Height: 30 Cm
20 Bonsai Carmona Macrophylla In Ceramic 12,25 245,00
Pot Ø: 15 Cm Height: 30 Cm
10 Bonsai Ficus Retusa In Ceramic 12,25 122,50
Pot Ø: 15 Cm Height: 30 Cm
10 Bonsai Ligustrum Sinense In Ceramic 12,25 122,50
Pot Ø: 15 Cm Height: 30 Cm
8 Bonsai Rhododendrum Indicum 15,28 122,24
Pot Ø: 15 Cm Height: 25 Cm
20 Bonsai Sageretia Theezans In Ceramic 12,25 245,00
Pot Ø: 15 Cm Height: 30 Cm
12 Bonsai Zanthoxylum Piperitum 7,83 93,96
Pot Ø: 12 Cm Height: 22,5 Cm
20 Cactus Gem 0,72 14,40
Pot Ø: 5,5 Cm Height: 5 Cm
18 Citr Limetta 9,09 163,62
Pot Ø: 14 Cm Height: 35 Cm
12 Coffea Arabica 2,76 33,12
Pot Ø: 9 Cm Height: 15 Cm
30 Dra Lucky Bamboo Spiral Stam In Tube And Tray 1,60 48,00
Pot Ø: 5 Cm Height: 30 Cm
6 Fic Mi Ginseng 7,22 43,32
Pot Ø: 12 Cm Height: 30 Cm
Packinglist
Date: 11-09-2026 Client no.: EXT759 List no.: 8208247 Page: 2 of 4
Order: PL-FG 267 260911
Amount Description Price: Total:
21 Fitt Gem 3 Kl 0,97 20,37
Pot Ø: 8,5 Cm Height: 12,5 Cm
20 Nephr Ex Bos Bl Bell 2,76 55,20
Pot Ø: 12 Cm Minimum Plantdiameter: 30 Cm Transport height: 30 Cm
18 Olea Europaea 8,24 148,32
Pot Ø: 12 Cm Height: 25 Cm
24 Rhodo Si Vogel Gem 3,21 77,04
Pot Ø: 14 Cm Height: 25 Cm
16 Ro Mix Party 2,15 34,40
Pot Ø: 7 Cm Height: 20 Cm
18 Schlumb Gem 3 Kl 1,46 26,28
Pot Ø: 9 Cm Height: 18 Cm
24 Solan Pseudocapsic 1,58 37,92
Pot Ø: 10,5 Cm Height: 25 Cm
8 Yucca 2,91 23,28
Pot Ø: 14 Cm Height: 60 Cm
Box no: 2 CC-Kar (+ 3 plaat) *imi*
6 Anthu An Beauty Black 9,44 56,64
Pot Ø: 17 Cm Height: 50 Cm
6 Anthu An Melodia Ibis 10,97 65,82
Pot Ø: 17 Cm Height: 65 Cm
6 Anthu Black Love Plastic Free 11,27 67,62
Pot Ø: 17 Cm Height: 60 Cm
15 Arau Heterophylla 4,16 62,40
Pot Ø: 10 Cm Height: 25 Cm
7 Araucaria Heterophylla 7,83 54,81
Pot Ø: 14 Cm Height: 38 Cm
Packinglist
Date: 11-09-2026 Client no.: EXT759 List no.: 8208247 Page: 3 of 4
Order: PL-FG 267 260911
Amount Description Price: Total:
10 Aspidistra Elatior 11,82 118,20
Pot Ø: 13 Cm Height: 50 Cm
8 Bonsai Acer Pa Little Princess 15,28 122,24
Pot Ø: 15 Cm Height: 30 Cm
20 Bonsai Carmona Macrophylla In Ceramic 8,75 175,00
Pot Ø: 15 Cm Height: 25 Cm
20 Chamaed Elegans 2,16 43,20
Pot Ø: 12 Cm Height: 40 Cm
27 Citrof Microcarpa 10,63 287,01
Pot Ø: 12 Cm Height: 60 Cm
20 Cyrt Clivicola Fortunei Outside 1,93 38,60
Pot Ø: 8,5 Cm Height: 20 Cm
15 Lau Nobilis 2,76 41,40
Pot Ø: 11 Cm Height: 32 Cm
8 Livistona Rotundifol 6,63 53,04
Pot Ø: 14 Cm Height: 45 Cm
6 Nepent Loes Hang 10,26 61,56
Pot Ø: 15 Cm Height: 40 Cm
6 Nepent Mix Hang 10,26 61,56
Pot Ø: 15 Cm Height: 30 Cm
10 Nephr Ex Green Momen 1,94 19,40
Pot Ø: 12 Cm Minimum Plantdiameter: 30 Cm Transport height: 35 Cm
6 Olea Europaea 10,09 60,54
Pot Ø: 14 Cm Height: 60 Cm
20 Sansev Cy Twist 2,25 45,00
Pot Ø: 6 Cm Height: 12,5 Cm
Packinglist
Date: 11-09-2026 Client no.: EXT759 List no.: 8208247 Page: 4 of 4
Order: PL-FG 267 260911
Amount Description Price: Total:
8 Strelitzia Nicolai 5,35 42,80
Pot Ø: 13 Cm Height: 50 Cm
4 Xanthozoma Mint 9,32 37,28
Pot Ø: 14 Cm Height: 40 Cm
Quan Code Packing Fustprice Rent Total
1 CC3 CC-Kar (+ 3 plaat) *imi* 80,20 6,50 86,70
1 CC6 CC-Kar (+ 6 plaat) *imi* 106,90 9,35 116,25
Total colli/boxes loaded: 2
Subtotal product 3344,64
Returnable Package 187,10
Package Rent 15,85
Total Amount € 3.547,59
`
	result, err := ParseDocumentText(text)
	if err != nil {
		t.Fatal(err)
	}
	if result.ParserKind != "holland_packing_list" || result.DocumentNumber != "8208247" || result.Currency != "EUR" {
		t.Fatalf("unexpected header: %+v", result)
	}
	if len(result.Lines) != 41 {
		t.Fatalf("expected 41 product lines, got %d", len(result.Lines))
	}
	if units := countUnits(result.Lines); units != 562 {
		t.Fatalf("expected 562 units, got %d", units)
	}
	if !result.ArithmeticOK || !almostEqual(result.ProductSubtotal, 3344.64) || !almostEqual(result.CalculatedTotal, 3344.64) || !almostEqual(result.DocumentTotal, 3547.59) || !almostEqual(result.PackageTotal, 202.95) {
		t.Fatalf("unexpected totals: %+v", result)
	}
	if result.Lines[0].LoadUnit != "1" || result.Lines[21].LoadUnit != "2" || result.Lines[len(result.Lines)-1].LoadUnit != "2" {
		t.Fatalf("unexpected trolley allocation: first=%q second=%q last=%q", result.Lines[0].LoadUnit, result.Lines[21].LoadUnit, result.Lines[len(result.Lines)-1].LoadUnit)
	}
	if result.Lines[7].HeightCM == nil || !almostEqual(*result.Lines[7].HeightCM, 22.5) || result.Lines[8].PotDiameterCM == nil || !almostEqual(*result.Lines[8].PotDiameterCM, 5.5) {
		t.Fatalf("decimal dimensions were not parsed: %+v / %+v", result.Lines[7], result.Lines[8])
	}
}
