package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool        *pgxpool.Pool
	sessionDays int
}

func NewService(pool *pgxpool.Pool, sessionDays int) *Service {
	return &Service{pool: pool, sessionDays: sessionDays}
}

func (service *Service) Register(
	ctx context.Context,
	input Registration,
	userAgent string,
) (string, time.Time, error) {
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return "", time.Time{}, err
	}

	transaction, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("begin registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	wholesaleStatus := "not_requested"
	if input.AccountType == "wholesale" {
		wholesaleStatus = "pending"
	}

	var customerID int64
	err = transaction.QueryRow(ctx, `
		INSERT INTO customers (
			email, phone, password_hash, full_name, account_type,
			wholesale_status, consent_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		RETURNING id
	`, input.Email, input.Phone, passwordHash, input.FullName, input.AccountType, wholesaleStatus).
		Scan(&customerID)
	if isUniqueViolation(err) {
		return "", time.Time{}, ErrAccountExists
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert customer: %w", err)
	}

	if input.AccountType == "wholesale" {
		var organizationID int64
		err = transaction.QueryRow(
			ctx,
			"SELECT id FROM organizations WHERE inn = $1 LIMIT 1",
			input.INN,
		).Scan(&organizationID)
		if err != nil && !isNoRows(err) {
			return "", time.Time{}, fmt.Errorf("find organization: %w", err)
		}
		if isNoRows(err) {
			err = transaction.QueryRow(ctx, `
				INSERT INTO organizations (name, inn, kpp, legal_address)
				VALUES ($1, $2, NULLIF($3, ''), $4)
				RETURNING id
			`, input.CompanyName, input.INN, input.KPP, input.LegalAddress).
				Scan(&organizationID)
			if isUniqueViolation(err) {
				return "", time.Time{}, ErrAccountExists
			}
			if err != nil {
				return "", time.Time{}, fmt.Errorf("insert organization: %w", err)
			}
		}

		if _, err := transaction.Exec(ctx, `
			INSERT INTO organization_members (organization_id, customer_id, role)
			VALUES ($1, $2, 'buyer')
		`, organizationID, customerID); err != nil {
			return "", time.Time{}, fmt.Errorf("insert organization member: %w", err)
		}
	}

	token, expiresAt, err := service.createSession(ctx, transaction, customerID, userAgent)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("commit registration: %w", err)
	}
	return token, expiresAt, nil
}

func (service *Service) Login(
	ctx context.Context,
	identifier, password, userAgent string,
) (string, time.Time, error) {
	phone := NormalizeRussianPhone(identifier)
	email := strings.ToLower(strings.TrimSpace(identifier))

	var customerID int64
	var passwordHash string
	var active bool
	if phone != "" {
		err := service.pool.QueryRow(ctx, `
			SELECT id, password_hash, is_active
			FROM customers WHERE phone = $1 LIMIT 1
		`, phone).Scan(&customerID, &passwordHash, &active)
		if err != nil {
			return "", time.Time{}, credentialsError(err)
		}
	} else {
		err := service.pool.QueryRow(ctx, `
			SELECT id, password_hash, is_active
			FROM customers WHERE LOWER(email) = $1 LIMIT 1
		`, email).Scan(&customerID, &passwordHash, &active)
		if err != nil {
			return "", time.Time{}, credentialsError(err)
		}
	}

	if !active || !VerifyPassword(password, passwordHash) {
		return "", time.Time{}, ErrInvalidCredentials
	}

	return service.createSession(ctx, service.pool, customerID, userAgent)
}

func (service *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if _, err := service.pool.Exec(
		ctx,
		"DELETE FROM auth_sessions WHERE token_hash = $1",
		hashToken(rawToken),
	); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (service *Service) UserByToken(ctx context.Context, rawToken string) (*User, error) {
	if rawToken == "" {
		return nil, nil
	}

	var user User
	err := service.pool.QueryRow(ctx, `
		SELECT
			c.id, c.email, c.phone, c.full_name, c.account_type,
			c.wholesale_status, c.retail_discount_bps
		FROM auth_sessions s
		JOIN customers c ON c.id = s.customer_id
		WHERE s.token_hash = $1
			AND s.expires_at > CURRENT_TIMESTAMP
			AND c.is_active = TRUE
		LIMIT 1
	`, hashToken(rawToken)).Scan(
		&user.ID,
		&user.Email,
		&user.Phone,
		&user.FullName,
		&user.AccountType,
		&user.WholesaleStatus,
		&user.RetailDiscountBPS,
	)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find session user: %w", err)
	}
	return &user, nil
}

type sessionExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (service *Service) createSession(
	ctx context.Context,
	executor sessionExecutor,
	customerID int64,
	userAgent string,
) (string, time.Time, error) {
	randomToken := make([]byte, 32)
	if _, err := rand.Read(randomToken); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(randomToken)
	expiresAt := time.Now().Add(time.Duration(service.sessionDays) * 24 * time.Hour)
	if _, err := executor.Exec(ctx, `
		INSERT INTO auth_sessions (token_hash, customer_id, expires_at, user_agent)
		VALUES ($1, $2, $3, NULLIF($4, ''))
	`, hashToken(rawToken), customerID, expiresAt, truncate(userAgent, 500)); err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}
	return rawToken, expiresAt, nil
}

func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func credentialsError(err error) error {
	if isNoRows(err) {
		return ErrInvalidCredentials
	}
	return fmt.Errorf("find login customer: %w", err)
}

func isNoRows(err error) bool {
	return err == pgx.ErrNoRows
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return err != nil &&
		errors.As(err, &postgresError) &&
		postgresError.Code == "23505"
}

func truncate(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
