// Package cashierstock mirrors availability reservations to Saby invoices.
// It has no sale, posting, payment or fiscal operation.
package cashierstock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	WarehouseID, MovementWarehouseID, OrganizationID, RegulationID int64
}
type Line struct {
	NomenclatureID int64  `json:"nomenclatureId"`
	Quantity       int64  `json:"quantity"`
	UnitPrice      string `json:"unitPrice"`
}
type Request struct {
	ReservationID int64  `json:"reservationId"`
	OrderID       int64  `json:"orderId"`
	OrderNumber   string `json:"orderNumber"`
	Terminal      string `json:"terminal,omitempty"`
	Lines         []Line `json:"lines"`
	Config        Config `json:"config"`
	BuyerID       int64  `json:"buyerId,omitempty"`
}
type Command struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Params json.RawMessage `json:"params"`
}
type Progress struct {
	Native    json.RawMessage
	Command   *Command
	Attempted bool
	Reply     json.RawMessage
	Error     string
}
type Plan struct {
	Native     json.RawMessage
	Command    *Command
	Done       bool
	Attention  bool
	Message    string
	RetryAfter time.Duration
}
type Provider interface {
	// Next must be read-only, including after an expired execution lease.
	Next(context.Context, Request, Progress) (Plan, error)
	// Execute sends only the durably prepared command once, without retries.
	Execute(context.Context, Command) (json.RawMessage, error)
}

