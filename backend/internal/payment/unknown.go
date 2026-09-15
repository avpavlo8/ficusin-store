package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
)

// YooKassa guarantees idempotence for 24 hours. Stop automatic POST retries
// one hour earlier so clock skew and a slow request cannot cross that border.
const automaticRecoveryWindow = 23 * time.Hour

var ErrPaymentNeedsReview = errors.New("результат платежа нужно проверить вручную в ЮKassa")

type Issue struct {
	ID               int64      `json:"id"`
	Amount           float64    `json:"amount"`
	CreatedAt        time.Time  `json:"createdAt"`
	RecoveryDeadline time.Time  `json:"recoveryDeadline"`
	Attempts         int        `json:"attempts"`
	LastAttemptAt    *time.Time `json:"lastAttemptAt,omitempty"`
	LastError        string     `json:"lastError"`
	NeedsReview      bool       `json:"needsReview"`
}

func paymentRetryDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return time.Minute
	case attempt == 2:
		return 2 * time.Minute
	case attempt == 3:
		return 5 * time.Minute
	case attempt == 4:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// attemptPaymentCreation persists and then reuses the byte-equivalent logical
// request. A timeout is left pending: it is an unknown outcome, not a failure.
func (service *Service) attemptPaymentCreation(ctx context.Context, paymentID int64, proposed integration.PaymentRequest) (integration.Payment, error) {
	var key string
	var createdAt time.Time
	var stored []byte

	if proposed.Amount > 0 {
		if proposed.Metadata == nil {
			proposed.Metadata = map[string]string{}
		}
		proposed.Metadata["ficusin_payment_id"] = strconv.FormatInt(paymentID, 10)
		encoded, err := json.Marshal(proposed)
		if err != nil {
			return integration.Payment{}, fmt.Errorf("encode payment request: %w", err)
		}
		if _, err := service.pool.Exec(ctx, `
			UPDATE payments
			SET request_payload=COALESCE(request_payload,$2::JSONB),
				next_recovery_at=COALESCE(next_recovery_at,CURRENT_TIMESTAMP),
				updated_at=CURRENT_TIMESTAMP
			WHERE id=$1 AND status='pending' AND provider_payment_id=''
		`, paymentID, encoded); err != nil {
			return integration.Payment{}, fmt.Errorf("store payment recovery request: %w", err)
		}
	}
	if err := service.pool.QueryRow(ctx, `
		SELECT idempotence_key,created_at,request_payload
		FROM payments WHERE id=$1 AND status='pending' AND provider_payment_id=''
	`, paymentID).Scan(&key, &createdAt, &stored); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return integration.Payment{}, errors.New("платёж уже обработан")
		}
		return integration.Payment{}, err
	}
	var request integration.PaymentRequest
	if len(stored) == 0 || string(stored) == "null" {
		return integration.Payment{}, ErrPaymentNeedsReview
	}
	if err := json.Unmarshal(stored, &request); err != nil {
		return integration.Payment{}, fmt.Errorf("decode payment recovery request: %w", err)
	}
	request.IdempotenceKey = key
	if !time.Now().Before(createdAt.Add(automaticRecoveryWindow)) {
		_, _ = service.pool.Exec(ctx, `
			UPDATE payments SET next_recovery_at=NULL,recovery_locked_until=NULL,
				last_error=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1
		`, paymentID, ErrPaymentNeedsReview.Error())
		return integration.Payment{}, ErrPaymentNeedsReview
	}

	var attempt int
	if err := service.pool.QueryRow(ctx, `
		UPDATE payments SET create_attempts=create_attempts+1,
			last_create_attempt_at=CURRENT_TIMESTAMP,recovery_locked_until=NULL,
			updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 RETURNING create_attempts
	`, paymentID).Scan(&attempt); err != nil {
		return integration.Payment{}, err
	}
	created, err := service.provider.CreatePayment(ctx, request)
	if err != nil {
		if !errors.Is(err,integration.ErrPaymentOutcomeUnknown) {
			var offerID *int64
			_ = service.pool.QueryRow(ctx,`SELECT shipment_offer_id FROM payments WHERE id=$1`,paymentID).Scan(&offerID)
			_,_ = service.pool.Exec(ctx,`UPDATE payments SET status='cancelled',last_error=$2,next_recovery_at=NULL,recovery_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,paymentID,err.Error())
			if offerID!=nil {
				if releaseErr:=service.releaseOfferCheckoutReservation(ctx,*offerID);releaseErr!=nil{return integration.Payment{},releaseErr}
				_,_ = service.pool.Exec(ctx,`UPDATE shipment_offers SET status=CASE WHEN expires_at<=CURRENT_TIMESTAMP THEN 'expired' ELSE 'offered' END,checkout_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,*offerID)
			}
			return integration.Payment{},err
		}
		next := time.Now().Add(paymentRetryDelay(attempt))
		if !next.Before(createdAt.Add(automaticRecoveryWindow)) {
			next = createdAt.Add(automaticRecoveryWindow)
		}
		_, _ = service.pool.Exec(ctx, `
			UPDATE payments SET last_error=$2,next_recovery_at=$3,
				recovery_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1
		`, paymentID, err.Error(), next)
		return integration.Payment{}, fmt.Errorf("результат создания оплаты не подтверждён; повтор будет выполнен с тем же ключом: %w", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		return integration.Payment{}, errors.New("ЮKassa не вернула идентификатор платежа")
	}
	if cents(created.Amount) != cents(request.Amount) {
		return integration.Payment{}, errors.New("ЮKassa вернула платёж с другой суммой")
	}
	status := strings.TrimSpace(created.Status)
	if status == "" {
		status = StatusPending
	}
	if _, err := service.pool.Exec(ctx, `
		UPDATE payments SET provider_payment_id=$2,status=$3,confirmation_url=$4,
			last_error='',next_recovery_at=NULL,recovery_locked_until=NULL,
			updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND provider_payment_id=''
	`, paymentID, created.ID, status, created.ConfirmationURL); err != nil {
		return integration.Payment{}, fmt.Errorf("save recovered payment: %w", err)
	}
	return created, nil
}

