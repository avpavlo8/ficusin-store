package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/consent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CallChecker hands out a phone number for the end user to call from
// their own phone (SMS.ru's "user calls us" flow), and reports whether
// that call has come in yet. See integration.SMSRUClient, which is the
// production implementation.
type CallChecker interface {
	RequestCallCheck(ctx context.Context, phone string) (checkID, callPhone, callPhonePretty string, err error)
	CallCheckStatus(ctx context.Context, checkID string) (confirmed, expired bool, err error)
}

type Service struct {
	pool        *pgxpool.Pool
	sessionDays int
	callChecker CallChecker
	checkTTL    time.Duration
	resendAfter time.Duration
}

func NewService(pool *pgxpool.Pool, sessionDays int, callChecker CallChecker) *Service {
	return &Service{
		pool:        pool,
		sessionDays: sessionDays,
		callChecker: callChecker,
		checkTTL:    5 * time.Minute,
		resendAfter: 45 * time.Second,
	}
}

// RequestCall asks SMS.ru for a phone number the given (already
// normalized) phone number should call to log in or register, and
// remembers the resulting check so ConfirmCall can later verify it.
func (service *Service) RequestCall(ctx context.Context, phone string) (checkID, callPhone, callPhonePretty string, err error) {
	if phone == "" {
		return "", "", "", errors.New("phone is required")
	}

	var lastRequestedAt time.Time
	err = service.pool.QueryRow(ctx, `
		SELECT created_at FROM call_checks
			WHERE phone = $1
				ORDER BY created_at DESC
					LIMIT 1
	`, phone).Scan(&lastRequestedAt)
	if err != nil && !isNoRows(err) {
		return "", "", "", fmt.Errorf("check previous call: %w", err)
	}
	if err == nil && time.Since(lastRequestedAt) < service.resendAfter {
		return "", "", "", ErrRequestTooSoon
	}

	checkID, callPhone, callPhonePretty, err = service.callChecker.RequestCallCheck(ctx, phone)
	if err != nil {
		return "", "", "", fmt.Errorf("request call check: %w", err)
	}

	if _, err := service.pool.Exec(ctx, `
		INSERT INTO call_checks (phone, check_id, call_phone, call_phone_pretty, expires_at)
			VALUES ($1, $2, $3, $4, $5)
	`, phone, checkID, callPhone, callPhonePretty, time.Now().Add(service.checkTTL)); err != nil {
		return "", "", "", fmt.Errorf("store call check: %w", err)
	}

	return checkID, callPhone, callPhonePretty, nil
}

// ConfirmCall checks whether the user has called the number we gave them
// for checkID. If the call hasn't come in yet, it returns pending=true
// and no error — the caller should keep polling. Once SMS.ru confirms the
// call, it logs the user in (creating a new customer using registration
// if this phone hasn't signed in before).
func (service *Service) ConfirmCall(
	ctx context.Context,
	phone, checkID string,
	registration Registration,
	meta ClientMeta,
) (token string, expiresAt time.Time, pending bool, err error) {
	var storedCheckID string
	var consumedAt *time.Time
	var checkExpiresAt time.Time
	err = service.pool.QueryRow(ctx, `
		SELECT check_id, consumed_at, expires_at
			FROM call_checks
				WHERE phone = $1
					ORDER BY created_at DESC
						LIMIT 1
	`, phone).Scan(&storedCheckID, &consumedAt, &checkExpiresAt)
	if isNoRows(err) {
		return "", time.Time{}, false, ErrInvalidCode
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("load call check: %w", err)
	}
	if storedCheckID != checkID || consumedAt != nil || time.Now().After(checkExpiresAt) {
		return "", time.Time{}, false, ErrInvalidCode
	}

	confirmed, expired, err := service.callChecker.CallCheckStatus(ctx, checkID)
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("check call status: %w", err)
	}
	if expired {
		return "", time.Time{}, false, ErrInvalidCode
	}
	if !confirmed {
		return "", time.Time{}, true, nil
	}

	flow := registration.Flow
	if flow == "" {
		if registration.FullName != "" {
			flow = "register"
		} else {
			flow = "login"
		}
	}

	var customerID int64
	err = service.pool.QueryRow(ctx, `
		SELECT id FROM customers WHERE phone = $1 LIMIT 1
	`, phone).Scan(&customerID)
	if err != nil && !isNoRows(err) {
		return "", time.Time{}, false, fmt.Errorf("find customer: %w", err)
	}
	if isNoRows(err) {
		if flow != "register" {
			return "", time.Time{}, false, ErrAccountNotFound
		}
		customerID, err = service.createCustomer(ctx, phone, registration, meta)
		if err != nil {
			return "", time.Time{}, false, err
		}
	} else if flow == "register" {
		return "", time.Time{}, false, ErrAccountExists
	}

	if _, err := service.pool.Exec(ctx, `
		UPDATE call_checks SET consumed_at = CURRENT_TIMESTAMP WHERE check_id = $1
	`, checkID); err != nil {
		return "", time.Time{}, false, fmt.Errorf("consume call check: %w", err)
	}

	token, expiresAt, err = service.createSession(ctx, service.pool, customerID, meta.UserAgent)
	if err != nil {
		return "", time.Time{}, false, err
	}
	return token, expiresAt, false, nil
}

