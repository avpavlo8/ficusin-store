package admin

import (
	"context"
	"errors"
	"testing"
)

func TestImportAllProductsRequiresProductEditPermission(t *testing.T) {
	t.Parallel()

	repository := &PostgresRepository{}
	_, err := repository.ImportAllProducts(context.Background(), Actor{}, true)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}
