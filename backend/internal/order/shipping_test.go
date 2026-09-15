package order

import "testing"

func TestCDEKStatusMappingUsesOnlyCustomerMilestones(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"RECEIVED_AT_SHIPMENT_WAREHOUSE": "shipped",
		"READY_FOR_SHIPMENT_IN_SENDER_CITY": "shipped",
		"TAKEN_BY_TRANSPORTER_FROM_SENDER_CITY": "shipped",
		"SENT_TO_RECIPIENT_CITY":          "shipped",
		"IN_TRANSIT":                     "shipped",
		"ACCEPTED_IN_TRANSIT_CITY":        "shipped",
		"RECEIVED_AT_DELIVERY_WAREHOUSE": "ready",
		"ACCEPTED_AT_PICK_UP_POINT":       "ready",
		"POSTOMAT_POSTED":                 "ready",
		"READY_FOR_RECIPIENT":            "ready",
		"DELIVERED":                      "completed",
	}
	for providerStatus, wanted := range tests {
		if got := cdekStatusToOrder[providerStatus]; got != wanted {
			t.Errorf("%s: got %q, want %q", providerStatus, got, wanted)
		}
	}
	if _, mapped := cdekStatusToOrder["CREATED"]; mapped {
		t.Fatal("internal CDEK state must not move the customer-facing order")
	}
	if _, mapped := cdekStatusToOrder["NOT_DELIVERED"]; mapped {
		t.Fatal("delivery exception needs manager review, not automatic cancellation")
	}
}
