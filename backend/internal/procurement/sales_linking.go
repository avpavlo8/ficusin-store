package procurement

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// manualSalesChannels — каналы, у которых внешний код продажи вообще может
// разойтись с номенклатурой. Сайт и СБИС кладут в продажи сам saby_id,
// связывать там нечего.
var manualSalesChannels = []string{"wb", "ozon"}

// unlinkedSalesLimit — сколько внешних кодов отдаём на экран за раз.
//
// Разбор ручной, дальше третьей сотни за один заход никто не уходит, а
// без потолка редкий канал с тысячами разовых кодов повесил бы таблицу.
const unlinkedSalesLimit = 300

// UnlinkedSale — внешний код, продажи которого не дошли до товара.
//
// Строка агрегирована по коду, а не по дню: разбирающему важно, сколько
// всего продано под этим кодом и когда его видели в последний раз, —
// давно замолчавший код чаще всего снятая карточка, и его можно отложить.
type UnlinkedSale struct {
	Channel    string  `json:"channel"`
	ExternalID string  `json:"externalId"`
	Days       int     `json:"days"`
	Units      int     `json:"units"`
	GrossRUB   float64 `json:"grossRub"`
	LastSale   string  `json:"lastSale"`
}

// SalesLink — решение человека: этот внешний код принадлежит этому товару.
type SalesLink struct {
	Channel    string `json:"channel"`
	ExternalID string `json:"externalId"`
	SabyID     string `json:"sabyId"`
}

// SalesLinkResult — что изменилось после связывания.
//
// Одного «сохранено» мало: человек разбирает список ради расчёта закупки и
// должен видеть, сколько продаж вернулось в расчёт и сколько кодов ещё
// ждут. TakenFrom не пустой, когда код пришлось отобрать у другого товара.
type SalesLinkResult struct {
	Channel     string `json:"channel"`
	ExternalID  string `json:"externalId"`
	SabyID      string `json:"sabyId"`
	SabyName    string `json:"sabyName"`
	LinkedRows  int    `json:"linkedRows"`
	LinkedUnits int    `json:"linkedUnits"`
	TakenFrom   string `json:"takenFrom"`
	Remaining   int    `json:"remaining"`
}

// SalesLinkStore — необязательное дополнение к Store: разбор продаж, которым
// не нашлось товара.
//
// Отдельным интерфейсом с приведением типа, как SalesDiagnostics у источника
// продаж. Store реализуют ещё и заглушки тестов, поэтому каждый метод,
// добавленный в него, стоит правки во всех сразу и связан с задачей только
// тем, что тоже ходит в базу.
type SalesLinkStore interface {
	ListUnlinkedSales(context.Context, string, int) ([]UnlinkedSale, error)
	LinkSalesProduct(context.Context, Actor, SalesLink) (SalesLinkResult, error)
}

// ErrSalesLinkUnsupported — хранилище не умеет разбирать продажи руками.
var ErrSalesLinkUnsupported = errors.New("procurement store cannot link sales manually")

// UnlinkedSales отдаёт коды канала, оставшиеся без товара.
func (service *Service) UnlinkedSales(ctx context.Context, channel string) ([]UnlinkedSale, error) {
	channel = strings.TrimSpace(channel)
	if !oneOf(channel, manualSalesChannels...) {
		return nil, ErrInvalidInput
	}
	store, able := service.store.(SalesLinkStore)
	if !able {
		return nil, ErrSalesLinkUnsupported
	}
	return store.ListUnlinkedSales(ctx, channel, unlinkedSalesLimit)
}

// LinkSalesProduct закрепляет внешний код за товаром СБИС.
func (service *Service) LinkSalesProduct(ctx context.Context, actor Actor, input SalesLink) (SalesLinkResult, error) {
	input.Channel = strings.TrimSpace(input.Channel)
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.SabyID = strings.TrimSpace(input.SabyID)
	if !oneOf(input.Channel, manualSalesChannels...) || input.ExternalID == "" || input.SabyID == "" ||
		len(input.ExternalID) > 200 || len(input.SabyID) > 200 {
		return SalesLinkResult{}, ErrInvalidInput
	}
	// У Wildberries внешний код продажи — это nmID, и хранится он числом.
	// Текст в этом поле означает, что связывать собрались не тот код, и
	// молча положить его в базу нельзя: приведение упадёт уже в запросе.
	if input.Channel == "wb" {
		if _, err := strconv.ParseInt(input.ExternalID, 10, 64); err != nil {
			return SalesLinkResult{}, ErrInvalidInput
		}
	}
	store, able := service.store.(SalesLinkStore)
	if !able {
		return SalesLinkResult{}, ErrSalesLinkUnsupported
	}
	return store.LinkSalesProduct(ctx, actor, input)
}

