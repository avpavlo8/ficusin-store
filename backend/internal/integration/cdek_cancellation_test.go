package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type carrierReply struct {
	method string
	status int
	body   string
}

func cancellationClient(t *testing.T, replies ...carrierReply) (*CDEKClient, *int) {
	t.Helper()
	calls := 0
	client := NewCDEKClient("client", "secret")
	client.token = "unit-test"
	client.tokenExpiry = time.Now().Add(time.Hour)
	client.httpClient = &http.Client{Transport: cdekRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if calls >= len(replies) {
			t.Fatalf("unexpected carrier request: %s %s", request.Method, request.URL.Path)
		}
		reply := replies[calls]
		calls++
		if request.Method != reply.method || request.URL.Path != "/v2/orders/known-uuid" {
			t.Fatalf("request %d: %s %s", calls, request.Method, request.URL.Path)
		}
		return &http.Response{StatusCode: reply.status, Body: io.NopCloser(strings.NewReader(reply.body)), Header: make(http.Header)}, nil
	})}
	return client, &calls
}

const activeCDEK = `{"entity":{"uuid":"known-uuid","statuses":[{"code":"CREATED","name":"Создан"}]}}`

func TestCDEKCancellationReconcilesOnlyConfirmedDeletion(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		replies         []carrierReply
		pending, failed bool
		code            string
	}{
		{name: "already removed", replies: []carrierReply{{"GET", 200, `{"entity":{"uuid":"known-uuid","statuses":[{"code":"REMOVED"}]}}`}}},
		{name: "documented deleted warning", replies: []carrierReply{{"GET", 400, `{"requests":[{"type":"GET","warnings":[{"code":"v2_entity_not_found","message":"Entity removed"}]}]}`}}},
		{name: "HTTP not found", replies: []carrierReply{{"GET", 404, `{}`}}},
		{name: "confirmed DELETE request", replies: []carrierReply{{"GET", 200, `{"entity":{"uuid":"known-uuid","statuses":[{"code":"CREATED"}]},"requests":[{"type":"DELETE","state":"SUCCESSFUL"}]}`}}},
		{name: "forbidden is not deletion", failed: true, code: "v2_entity_forbidden", replies: []carrierReply{{"GET", 400, `{"requests":[{"errors":[{"code":"v2_entity_forbidden","message":"Forbidden"}]}]}`}}},
		{name: "mixed not found and forbidden", failed: true, code: "v2_entity_forbidden", replies: []carrierReply{{"GET", 404, `{"errors":[{"code":"v2_entity_not_found"},{"code":"v2_entity_forbidden"}]}`}}},
		{name: "server failure is unknown", failed: true, replies: []carrierReply{{"GET", 503, `{"errors":[{"code":"v2_entity_not_found"}]}`}}},
		{name: "wrong UUID is not removal", failed: true, replies: []carrierReply{{"GET", 200, `{"entity":{"uuid":"different","statuses":[{"code":"REMOVED"}]}}`}}},
		{name: "accepted deletion needs confirmation", pending: true, replies: []carrierReply{{"GET", 200, activeCDEK}, {"DELETE", 202, `{"requests":[{"type":"DELETE","state":"ACCEPTED"}]}`}}},
		{name: "pending deletion is read only", pending: true, replies: []carrierReply{{"GET", 200, `{"entity":{"uuid":"known-uuid"},"requests":[{"type":"DELETE","state":"ACCEPTED"}]}`}}},
		{name: "rejected duplicate resolves by GET", replies: []carrierReply{{"GET", 200, activeCDEK}, {"DELETE", 400, `{"requests":[{"errors":[{"code":"error_validate_order_delete_status","message":"Cannot delete"}]}]}`}, {"GET", 400, `{"warnings":[{"code":"v2_entity_not_found"}]}`}}},
		{name: "handed off parcel remains failure", failed: true, code: "error_validate_order_delete_status", replies: []carrierReply{{"GET", 200, activeCDEK}, {"DELETE", 400, `{"requests":[{"errors":[{"code":"error_validate_order_delete_status","message":"Already handed off"}]}]}`}, {"GET", 200, activeCDEK}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client, calls := cancellationClient(t, scenario.replies...)
			err := client.CancelOrder(context.Background(), "known-uuid")
			if scenario.pending {
				if !errors.Is(err, ErrCDEKCancellationPending) {
					t.Fatalf("want pending, got %v", err)
				}
			} else if scenario.failed {
				if err == nil {
					t.Fatal("unconfirmed deletion accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if scenario.code != "" && !strings.Contains(err.Error(), scenario.code) {
				t.Fatalf("provider code lost: %v", err)
			}
			if *calls != len(scenario.replies) {
				t.Fatalf("calls=%d want %d", *calls, len(scenario.replies))
			}
		})
	}
}

func TestCDEKFetchUsesLatestDatedStatusAndIgnoresOldDeleteFailure(t *testing.T) {
	client, _ := cancellationClient(t, carrierReply{"GET", 200, `{"entity":{"uuid":"known-uuid","statuses":[{"code":"REMOVED","date_time":"2026-10-22T18:00:00+0300"},{"code":"CREATED","date_time":"2026-10-21T18:00:00+0300"}]},"requests":[{"type":"DELETE","state":"INVALID","errors":[{"code":"old_error"}]}]}`})
	shipment, err := client.FetchOrder(context.Background(), "known-uuid")
	if err != nil || shipment.Status != "REMOVED" {
		t.Fatalf("shipment=%+v error=%v", shipment, err)
	}
}
