package admin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/jackc/pgx/v5"
)

type OrderCDEKState struct {
	Revision         int    `json:"revision"`
	TrackNumber      string `json:"trackNumber"`
	ManualTrack      bool   `json:"manualTrack"`
	Error            string `json:"error"`
	Pending          bool   `json:"pending"`
	CanRetry         bool   `json:"canRetry"`
	RetryUnavailable string `json:"retryUnavailable"`
	CanSetTracking   bool   `json:"canSetTracking"`
}

type OrderCDEKInput struct {
	Revision    int    `json:"revision"`
	Action      string `json:"action"`
	TrackNumber string `json:"trackNumber"`
}

type orderCDEKData struct {
	revision, tariff                           int
	delivery, status, payee, state, uuid       string
	carrierStatus, reason, lastError, track    string
	paymentStatus                              string
	feePending, preorder, requested, reopening bool
	hasOffers                                  bool
}

var (
	ErrOrderCDEKInput    = errors.New("Проверьте действие и трек-номер: допустимы до 64 латинских букв, цифр и дефисов")
	ErrOrderCDEKConflict = errors.New("Данные отправления изменились. Обновите заказ и проверьте статус СДЭК")
	trackingNumberFormat = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)
)

func loadOrderCDEK(ctx context.Context, reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64, lock bool) (orderCDEKData, error) {
	query := `SELECT shipment_revision,COALESCE(cdek_tariff_code,0),delivery_method,status,delivery_payee,
		cdek_create_state,cdek_uuid,cdek_status,cdek_status_reason,cdek_last_error,cdek_track_number,
		payment_status,delivery_fee_pending=1,has_preorder=1,cdek_checkout_requested=1,
		EXISTS(SELECT 1 FROM order_packing_workflows w WHERE w.order_id=o.id AND w.state='reopening'),
		EXISTS(SELECT 1 FROM shipment_offers so WHERE so.order_id=o.id AND so.status NOT IN ('cancelled','expired','stale'))
		FROM orders o WHERE id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	var d orderCDEKData
	err := reader.QueryRow(ctx, query, id).Scan(&d.revision, &d.tariff, &d.delivery, &d.status, &d.payee,
		&d.state, &d.uuid, &d.carrierStatus, &d.reason, &d.lastError, &d.track, &d.paymentStatus,
		&d.feePending, &d.preorder, &d.requested, &d.reopening, &d.hasOffers)
	return d, err
}

func (repository *PostgresRepository) cdekOrderState(d orderCDEKData) OrderCDEKState {
	state := OrderCDEKState{Revision: d.revision, TrackNumber: d.track, ManualTrack: d.state == "manual"}
	if d.delivery != "cdek" || d.status == "cancelled" {
		return state
	}
	switch d.state {
	case "manual":
	case "retry_queued", "creating", "retry_cancel":
		state.Pending = true
	case "unknown":
		state.Pending = true
		state.Error = d.lastError
		if state.Error == "" {
			state.Error = "СДЭК ещё не подтвердил результат регистрации"
		}
	default:
		state.Error = d.lastError
	}
	rejected := strings.EqualFold(d.carrierStatus, "INVALID")
	removed := strings.EqualFold(d.carrierStatus, "REMOVED")
	if rejected && state.Error == "" && !state.ManualTrack && !state.Pending {
		state.Error = d.reason
		if state.Error == "" {
			state.Error = "СДЭК отклонил регистрацию без подробностей"
		}
	}
	if d.state == "registered" && !rejected && !removed && d.track == "" && d.status != "completed" {
		state.Pending = true
	}
	if !d.reopening && !d.hasOffers && d.state != "creating" && d.state != "unknown" && d.state != "retry_cancel" {
		state.CanSetTracking = d.track == "" || state.ManualTrack
	}
	configured := repository.shipmentQuotes != nil && repository.shipmentQuotes.Configured() &&
		repository.packingSettings != nil && repository.packingSettings.Enabled(settings.CDEKOrdersEnabled)
	eligible := !d.feePending && !d.preorder && d.payee == "shop" && d.tariff > 0 && !d.hasOffers
	paymentReady := d.paymentStatus == "paid" || d.paymentStatus == "on_delivery" ||
		d.requested && (d.paymentStatus == "pending" || d.paymentStatus == "partially_paid")
	if (d.state == "new" || d.state == "pending") && configured && eligible && paymentReady &&
		!d.reopening && d.track == "" && d.status != "completed" && d.status != "shipped" {
		state.Pending = true
	}
	retryable := d.state == "retry" && d.uuid == "" ||
		(d.state == "registered" || d.state == "retry_cancel_failed") && d.uuid != "" && (rejected || removed)
	if !retryable || d.track != "" || d.reopening || d.status == "completed" || d.status == "shipped" {
		return state
	}

	switch {
	case !eligible:
		state.RetryUnavailable = "Проверьте состав, тариф и условия доставки перед повторной регистрацией."
	case !paymentReady:
		state.RetryUnavailable = "Заказ пока не готов к автоматической регистрации в СДЭК."
	case !configured:
		state.RetryUnavailable = "Автоматическая регистрация СДЭК выключена или не настроена."
	default:
		state.CanRetry = true
	}
	return state
}

func (repository *PostgresRepository) OrderCDEK(ctx context.Context, id int64) (OrderCDEKState, error) {
	data, err := loadOrderCDEK(ctx, repository.pool, id, false)
	if err != nil {
		return OrderCDEKState{}, err
	}
	return repository.cdekOrderState(data), nil
}

func (repository *PostgresRepository) UpdateOrderCDEK(ctx context.Context, actor Actor, id int64, input OrderCDEKInput) (OrderCDEKState, error) {
	if !Can(actor.Role, PermissionOrdersEdit) {
		return OrderCDEKState{}, ErrForbidden
	}
	track := strings.TrimSpace(input.TrackNumber)
	if input.Revision <= 0 || (input.Action != "retry" && input.Action != "set_tracking") ||
		(input.Action == "set_tracking" && !trackingNumberFormat.MatchString(track)) {
		return OrderCDEKState{}, ErrOrderCDEKInput
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return OrderCDEKState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7241,$1::integer)`, id); err != nil {
		return OrderCDEKState{}, err
	}
	data, err := loadOrderCDEK(ctx, tx, id, true)
	if err != nil {
		return OrderCDEKState{}, err
	}
	state := repository.cdekOrderState(data)
	if input.Revision != state.Revision || data.delivery != "cdek" || data.status == "cancelled" {
		return state, ErrOrderCDEKConflict
	}
	before := map[string]any{"state": data.state}
	after := map[string]any{}
	auditAction := "order.cdek.retry"
	if input.Action == "retry" {
		if !state.CanRetry {
			if state.RetryUnavailable != "" {
				return state, fmt.Errorf("%w. %s", ErrOrderCDEKConflict, state.RetryUnavailable)
			}
			return state, ErrOrderCDEKConflict
		}
		next := "retry_queued"
		if data.uuid != "" {
			next = "retry_cancel"
		}
		_, err = tx.Exec(ctx, `UPDATE orders SET cdek_create_state=$2,cdek_next_attempt_at=CURRENT_TIMESTAMP,
			cdek_cancel_attempts=0 WHERE id=$1`, id, next)
		after["state"] = next
	} else {
		if !state.CanSetTracking {
			return state, ErrOrderCDEKConflict
		}
		if data.state == "manual" && data.track == track {
			return state, nil
		}
		_, err = tx.Exec(ctx, `UPDATE orders SET cdek_track_number=$2,cdek_create_state='manual',
			cdek_next_attempt_at=NULL WHERE id=$1`, id, track)
		auditAction = "order.cdek.manual_tracking"
		before["trackNumber"], after["trackNumber"] = data.track, track
		after["state"] = "manual"
	}
	if err != nil {
		return state, err
	}
	if err = insertAudit(ctx, tx, actor, auditAction, "order", fmt.Sprint(id), before, after); err != nil {
		return state, err
	}
	if err = tx.Commit(ctx); err != nil {
		return state, err
	}
	if repository.packingWake != nil {
		repository.packingWake()
	}
	return repository.OrderCDEK(ctx, id)
}
