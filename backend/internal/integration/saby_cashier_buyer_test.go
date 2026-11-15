package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/cashierstock"
)

func TestCashierWebsiteBuyerAndReadableComment(t *testing.T) {
	h := newCashierHarness(t)
	request := cashierTestRequest()
	request.BuyerID = 18749
	progress := cashierDrive(t, h, request, cashierstock.Progress{})
	var state cashierReserveReceipt
	if json.Unmarshal(progress.Native, &state) != nil || state.Identity.BuyerID != 18749 {
		t.Fatal("buyer identity not retained")
	}
	buyer, err := cashierBuyer(h.header)
	if err != nil || buyer != 18749 || sabyRecordField(h.header, "ДокументРасширение.Название") != "Резерв заказа 0000-77" {
		t.Fatal("buyer/comment not saved", buyer, err)
	}
	if h.reserve[state.Rows[0].ID] != 1 || h.writes["Reserve.Apply"] != 1 {
		t.Fatal("native reserve missing")
	}
}

func TestCashierExistingReservePresentationDoesNotReplaceOrReleaseRows(t *testing.T) {
	h := newCashierHarness(t)
	old := cashierTestRequest()
	progress := cashierDrive(t, h, old, cashierstock.Progress{})
	var before cashierReserveReceipt
	json.Unmarshal(progress.Native, &before)
	next := old
	next.BuyerID = 18749
	progress = cashierDrive(t, h, next, progress)
	var after cashierReserveReceipt
	json.Unmarshal(progress.Native, &after)
	if before.Identity.ID != after.Identity.ID || before.Identity.UUID != after.Identity.UUID || !cashierSameRows(before.Rows, after.Rows) {
		t.Fatal("presentation changed invoice/row identity")
	}
	if after.Identity.BuyerID != 18749 || after.Marker != "Резерв заказа 0000-77" || after.RequestHash != cashierstock.Fingerprint(next) {
		t.Fatal("presentation not acknowledged")
	}
	for method, want := range map[string]int{"СчетСкИсх.Создать": 1, "СчетСкИсх.WriteForParticipant": 2, "СчетСкИсх.DocNomCreate": 1, "СчетСкИсх.CustomDocNomUpdate": 1, "Reserve.Apply": 1, "Reserve.Remove": 0, "СчетСкИсх.CustomDocNomDelete": 0} {
		if h.writes[method] != want {
			t.Fatalf("%s count %d, want %d", method, h.writes[method], want)
		}
	}
	if h.reserve[before.Rows[0].ID] != 1 {
		t.Fatal("existing native reserve lost")
	}
}

func TestCashierPresentationLostAcknowledgementOnlyReads(t *testing.T) {
	h := newCashierHarness(t)
	old := cashierTestRequest()
	progress := cashierDrive(t, h, old, cashierstock.Progress{})
	next := old
	next.BuyerID = 18749
	plan, err := h.provider.Next(context.Background(), next, progress)
	if err != nil || plan.Attention || plan.Command == nil || plan.Command.Kind != "header" {
		t.Fatal("presentation write not prepared", err, plan)
	}
	if _, err = h.provider.Execute(context.Background(), *plan.Command); err != nil {
		t.Fatal(err)
	}
	progress = cashierstock.Progress{Native: plan.Native, Command: plan.Command, Attempted: true, Error: "lost acknowledgement"}
	cashierDrive(t, h, next, progress)
	if h.writes["СчетСкИсх.WriteForParticipant"] != 2 || h.writes["Reserve.Apply"] != 1 || h.writes["Reserve.Remove"] != 0 {
		t.Fatal("lost acknowledgement repeated a write", h.writes)
	}
}

func TestCashierChangedBuyerStopsBeforeStockMutation(t *testing.T) {
	h := newCashierHarness(t)
	request := cashierTestRequest()
	request.BuyerID = 18749
	progress := cashierDrive(t, h, request, cashierstock.Progress{})
	setSabyRecordField(h.header, "Лицо1", int64(999))
	request.Lines[0].Quantity = 2
	plan, err := h.provider.Next(context.Background(), request, progress)
	if err != nil || !plan.Attention || plan.Command != nil || h.writes["Reserve.Remove"] != 0 {
		t.Fatal("changed buyer was ignored", err, plan)
	}
}
