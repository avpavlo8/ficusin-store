package ordersale

import "testing"

func TestRegulationAmendmentCannotChangeOtherRequestFields(t *testing.T) {
	before, _ := liveStockProofFixture()
	effective := before
	effective.RegulationID++
	if !onlyRegulationDiff(before, effective) {
		t.Fatal("regulation-only correction rejected")
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.BuyerID++ }, func(r *Request) { r.NativeWarehouseID++ }, func(r *Request) { r.SiteWarehouseID++ },
		func(r *Request) { r.MovementWarehouseID++ }, func(r *Request) { r.OrganizationID++ }, func(r *Request) { r.IntentID++ },
		func(r *Request) { r.OperationKey = "ffffffff-ffff-4fff-8fff-ffffffffffff" }, func(r *Request) { r.Event.OrderID++ },
		func(r *Request) { r.Event.Lines = append([]Line(nil), r.Event.Lines...); r.Event.Lines[0].Quantity++ },
		func(r *Request) {
			r.Event.Lines = append([]Line(nil), r.Event.Lines...)
			r.Event.Lines[0].UnitPrice = "0.01"
		},
	} {
		candidate := effective
		change(&candidate)
		if onlyRegulationDiff(before, candidate) {
			t.Fatal("configuration approval could retarget document or goods", candidate)
		}
	}
}