// createCustomer creates a new customer record the first time a phone
// number completes call verification. FullName and AccountType are
// required; an organization is only created when the customer registered
// as wholesale and actually supplied a company name and INN.
func (service *Service) createCustomer(
	ctx context.Context,
	phone string,
	registration Registration,
	meta ClientMeta,
) (int64, error) {
	if registration.FullName == "" ||
		(registration.AccountType != "retail" && registration.AccountType != "wholesale") {
		return 0, ErrRegistrationDetailsRequired
	}
	if !registration.Consent {
		return 0, ErrConsentRequired
	}

	wholesaleStatus := "not_requested"
	if registration.AccountType == "wholesale" {
		wholesaleStatus = "pending"
	}

	// The account and the evidence of the agreement behind it are written
	// together, so one can never exist without the other.
	transaction, err := service.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var customerID int64
	err = transaction.QueryRow(ctx, `
		INSERT INTO customers (
			email, phone, full_name, last_name, patronymic,
			delivery_address, account_type, wholesale_status, consent_at
		)
			VALUES (NULLIF($1, ''), $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)
			RETURNING id
	`,
		registration.Email,
		phone,
		registration.FullName,
		registration.LastName,
		registration.Patronymic,
		registration.DeliveryAddress,
		registration.AccountType,
		wholesaleStatus,
	).Scan(&customerID)
	if isUniqueViolation(err) {
		return 0, ErrAccountExists
	}
	if err != nil {
		return 0, fmt.Errorf("insert customer: %w", err)
	}
	if err := consent.Record(ctx, transaction, consent.Event{
		CustomerID: &customerID,
		Event:      consent.EventRegistration,
		Phone:      phone,
		IPAddress:  meta.IPAddress,
		UserAgent:  meta.UserAgent,
	}); err != nil {
		return 0, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit registration: %w", err)
	}

	if registration.AccountType == "wholesale" &&
		registration.CompanyName != "" && registration.INN != "" {
		var organizationID int64
		err = service.pool.QueryRow(ctx, `
			SELECT id FROM organizations WHERE inn = $1 LIMIT 1
		`, registration.INN).Scan(&organizationID)
		if err != nil && !isNoRows(err) {
			return 0, fmt.Errorf("find organization: %w", err)
		}
		if isNoRows(err) {
			err = service.pool.QueryRow(ctx, `
				INSERT INTO organizations (name, inn, kpp, legal_address)
					VALUES ($1, $2, NULLIF($3, ''), $4)
					RETURNING id
			`, registration.CompanyName, registration.INN, registration.KPP,
				registration.LegalAddress).Scan(&organizationID)
			if err != nil && !isUniqueViolation(err) {
				return 0, fmt.Errorf("insert organization: %w", err)
			}
		}
		if organizationID != 0 {
			if _, err := service.pool.Exec(ctx, `
				INSERT INTO organization_members (organization_id, customer_id, role)
					VALUES ($1, $2, 'buyer')
					ON CONFLICT DO NOTHING
			`, organizationID, customerID); err != nil {
				return 0, fmt.Errorf("insert organization member: %w", err)
			}
		}
	}

	return customerID, nil
}

