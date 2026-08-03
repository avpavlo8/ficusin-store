package admin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BootstrapOwners makes sure somebody can get into the admin panel on a
// fresh install. It matches the addresses in ADMIN_EMAILS against existing
// accounts and writes real admin_users rows for them.
//
// This runs only while the table holds no active administrator. Rights are
// granted from admin_users and nowhere else afterwards: an email address is
// never verified and the account owner can change theirs from the profile
// page, so leaving the match in place at request time would mean anyone who
// knows the address could claim the panel by registering with it.
func BootstrapOwners(
	ctx context.Context,
	pool *pgxpool.Pool,
	logger *slog.Logger,
	emails []string,
) error {
	normalized := make([]string, 0, len(emails))
	for _, email := range emails {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			normalized = append(normalized, email)
		}
	}
	if len(normalized) == 0 {
		return nil
	}

	var existing int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER FROM admin_users
		WHERE is_active = TRUE AND customer_id IS NOT NULL
	`).Scan(&existing); err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if existing > 0 {
		return nil
	}

	tag, err := pool.Exec(ctx, `
		INSERT INTO admin_users (customer_id, email, role, is_active, updated_at)
		SELECT c.id, c.email, $2, TRUE, CURRENT_TIMESTAMP
		FROM customers c
		WHERE c.email IS NOT NULL AND LOWER(c.email) = ANY($1)
		ON CONFLICT (customer_id) WHERE customer_id IS NOT NULL DO NOTHING
	`, normalized, RoleOwner)
	if err != nil {
		return fmt.Errorf("bootstrap administrators: %w", err)
	}
	if tag.RowsAffected() > 0 {
		logger.Info("granted the owner role from ADMIN_EMAILS", "accounts", tag.RowsAffected())
		return nil
	}
	logger.Warn(
		"no administrator exists yet and no account matches ADMIN_EMAILS; " +
			"register with one of those addresses and restart the app",
	)
	return nil
}
