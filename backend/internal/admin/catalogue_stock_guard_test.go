package admin

import (
	"context"
	"errors"
	"testing"
)

func TestCreateProductRejectsPositiveStockBeforeWriting(t *testing.T) {
	// A locally created product has no Saby identity. The repository has no DB
	// here so the validation must reject the request before opening a transaction.
	repository := &PostgresRepository{}
	_, err := repository.CreateProduct(context.Background(), Actor{Role: RoleOwner}, ProductCreate{
		Name: "Фикус", PriceMinor: 100000, Stock: 1,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("positive stock without Saby link: got %v, want ErrInvalidInput", err)
	}
}
