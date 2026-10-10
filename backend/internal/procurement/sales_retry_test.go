package procurement

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetrySalesTransactionAfterDeadlock(t *testing.T) {
	attempts := 0
	count, err := retrySalesTransaction(context.Background(), func() (int, error) {
		attempts++
		if attempts == 1 {
			return 0, errors.Join(errors.New("insert sales event"), &pgconn.PgError{Code: "40P01"})
		}
		return 7, nil
	})
	if err != nil || count != 7 || attempts != 2 {
		t.Fatalf("count=%d err=%v attempts=%d", count, err, attempts)
	}
}

func TestRetrySalesTransactionDoesNotReplayOtherErrors(t *testing.T) {
	attempts := 0
	_, err := retrySalesTransaction(context.Background(), func() (int, error) {
		attempts++
		return 0, &pgconn.PgError{Code: "23505"}
	})
	if err == nil || attempts != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}
