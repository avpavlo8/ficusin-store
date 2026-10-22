package payment

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
)

// AuthorizeOrderPayment gives account owners access to their orders. A guest
// token authorizes payment only, never reading contacts or the full order.
func (service *Service) AuthorizeOrderPayment(ctx context.Context, number, token string, customerID int64) (bool, error) {
	var owner *int64
	var stored string
	err := service.pool.QueryRow(ctx, `SELECT customer_id, guest_payment_token_hash FROM orders WHERE order_number=$1`, number).Scan(&owner, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if owner != nil {
		return customerID > 0 && *owner == customerID, nil
	}
	if len(token) != 64 || len(stored) != 64 {
		return false, nil
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare([]byte(stored), []byte(hex.EncodeToString(sum[:]))) == 1, nil
}