// UpdateProfile saves the fields a customer may edit from their account
// page. Phone and account type stay read-only: the phone identifies the
// account, and switching to wholesale goes through the reviewed-details
// flow instead. The caller re-reads the profile afterwards so the response
// matches what /auth/me would return, admin role included.
func (service *Service) UpdateProfile(
	ctx context.Context,
	customerID int64,
	profile Profile,
) error {
	tag, err := service.pool.Exec(ctx, `
		UPDATE customers SET
			full_name = $2,
			last_name = $3,
			patronymic = $4,
			email = NULLIF($5, ''),
			delivery_address = $6,
			updated_at = CURRENT_TIMESTAMP
				WHERE id = $1 AND is_active = TRUE
	`,
		customerID,
		profile.FullName,
		profile.LastName,
		profile.Patronymic,
		profile.Email,
		profile.DeliveryAddress,
	)
	if isUniqueViolation(err) {
		return ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("update customer profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// SaveAvatar stores a profile picture. The browser downscales and
// re-encodes the file before sending it, so the bytes that arrive here are
// already small enough to keep in PostgreSQL alongside the customer row.
func (service *Service) SaveAvatar(
	ctx context.Context,
	customerID int64,
	image []byte,
	mime string,
) error {
	tag, err := service.pool.Exec(ctx, `
		UPDATE customers SET
			avatar_image = $2,
			avatar_mime = $3,
			avatar_updated_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
				WHERE id = $1 AND is_active = TRUE
	`, customerID, image, mime)
	if err != nil {
		return fmt.Errorf("save avatar: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (service *Service) DeleteAvatar(ctx context.Context, customerID int64) error {
	if _, err := service.pool.Exec(ctx, `
		UPDATE customers SET
			avatar_image = NULL,
			avatar_mime = NULL,
			avatar_updated_at = NULL,
			updated_at = CURRENT_TIMESTAMP
				WHERE id = $1
	`, customerID); err != nil {
		return fmt.Errorf("delete avatar: %w", err)
	}
	return nil
}

// Avatar returns the stored picture, or nil bytes when the customer never
// uploaded one.
func (service *Service) Avatar(ctx context.Context, customerID int64) ([]byte, string, error) {
	var image []byte
	var mime *string
	err := service.pool.QueryRow(ctx, `
		SELECT avatar_image, avatar_mime FROM customers WHERE id = $1
	`, customerID).Scan(&image, &mime)
	if isNoRows(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("load avatar: %w", err)
	}
	if mime == nil {
		return image, "", nil
	}
	return image, *mime, nil
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
			c.id, COALESCE(c.email, ''), c.phone, c.full_name, c.last_name, c.patronymic,
			c.delivery_address, c.account_type, c.wholesale_status,
			c.retail_discount_bps, c.lifetime_spend_minor,
			COALESCE((
				SELECT au.role
				FROM admin_users au
				WHERE au.is_active = TRUE AND au.customer_id = c.id
				LIMIT 1
			), ''),
			COALESCE(TO_CHAR(c.avatar_updated_at AT TIME ZONE 'UTC', 'YYYYMMDD"T"HH24MISS'), '')
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
		&user.LastName,
		&user.Patronymic,
		&user.DeliveryAddress,
		&user.AccountType,
		&user.WholesaleStatus,
		&user.RetailDiscountBPS,
		&user.LifetimeSpendMinor,
		&user.AdminRole,
		&user.AvatarUpdatedAt,
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
