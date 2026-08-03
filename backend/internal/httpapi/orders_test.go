package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
)

type profileAuthStub struct {
	authStub
	user *auth.User
}

func (stub profileAuthStub) UserByToken(context.Context, string) (*auth.User, error) {
	return stub.user, nil
}

type recordingOrderCreator struct {
	input order.CreateInput
}

func (creator *recordingOrderCreator) Create(
	_ context.Context,
	input order.CreateInput,
) (order.Created, error) {
	creator.input = input
	return order.Created{OrderNumber: "ZR-TEST"}, nil
}

func TestCreateOrderUsesAuthenticatedProfileDefaults(t *testing.T) {
	t.Parallel()

	authentication := profileAuthStub{user: &auth.User{
		ID:              42,
		Email:           "customer@example.com",
		Phone:           "+79156151100",
		FullName:        "Александр",
		LastName:        "Павловский",
		Patronymic:      "Владимирович",
		DeliveryAddress: "Рязань, Новосёлов, 40А",
	}}
	creator := &recordingOrderCreator{}
	dependencies := testDependencies(catalogStub{}, authentication)
	dependencies.OrderCreator = creator

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/orders",
		strings.NewReader(`{
			"customer":{"name":"","phone":"","email":"","address":"","comment":""},
			"delivery":"courier",
			"items":[{"id":"ficus","quantity":1}],
			"consent":true
		}`),
	)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "session-token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if creator.input.CustomerID == nil || *creator.input.CustomerID != 42 {
		t.Fatalf("customer ID = %#v", creator.input.CustomerID)
	}
	if creator.input.Customer.Name != "Павловский Александр Владимирович" {
		t.Fatalf("customer name = %q", creator.input.Customer.Name)
	}
	if creator.input.Customer.Phone != "+79156151100" {
		t.Fatalf("customer phone = %q", creator.input.Customer.Phone)
	}
	if creator.input.Customer.Email != "customer@example.com" {
		t.Fatalf("customer email = %q", creator.input.Customer.Email)
	}
	if creator.input.Customer.Address != "Рязань, Новосёлов, 40А" {
		t.Fatalf("customer address = %q", creator.input.Customer.Address)
	}
}

// The consent record is the only evidence of the agreement we would ever
// have, so an order without it must not be accepted.
func TestCreateOrderRequiresConsent(t *testing.T) {
	t.Parallel()

	creator := &recordingOrderCreator{}
	dependencies := testDependencies(catalogStub{}, authStub{})
	dependencies.OrderCreator = creator

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/orders",
		strings.NewReader(`{
			"customer":{"name":"Александр","phone":"+79156151100",
				"email":"customer@example.com","address":"Рязань","comment":""},
			"delivery":"courier",
			"items":[{"id":"ficus","quantity":1}]
		}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body)
	}
	if creator.input.Delivery != "" {
		t.Fatal("the order must not reach the service without consent")
	}
}
