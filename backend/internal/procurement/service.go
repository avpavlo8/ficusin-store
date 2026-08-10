package procurement

import (
	"context"
	"strings"
)

type Store interface {
	Dashboard(context.Context) (Dashboard, error)
	CreateSupplier(context.Context, Actor, SupplierCreate) (Supplier, error)
	CreateOrder(context.Context, Actor, OrderCreate) (OrderSummary, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	return service.store.Dashboard(ctx)
}

func (service *Service) CreateSupplier(
	ctx context.Context,
	actor Actor,
	input SupplierCreate,
) (Supplier, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	input.CountryCode = strings.ToUpper(strings.TrimSpace(input.CountryCode))
	input.DefaultCurrency = strings.ToUpper(strings.TrimSpace(input.DefaultCurrency))
	if input.Name == "" || !oneOf(input.Kind, KindInternational, KindDomestic) ||
		!oneOf(input.DefaultCurrency, "EUR", "USD", "RUB") || len(input.CountryCode) > 2 {
		return Supplier{}, ErrInvalidInput
	}
	return service.store.CreateSupplier(ctx, actor, input)
}

func (service *Service) CreateOrder(
	ctx context.Context,
	actor Actor,
	input OrderCreate,
) (OrderSummary, error) {
	input.OrderNumber = strings.TrimSpace(input.OrderNumber)
	input.SourceKind = strings.TrimSpace(input.SourceKind)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Notes = strings.TrimSpace(input.Notes)
	if input.SupplierID <= 0 || !oneOf(input.SourceKind,
		SourceRecommendation, SourceManual, SourceInvoice, SourcePaymentInvoice,
	) || !oneOf(input.Currency, "EUR", "USD", "RUB") {
		return OrderSummary{}, ErrInvalidInput
	}
	return service.store.CreateOrder(ctx, actor, input)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