func (service *Service) claimUnknownPayments(ctx context.Context, limit int) ([]int64, error) {
	rows, err := service.pool.Query(ctx, `
		WITH candidates AS (
			SELECT id FROM payments
			WHERE status='pending' AND provider_payment_id='' AND request_payload IS NOT NULL
				AND created_at>CURRENT_TIMESTAMP-$1::INTERVAL
				AND COALESCE(next_recovery_at,CURRENT_TIMESTAMP)<=CURRENT_TIMESTAMP
				AND COALESCE(recovery_locked_until,'-infinity'::TIMESTAMPTZ)<=CURRENT_TIMESTAMP
			ORDER BY next_recovery_at NULLS FIRST,id
			FOR UPDATE SKIP LOCKED LIMIT $2
		)
		UPDATE payments p SET recovery_locked_until=CURRENT_TIMESTAMP+INTERVAL '2 minutes'
		FROM candidates c WHERE p.id=c.id RETURNING p.id
	`, automaticRecoveryWindow.String(), limit)
	if err != nil { return nil, err }
	defer rows.Close()
	ids := []int64{}
	for rows.Next() { var id int64; if err := rows.Scan(&id); err != nil { return nil, err }; ids = append(ids,id) }
	return ids, rows.Err()
}

func (service *Service) RecoverUnknown(ctx context.Context, paymentID int64) (string, error) {
	created, err := service.attemptPaymentCreation(ctx, paymentID, integration.PaymentRequest{})
	if err != nil {
		return "", err
	}
	return created.ConfirmationURL, nil
}

