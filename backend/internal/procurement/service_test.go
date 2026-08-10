package procurement

import (
	"context"
	"errors"
	"testing"
)

type storeStub struct {
	supplierInput SupplierCreate
	orderInput    OrderCreate
}

func (stub *storeStub) Dashboard(context.Context) (Dashboard, error) { return Dashboard{}, nil }
func (stub *storeStub) CreateSupplier(_ context.Context, _ Actor, input SupplierCreate) (Supplier, error) {
	stub.supplierInput = input
	return Supplier{Name: input.Name, Kind: input.Kind, CountryCode: input.CountryCode, DefaultCurrency: input.DefaultCurrency}, nil
}
func (stub *storeStub) CreateOrder(_ context.Context, _ Actor, input OrderCreate) (OrderSummary, error) {
	stub.orderInput = input
	return OrderSummary{SupplierID: input.SupplierID, SourceKind: input.SourceKind, Currency: input.Currency}, nil
}

func TestCreateSupplierNormalizesInput(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	_, err := NewService(store).CreateSupplier(context.Background(), Actor{}, SupplierCreate{
		Name: "  ТК Ярославский ", Kind: KindDomestic, CountryCode: "ru", DefaultCurrency: "rub",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.supplierInput.Name != "ТК Ярославский" || store.supplierInput.CountryCode != "RU" || store.supplierInput.DefaultCurrency != "RUB" {
		t.Fatalf("unexpected normalized supplier: %+v", store.supplierInput)
	}
}

func TestCreateSupplierRejectsUnknownCurrency(t *testing.T) {
	t.Parallel()
	_, err := NewService(&storeStub{}).CreateSupplier(context.Background(), Actor{}, SupplierCreate{
		Name: "Поставщик", Kind: KindDomestic, DefaultCurrency: "CNY",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateOrderRequiresSupplier(t *testing.T) {
	t.Parallel()
	_, err := NewService(&storeStub{}).CreateOrder(context.Background(), Actor{}, OrderCreate{
		SourceKind: SourceManual, Currency: "EUR",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