func (store *PostgresStore) ListUnlinkedSales(ctx context.Context, channel string, limit int) ([]UnlinkedSale, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT external_product_id, COUNT(*)::INTEGER,
			COALESCE(SUM(units), 0)::INTEGER,
			COALESCE(SUM(gross_rub), 0)::DOUBLE PRECISION,
			MAX(sale_date)::TEXT
		FROM procurement_sales_daily
		WHERE channel = $1 AND saby_id IS NULL
		GROUP BY external_product_id
		ORDER BY SUM(units) DESC, MAX(sale_date) DESC
		LIMIT $2
	`, channel, limit)
	if err != nil {
		return nil, fmt.Errorf("list unlinked sales: %w", err)
	}
	defer rows.Close()
	items := make([]UnlinkedSale, 0, 64)
	for rows.Next() {
		item := UnlinkedSale{Channel: channel}
		if err := rows.Scan(&item.ExternalID, &item.Days, &item.Units, &item.GrossRUB, &item.LastSale); err != nil {
			return nil, fmt.Errorf("scan unlinked sales: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// LinkSalesProduct связывает код канала с товаром и чинит уже загруженные
// продажи.
//
// Одной записи в справочник каналов мало: saby_id проставляется только при
// вставке продажи, поэтому без засыпки задним числом разобранная строка
// вернулась бы в расчёт лишь после следующей глубокой выгрузки, а более
// старая — никогда.
func (store *PostgresStore) LinkSalesProduct(ctx context.Context, actor Actor, input SalesLink) (SalesLinkResult, error) {
	result := SalesLinkResult{Channel: input.Channel, ExternalID: input.ExternalID, SabyID: input.SabyID}
	var wbNumberID int64
	if input.Channel == "wb" {
		parsed, err := strconv.ParseInt(input.ExternalID, 10, 64)
		if err != nil {
			return SalesLinkResult{}, ErrInvalidInput
		}
		wbNumberID = parsed
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("begin sales link: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `SELECT name FROM saby_nomenclature WHERE saby_id = $1`, input.SabyID).Scan(&result.SabyName)
	if errors.Is(err, pgx.ErrNoRows) {
		return SalesLinkResult{}, ErrNotFound
	}
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("load nomenclature for sales link: %w", err)
	}

	// Карточка маркетплейса продаёт ровно один товар, а поле кода в
	// справочнике одно на товар. Если код уже закреплён за другим растением,
	// оставить обе связи нельзя: одна из них припишет чужие продажи. Человек
	// сейчас сказал, чей это код, — снимаем прежнюю и показываем, у кого.
	if input.Channel == "wb" {
		err = tx.QueryRow(ctx, `
			UPDATE procurement_product_channels
			SET wb_nm_id = NULL, updated_by = $3, updated_at = CURRENT_TIMESTAMP
			WHERE wb_nm_id = $1 AND saby_id <> $2
			RETURNING saby_id
		`, wbNumberID, input.SabyID, actor.CustomerID).Scan(&result.TakenFrom)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE procurement_product_channels
			SET ozon_offer_id = '', updated_by = $3, updated_at = CURRENT_TIMESTAMP
			WHERE ozon_offer_id = $1 AND saby_id <> $2
			RETURNING saby_id
		`, input.ExternalID, input.SabyID, actor.CustomerID).Scan(&result.TakenFrom)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SalesLinkResult{}, fmt.Errorf("release channel code: %w", err)
	}

	if input.Channel == "wb" {
		_, err = tx.Exec(ctx, `
			INSERT INTO procurement_product_channels (saby_id, wb_nm_id, updated_by)
			VALUES ($1, $2, $3)
			ON CONFLICT (saby_id) DO UPDATE SET wb_nm_id = EXCLUDED.wb_nm_id,
				updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
		`, input.SabyID, wbNumberID, actor.CustomerID)
	} else {
		_, err = tx.Exec(ctx, `
			INSERT INTO procurement_product_channels (saby_id, ozon_offer_id, updated_by)
			VALUES ($1, $2, $3)
			ON CONFLICT (saby_id) DO UPDATE SET ozon_offer_id = EXCLUDED.ozon_offer_id,
				updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP
		`, input.SabyID, input.ExternalID, actor.CustomerID)
	}
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("save channel code: %w", err)
	}

	command, err := tx.Exec(ctx, `
		UPDATE procurement_sales_daily SET saby_id = $3, synced_at = CURRENT_TIMESTAMP
		WHERE channel = $1 AND external_product_id = $2 AND saby_id IS DISTINCT FROM $3
	`, input.Channel, input.ExternalID, input.SabyID)
	if err != nil {
		return SalesLinkResult{}, fmt.Errorf("backfill linked sales: %w", err)
	}
	result.LinkedRows = int(command.RowsAffected())

	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(units), 0)::INTEGER FROM procurement_sales_daily
		WHERE channel = $1 AND external_product_id = $2
	`, input.Channel, input.ExternalID).Scan(&result.LinkedUnits); err != nil {
		return SalesLinkResult{}, fmt.Errorf("count linked sales: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT external_product_id)::INTEGER FROM procurement_sales_daily
		WHERE channel = $1 AND saby_id IS NULL
	`, input.Channel).Scan(&result.Remaining); err != nil {
		return SalesLinkResult{}, fmt.Errorf("count remaining sales: %w", err)
	}
	if err := audit(ctx, tx, actor, "procurement.sales.link", "procurement_sales_daily", 0, result); err != nil {
		return SalesLinkResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SalesLinkResult{}, fmt.Errorf("commit sales link: %w", err)
	}
	return result, nil
}
