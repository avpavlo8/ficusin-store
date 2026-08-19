// Package settings holds the switches the shop owner may flip without a
// redeploy: whether an integration is live, how long an unpaid order waits,
// who the parcels are sent from.
//
// Secrets are deliberately not here. Keys belong in the environment; a key
// in a table is a key that leaks through a database backup, and this shop
// has already lost two integrations to a credentials table.
package settings

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Keys. Adding one here and to defaults is all it takes for it to appear in
// the panel.
const (
	CDEKOrdersEnabled = "cdek.orders_enabled"
	CDEKSenderName    = "cdek.sender_name"
	CDEKSenderPhone   = "cdek.sender_phone"
	CDEKSenderAddress = "cdek.sender_address"
	PaymentsEnabled   = "payments.enabled"
	TelegramEnabled   = "telegram.enabled"
	AutoCancelHours   = "orders.auto_cancel_hours"
	SabyStockEnabled  = "saby.stock_enabled"
	CourierFee        = "delivery.courier_fee"
	PostFee           = "delivery.post_fee"
)

// Definition is what the panel needs to draw one setting.
type Definition struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Note  string `json:"note"`
	Kind  string `json:"kind"` // "switch", "text" or "number"
}

// Definitions is the whole list, in the order the panel shows it.
var Definitions = []Definition{
	{
		Key:   PaymentsEnabled,
		Title: "Оплата картой",
		Note:  "Выключите, чтобы покупатели не платили во время проверок. Ключи при этом трогать не нужно.",
		Kind:  "switch",
	},
	{
		Key:   CDEKOrdersEnabled,
		Title: "Создавать заказы в СДЭК",
		Note:  "Выключено — доставка считается и пункты выдачи работают, но отправления в СДЭК не создаются и трек-номера не появляются.",
		Kind:  "switch",
	},
	{
		Key:   SabyStockEnabled,
		Title: "Списывать остатки в СБИС",
		Note:  "Выключено — магазин только записывает в журнал, что он сделал бы с настоящим складом: заказ занимает растение, отгрузка списывает. Наружу ничего не уходит, и это стоит проверить до того, как трогать живой склад.",
		Kind:  "switch",
	},
	{
		Key:   TelegramEnabled,
		Title: "Уведомления в Telegram",
		Note:  "Выключите, чтобы тестовые заказы не сыпались менеджеру.",
		Kind:  "switch",
	},
	{
		Key:   AutoCancelHours,
		Title: "Отменять неоплаченный заказ через, часов",
		Note:  "Заказ освобождает товар обратно на склад. 0 — не отменять автоматически.",
		Kind:  "number",
	},
	{
		Key:   CourierFee,
		Title: "Курьер по городу, ₽",
		Note:  "Сколько покупатель платит за доставку курьером. 0 — возим бесплатно.",
		Kind:  "number",
	},
	{
		Key:   PostFee,
		Title: "Почта России, ₽",
		Note:  "Сколько покупатель платит за отправку почтой. 0 — возим бесплатно.",
		Kind:  "number",
	},
	{
		Key:   CDEKSenderName,
		Title: "Отправитель: имя",
		Note:  "Кого СДЭК указывает отправителем.",
		Kind:  "text",
	},
	{
		Key:   CDEKSenderPhone,
		Title: "Отправитель: телефон",
		Note:  "Номер для курьера и склада СДЭК.",
		Kind:  "text",
	},
	{
		Key:   CDEKSenderAddress,
		Title: "Отправитель: адрес",
		Note:  "Откуда забирают посылки.",
		Kind:  "text",
	},
}

// defaults are what a setting means before anyone has touched it. Both
// integrations start switched on so that an existing shop does not quietly
// lose them the moment this ships.
var defaults = map[string]string{
	PaymentsEnabled:   "1",
	CDEKOrdersEnabled: "0",
	TelegramEnabled:   "1",
	AutoCancelHours:   "24",
	SabyStockEnabled:  "0",
	CourierFee:        "490",
	PostFee:           "590",
	CDEKSenderName:    "",
	CDEKSenderPhone:   "",
	CDEKSenderAddress: "",
}

// Service keeps the settings in memory so that reading one costs nothing.
// The copy is refreshed on a timer and immediately after any change, which
// is what makes the panel feel instant.
type Service struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	mu     sync.RWMutex
	values map[string]string
}

func NewService(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	service := &Service{pool: pool, logger: logger, values: map[string]string{}}
	service.reload(context.Background())
	return service
}

// Run keeps the cache honest when the shop runs in more than one copy, and
// after a change made straight in the database.
func (service *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.reload(ctx)
		}
	}
}

func (service *Service) reload(ctx context.Context) {
	if service == nil || service.pool == nil {
		return
	}
	rows, err := service.pool.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		service.logger.Error("load settings failed", "error", err)
		return
	}
	defer rows.Close()
	loaded := make(map[string]string, len(defaults))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			service.logger.Error("scan setting failed", "error", err)
			return
		}
		loaded[key] = value
	}
	if rows.Err() != nil {
		return
	}
	service.mu.Lock()
	service.values = loaded
	service.mu.Unlock()
}

// Value returns the stored setting, or its default when nobody has set it.
func (service *Service) Value(key string) string {
	if service == nil {
		return defaults[key]
	}
	service.mu.RLock()
	value, stored := service.values[key]
	service.mu.RUnlock()
	if !stored {
		return defaults[key]
	}
	return value
}

// Enabled reads a switch. Anything other than "0" counts as on, so a
// half-written value fails towards the shop working.
func (service *Service) Enabled(key string) bool {
	return strings.TrimSpace(service.Value(key)) != "0"
}

func (service *Service) Number(key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(service.Value(key)))
	if err != nil {
		fallback, _ := strconv.Atoi(defaults[key])
		return fallback
	}
	return value
}

// DefaultNumber — умолчание настройки для тех, у кого самих настроек нет.
// Нужно там, где сервис собирают без панели: в тестах цена доставки всё
// равно обязана быть настоящей, иначе тест проверяет выдуманный магазин.
func DefaultNumber(key string) int {
	value, _ := strconv.Atoi(defaults[key])
	return value
}

// All returns every setting with its current value, for the panel.
func (service *Service) All() map[string]string {
	values := make(map[string]string, len(Definitions))
	for _, definition := range Definitions {
		values[definition.Key] = service.Value(definition.Key)
	}
	return values
}

// Save writes the changes and refreshes the cache at once, so the person who
// flipped a switch sees it take effect immediately.
func (service *Service) Save(ctx context.Context, changes map[string]string) error {
	for _, definition := range Definitions {
		value, changed := changes[definition.Key]
		if !changed {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) > 300 {
			value = value[:300]
		}
		if _, err := service.pool.Exec(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES ($1, $2, CURRENT_TIMESTAMP)
			ON CONFLICT (key)
			DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP
		`, definition.Key, value); err != nil {
			return err
		}
	}
	service.reload(ctx)
	return nil
}