func Fingerprint(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func ReadRequest(ctx context.Context, tx pgx.Tx, id int64, config Config, buyerID int64) (Request, error) {
	r := Request{ReservationID: id, Config: config, BuyerID: buyerID, Lines: []Line{}}
	var status, method, payment string
	err := tx.QueryRow(ctx, `SELECT o.id,o.order_number,o.status,o.payment_method,COALESCE(o.payment_status,'')
 FROM cashier_stock_reservations r JOIN orders o ON o.id=r.order_id WHERE r.id=$1 FOR SHARE OF o`, id).Scan(&r.OrderID, &r.OrderNumber, &status, &method, &payment)
	if err != nil {
		return r, err
	}
	if method != "on_delivery" {
		return r, errors.New("Способ оплаты изменился; нужна сверка кассового резерва")
	}
	if status == "cancelled" {
		r.Terminal = "cancelled"
		return r, nil
	}
	if status == "completed" {
		if payment != "paid" {
			return r, errors.New("Выдача не подтверждена оплатой на кассе")
		}
		r.Terminal = "completed"
		return r, nil
	}
	rows, err := tx.Query(ctx, `SELECT COALESCE((SELECT CASE WHEN COUNT(DISTINCT external_id)=1 THEN MIN(external_id) ELSE '' END
 FROM product_external_ids WHERE variant_id=oi.variant_id AND provider='saby' AND id_type='id' AND status='active' HAVING COUNT(*)>0),
 (SELECT saby_id FROM product_variants WHERE id=oi.variant_id),''),oi.quantity,oi.unit_price::TEXT
 FROM order_items oi WHERE oi.order_id=$1 ORDER BY oi.id`, r.OrderID)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	grouped := map[string]Line{}
	for rows.Next() {
		var sku, price string
		var quantity int64
		if err = rows.Scan(&sku, &quantity, &price); err != nil {
			return r, err
		}
		native, parseErr := strconv.ParseInt(sku, 10, 64)
		p, valid := new(big.Rat).SetString(price)
		if parseErr != nil || native <= 0 || quantity <= 0 || quantity > 1000000 || !valid || p.Sign() < 0 || len(price) > 40 {
			return r, errors.New("Для резерва СБИС нужны связанные товары, корректные цены и количества")
		}
		// The DB column is decimal money. Preserve its exact two-decimal value.
		canonical := p.FloatString(2)
		if exact, _ := new(big.Rat).SetString(canonical); exact.Cmp(p) != 0 {
			return r, errors.New("Цена резерва содержит доли копейки")
		}
		key := strconv.FormatInt(native, 10) + ":" + canonical
		line := grouped[key]
		line.NomenclatureID = native
		line.UnitPrice = canonical
		line.Quantity += quantity
		if line.Quantity > 1000000 {
			return r, errors.New("Слишком большое количество для резерва СБИС")
		}
		grouped[key] = line
	}
	if err = rows.Err(); err != nil {
		return r, err
	}
	for _, line := range grouped {
		r.Lines = append(r.Lines, line)
	}
	sort.Slice(r.Lines, func(i, j int) bool {
		if r.Lines[i].NomenclatureID == r.Lines[j].NomenclatureID {
			return r.Lines[i].UnitPrice < r.Lines[j].UnitPrice
		}
		return r.Lines[i].NomenclatureID < r.Lines[j].NomenclatureID
	})
	if len(r.Lines) == 0 || len(r.Lines) > 1000 {
		return r, errors.New("Состав заказа для резерва СБИС пуст или слишком велик")
	}
	return r, nil
}

// Step retains a command before sending it and never replays an attempted one.
// A lost process/HTTP reply is passed to Provider.Next for read-only recovery.
func Step(ctx context.Context, pool *pgxpool.Pool, provider Provider, config Config, buyerID int64) (bool, error) {
	if provider == nil {
		return false, errors.New("Не подключён обработчик резервов СБИС")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO cashier_native_reservations(reservation_id)
 SELECT id FROM cashier_stock_reservations WHERE completed_at IS NULL AND cancelled_at IS NULL ON CONFLICT DO NOTHING`)
	if err != nil {
		return false, err
	}
	var id int64
	var native, requestRaw, commandRaw, reply []byte
	var attempted bool
	var commandError, status, applied, token string
	err = tx.QueryRow(ctx, `SELECT reservation_id,provider_state,request,pending_command,command_reply,
 command_attempted_at IS NOT NULL,command_error,status,applied_hash,gen_random_uuid()::TEXT
 FROM cashier_native_reservations WHERE status<>'needs_attention' AND next_attempt_at<=clock_timestamp()
 AND (lease_until IS NULL OR lease_until<clock_timestamp())
 ORDER BY next_attempt_at,reservation_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &native, &requestRaw, &commandRaw, &reply, &attempted, &commandError, &status, &applied, &token)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	desired, readErr := ReadRequest(ctx, tx, id, config, buyerID)
	if readErr != nil {
		_, err = tx.Exec(ctx, `UPDATE cashier_native_reservations SET status='needs_attention',last_error=$2,updated_at=clock_timestamp() WHERE reservation_id=$1`, id, "Не удалось проверить состав и привязки кассового заказа")
		if err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	var request Request
	if len(requestRaw) > 0 {
		if json.Unmarshal(requestRaw, &request) != nil || request.ReservationID != id || request.OrderID != desired.OrderID {
			return false, errors.New("Повреждён сохранённый запрос резерва СБИС")
		}
	}
	if len(requestRaw) == 0 || status == "settled" {
		if status == "settled" && applied == Fingerprint(desired) {
			_, err = tx.Exec(ctx, `UPDATE cashier_native_reservations SET next_attempt_at=clock_timestamp()+INTERVAL '5 seconds' WHERE reservation_id=$1`, id)
			if err != nil {
				return false, err
			}
			return true, tx.Commit(ctx)
		}
		if len(commandRaw) > 0 {
			return false, errors.New("Завершённый резерв содержит неразобранную команду")
		}
		request = desired
		requestRaw, _ = json.Marshal(request)
	}
	var command *Command
	if len(commandRaw) > 0 && string(commandRaw) != "null" {
		command = &Command{}
		if json.Unmarshal(commandRaw, command) != nil || command.Kind == "" || len(command.Params) == 0 {
			return false, errors.New("Повреждена сохранённая команда резерва СБИС")
		}
	}
	_, err = tx.Exec(ctx, `UPDATE cashier_native_reservations SET request=$2,status='queued',lease_token=$3::UUID,
 lease_until=clock_timestamp()+INTERVAL '90 seconds',command_attempted_at=CASE WHEN $4 THEN clock_timestamp() ELSE command_attempted_at END,
	 revision_started_at=CASE WHEN $5 THEN clock_timestamp() ELSE revision_started_at END,
	 updated_at=clock_timestamp() WHERE reservation_id=$1`, id, requestRaw, token, command != nil && !attempted, status == "settled")
	if err != nil {
		return false, err
	}
	if command != nil && !attempted {
		if command.ID == "" {
			return false, errors.New("Команда резерва не имеет долговечного идентификатора")
		}
		_, err = tx.Exec(ctx, `INSERT INTO cashier_native_attempts(command_id,reservation_id,request,command) VALUES($1::UUID,$2,$3,$4)`, command.ID, id, requestRaw, commandRaw)
		if err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if command != nil && !attempted {
		answer, callErr := provider.Execute(callCtx, *command)
		message := ""
		if callErr != nil {
			message = "Ответ изменения резерва СБИС не подтверждён; разрешена только сверка"
		}
		if len(answer) == 0 {
			answer = []byte("null")
		}
		_, err = pool.Exec(ctx, `UPDATE cashier_native_attempts SET reply=$3,error=$4,replied_at=clock_timestamp() WHERE command_id=$1::UUID AND reservation_id=$2`, command.ID, id, answer, message)
		if err != nil {
			return true, err
		}
		_, err = pool.Exec(ctx, `UPDATE cashier_native_reservations SET command_reply=$3,command_error=$4,
 lease_token=NULL,lease_until=NULL,next_attempt_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE reservation_id=$1 AND lease_token=$2::UUID`, id, token, answer, message)
		return true, err
	}
	plan, nextErr := provider.Next(callCtx, request, Progress{Native: native, Command: command, Attempted: attempted, Reply: reply, Error: commandError})
	if nextErr != nil {
		_, err = pool.Exec(ctx, `UPDATE cashier_native_reservations SET lease_token=NULL,lease_until=NULL,
 next_attempt_at=clock_timestamp()+INTERVAL '30 seconds',last_error='Не удалось прочитать состояние резерва СБИС',updated_at=clock_timestamp()
 WHERE reservation_id=$1 AND lease_token=$2::UUID`, id, token)
		return true, err
	}
	if len(plan.Native) == 0 {
		plan.Native = []byte("{}")
	}
	var nextCommand any
	if plan.Command != nil {
		plan.Command.ID = token
		b, e := json.Marshal(plan.Command)
		if e != nil {
			return true, e
		}
		nextCommand = b
	}
	nextStatus := "queued"
	if plan.Done {
		nextStatus = "settled"
	}
	if plan.Attention {
		nextStatus = "needs_attention"
	}
	if plan.Done && plan.Command != nil || plan.Attention && plan.Command != nil || plan.Done && plan.Attention {
		return true, errors.New("Некорректный план резерва СБИС")
	}
	newApplied := applied
	if plan.Done {
		newApplied = Fingerprint(request)
	}
	delay := plan.RetryAfter
	if nextStatus == "settled" {
		delay = 5 * time.Second
	}
	if delay < 0 || delay > time.Minute {
		return true, errors.New("Некорректный срок сверки резерва")
	}
	_, err = pool.Exec(ctx, `UPDATE cashier_native_reservations SET provider_state=$3,pending_command=$4,
 command_attempted_at=NULL,command_reply=NULL,command_error='',status=$5,applied_hash=$6,last_error=$7,
 lease_token=NULL,lease_until=NULL,next_attempt_at=clock_timestamp()+$8::BIGINT*INTERVAL '1 millisecond',
 updated_at=clock_timestamp() WHERE reservation_id=$1 AND lease_token=$2::UUID`, id, token, plan.Native, nextCommand, nextStatus, newApplied, plan.Message, delay.Milliseconds())
	return true, err
}

func Run(ctx context.Context, pool *pgxpool.Pool, provider Provider, config Config, buyerID int64, logger *slog.Logger) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		worked, err := Step(ctx, pool, provider, config, buyerID)
		if err != nil {
			logger.Error("cashier Saby reservation step failed", "error", err)
		}
		delay := 5 * time.Second
		if worked && err == nil {
			delay = 100 * time.Millisecond
		}
		timer.Reset(delay)
	}
}
