package integration

import "testing"

func TestCombineParcelsStandsBoxesSideBySide(t *testing.T) {
	// The case the shop owner described: a pineapple and a monstera travel
	// next to each other, not stacked and not merged.
	pineapple := Parcel{LengthCM: 40, WidthCM: 20, HeightCM: 20, WeightGrams: 1200}
	monstera := Parcel{LengthCM: 60, WidthCM: 20, HeightCM: 20, WeightGrams: 2300}

	box := CombineParcels([]Parcel{pineapple, monstera})

	if box.LengthCM != 60 || box.WidthCM != 40 || box.HeightCM != 20 {
		t.Fatalf("expected 60x40x20, got %dx%dx%d", box.LengthCM, box.WidthCM, box.HeightCM)
	}
	if box.WeightGrams != 3500 {
		t.Fatalf("expected 3500 g, got %d", box.WeightGrams)
	}
}

func TestCombineParcelsLaysEachBoxOnItsLongestSide(t *testing.T) {
	// Same box, dimensions entered in a different order. A quote must not
	// depend on which field the manager typed the 60 into.
	upright := Parcel{LengthCM: 20, WidthCM: 60, HeightCM: 20, WeightGrams: 1000}
	flat := Parcel{LengthCM: 20, WidthCM: 20, HeightCM: 60, WeightGrams: 1000}

	first := CombineParcels([]Parcel{upright})
	second := CombineParcels([]Parcel{flat})

	if first != second {
		t.Fatalf("orientation changed the box: %+v vs %+v", first, second)
	}
	if first.LengthCM != 60 || first.WidthCM != 20 || first.HeightCM != 20 {
		t.Fatalf("expected 60x20x20, got %dx%dx%d", first.LengthCM, first.WidthCM, first.HeightCM)
	}
}

func TestCombineParcelsFallsBackWhenNothingIsFilledIn(t *testing.T) {
	// Products imported from Saby arrive without dimensions. A quote still
	// has to come out, and it must not be a zero-sized box.
	box := CombineParcels([]Parcel{{}, {}})

	if box != DefaultParcel {
		t.Fatalf("expected the fallback box, got %+v", box)
	}
}

func TestCombineParcelsKeepsMeasuredItemsWhenOneIsMissing(t *testing.T) {
	measured := Parcel{LengthCM: 50, WidthCM: 30, HeightCM: 30, WeightGrams: 2000}

	box := CombineParcels([]Parcel{measured, {}})

	if box.LengthCM != 50 || box.WidthCM != 30 || box.HeightCM != 30 {
		t.Fatalf("empty parcel changed the box: %dx%dx%d", box.LengthCM, box.WidthCM, box.HeightCM)
	}
}