func (service *Service) RecoverUnknownForOrder(ctx context.Context, orderID, paymentID int64) (string, error) {
	var belongs bool
	if err := service.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE id=$1 AND order_id=$2)`,paymentID,orderID).Scan(&belongs); err != nil { return "",err }
	if !belongs { return "",errors.New("попытка оплаты не относится к этому заказу") }
	return service.RecoverUnknown(ctx,paymentID)
}

func (service *Service) adoptUnknownFromMetadata(ctx context.Context, payment integration.Payment) (int64,int64,*int64,float64,bool,error) {
	localID,err:=strconv.ParseInt(payment.Metadata["ficusin_payment_id"],10,64)
	if err!=nil||localID<=0{return 0,0,nil,0,false,nil}
	var orderID int64;var offerID *int64;var expected float64
	err=service.pool.QueryRow(ctx,`SELECT order_id,shipment_offer_id,amount::DOUBLE PRECISION FROM payments WHERE id=$1 AND status='pending' AND provider_payment_id=''`,localID).Scan(&orderID,&offerID,&expected)
	if errors.Is(err,pgx.ErrNoRows){return 0,0,nil,0,false,nil}
	if err!=nil{return 0,0,nil,0,false,err}
	if cents(payment.Amount)!=cents(expected){return 0,0,nil,0,false,errors.New("ЮKassa вернула неизвестный платёж с другой суммой")}
	command,err:=service.pool.Exec(ctx,`UPDATE payments SET provider_payment_id=$2,last_error='',next_recovery_at=NULL,recovery_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND provider_payment_id=''`,localID,payment.ID)
	if err!=nil{return 0,0,nil,0,false,err}
	if command.RowsAffected()!=1{return 0,0,nil,0,false,nil}
	return localID,orderID,offerID,expected,true,nil
}

func (service *Service) paymentIssues(ctx context.Context, orderID int64) ([]Issue, error) {
	rows, err := service.pool.Query(ctx, `
		SELECT id,amount::DOUBLE PRECISION,created_at,create_attempts,
			last_create_attempt_at,last_error,
			(request_payload IS NULL OR CURRENT_TIMESTAMP>=created_at+$2::INTERVAL)
		FROM payments
		WHERE order_id=$1 AND status='pending' AND provider_payment_id=''
		ORDER BY id DESC
	`, orderID, automaticRecoveryWindow.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	issues := []Issue{}
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.ID, &issue.Amount, &issue.CreatedAt, &issue.Attempts, &issue.LastAttemptAt, &issue.LastError, &issue.NeedsReview); err != nil {
			return nil, err
		}
		issue.RecoveryDeadline = issue.CreatedAt.Add(automaticRecoveryWindow)
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// ResolveUnknown attaches a provider object only after checking its identity
// and exact amount. This is the operator escape hatch after automatic retries
// become unsafe.
func (service *Service) ResolveUnknown(ctx context.Context, orderID, paymentID int64, providerPaymentID string) (Balance, error) {
	providerPaymentID = strings.TrimSpace(providerPaymentID)
	if providerPaymentID == "" {
		return Balance{}, errors.New("укажите ID платежа из ЮKassa")
	}
	var expected float64
	var orderNumber string
	if err := service.pool.QueryRow(ctx, `
		SELECT p.amount::DOUBLE PRECISION,o.order_number
		FROM payments p JOIN orders o ON o.id=p.order_id
		WHERE p.id=$1 AND p.order_id=$2 AND p.status='pending' AND p.provider_payment_id=''
	`, paymentID, orderID).Scan(&expected, &orderNumber); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Balance{}, errors.New("неизвестная попытка оплаты не найдена")
		}
		return Balance{}, err
	}
	providerPayment, err := service.provider.FetchPayment(ctx, providerPaymentID)
	if err != nil {
		return Balance{}, err
	}
	if providerPayment.ID != providerPaymentID || cents(providerPayment.Amount) != cents(expected) {
		return Balance{}, errors.New("ID относится к платежу с другой суммой")
	}
	metadataMatches := providerPayment.Metadata["ficusin_payment_id"] == strconv.FormatInt(paymentID, 10)
	descriptionMatches := strings.Contains(providerPayment.Description, orderNumber)
	if !metadataMatches && !descriptionMatches {
		return Balance{}, errors.New("платёж не относится к этому заказу")
	}
	command, err := service.pool.Exec(ctx, `
		UPDATE payments SET provider_payment_id=$3,last_error='',next_recovery_at=NULL,
			recovery_locked_until=NULL,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND order_id=$2 AND status='pending' AND provider_payment_id=''
	`, paymentID, orderID, providerPaymentID)
	if err != nil {
		return Balance{}, err
	}
	if command.RowsAffected() != 1 {
		return Balance{}, errors.New("попытка оплаты уже была обработана")
	}
	if err := service.SyncOutstanding(ctx, providerPaymentID); err != nil {
		return Balance{}, err
	}
	return service.BalanceForOrder(ctx, orderID)
}

// DismissUnknown is deliberately manual and only available after automatic
// recovery has stopped. The operator has checked YooKassa and confirmed that
// no provider object exists; only then may a new payment attempt be opened.
func (service *Service) DismissUnknown(ctx context.Context, orderID, paymentID int64) (Balance,error) {
	var offerID *int64
	err:=service.pool.QueryRow(ctx,`
		UPDATE payments SET status='cancelled',last_error='Оператор подтвердил отсутствие платежа в ЮKassa',
			next_recovery_at=NULL,recovery_locked_until=NULL,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND order_id=$2 AND status='pending' AND provider_payment_id=''
			AND created_at+$3::INTERVAL<=CURRENT_TIMESTAMP
		RETURNING shipment_offer_id
	`,paymentID,orderID,automaticRecoveryWindow.String()).Scan(&offerID)
	if errors.Is(err,pgx.ErrNoRows){return Balance{},errors.New("попытку ещё нельзя закрыть: сначала дождитесь окончания безопасной автопроверки")}
	if err!=nil{return Balance{},err}
	if offerID!=nil {
		if err:=service.releaseOfferCheckoutReservation(ctx,*offerID);err!=nil{return Balance{},err}
		_,err=service.pool.Exec(ctx,`UPDATE shipment_offers SET status=CASE WHEN expires_at<=CURRENT_TIMESTAMP THEN 'expired' ELSE 'offered' END,checkout_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,*offerID)
		if err!=nil{return Balance{},err}
	}
	return service.BalanceForOrder(ctx,orderID)
}
