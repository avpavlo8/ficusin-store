package admin

import "testing"

func TestPackingBoxesCannotExceedOrderPositions(t *testing.T) {
	box := func(id int64) ShipmentBoxInput {
		return ShipmentBoxInput{LengthCM: 20, WidthCM: 20, HeightCM: 30, WeightGrams: 1000, Contents: []ShipmentBoxContent{{OrderItemID: id, Quantity: 1}}}
	}
	single := []ShipmentOfferLineInput{{OrderItemID: 1, Quantity: 2}}
	if err := validatePackingSelection([]ShipmentBoxInput{box(1), box(1)}, single); err == nil {
		t.Fatal("accepted two boxes for one order position")
	}
	both := []ShipmentOfferLineInput{{OrderItemID: 1, Quantity: 1}, {OrderItemID: 2, Quantity: 1}}
	if err := validatePackingSelection([]ShipmentBoxInput{box(1), box(2)}, both); err != nil {
		t.Fatalf("rejected one box per position: %v", err)
	}
}
