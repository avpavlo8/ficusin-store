package integration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Отказ Ozon по продажам выглядит одинаково для четырёх разных бед: список
// отправлений пуст; список полон, но доставленных в нём нет; доставленные
// есть, но без кода продавца; площадка вовсе не ответила. Лечится каждая
// по-своему, а на плашке было одно и то же «Ozon не вернул отправлений», и
// следующий заход всякий раз начинался с догадки.
//
// Поэтому после отказа канал ходит за отправлениями ещё раз — уже не за
// продажами, а за ответом на вопрос «что вообще пришло»: сколько
// отправлений, с какими статусами, сколько в них позиций и у скольких есть
// offer_id. Лишний заход стоит десятка запросов раз в сутки и только когда
// синхронизация уже не удалась.
type ozonScan struct {
	path      string
	postings  int
	delivered int
	items     int
	coded     int
	statuses  map[string]int
	failure   error
}

func (scan ozonScan) describe() string {
	if scan.failure != nil {
		return fmt.Sprintf("%s — %s", scan.path, safeRemoteMessage(scan.failure.Error()))
	}
	if scan.postings == 0 {
		return fmt.Sprintf("%s — отправлений нет", scan.path)
	}
	return fmt.Sprintf(
		"%s — отправлений %d (%s), доставленных %d, в них позиций %d, с кодом продавца %d",
		scan.path, scan.postings, describeOzonStatuses(scan.statuses),
		scan.delivered, scan.items, scan.coded,
	)
}

// describeOzonStatuses перечисляет статусы по убыванию частоты: наборы
// значений у FBS и FBO разные, и какое из них означает доставку у этого
// продавца — видно только по ответу площадки.
func describeOzonStatuses(statuses map[string]int) string {
	names := make([]string, 0, len(statuses))
	for name := range statuses {
		names = append(names, name)
	}
	sort.Slice(names, func(first, second int) bool {
		if statuses[names[first]] != statuses[names[second]] {
			return statuses[names[first]] > statuses[names[second]]
		}
		return names[first] < names[second]
	})
	if len(names) > 5 {
		names = names[:5]
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, statuses[name]))
	}
	if len(parts) == 0 {
		return "без статусов"
	}
	return strings.Join(parts, ", ")
}

// DescribeSalesFailure объясняет отказ синхронизации продаж подробнее, чем
// это может сделать сам обмен. Для каналов, о которых сказать нечего,
// возвращает исходную ошибку без изменений.
func (executor *MarketplaceExecutor) DescribeSalesFailure(
	ctx context.Context,
	channel string,
	from, to time.Time,
	cause error,
) error {
	if channel != "ozon" || cause == nil || !executor.Configured("ozon") {
		return cause
	}
	reports := make([]string, 0, 2)
	for _, path := range []string{"/v3/posting/fbs/list", "/v2/posting/fbo/list"} {
		reports = append(reports, executor.scanOzonPostings(ctx, path, from, to).describe())
	}
	return fmt.Errorf("%w. Разведка за %s — %s: %s", cause,
		from.Format("2006-01-02"), to.Format("2006-01-02"), strings.Join(reports, "; "))
}

func (executor *MarketplaceExecutor) scanOzonPostings(
	ctx context.Context,
	path string,
	from, to time.Time,
) ozonScan {
	scan := ozonScan{path: path, statuses: map[string]int{}}
	postings, err := executor.fetchOzonPostings(ctx, path, from, to)
	if err != nil {
		var empty *emptyBodyError
		if errors.As(err, &empty) {
			// Пустое тело с кодом успеха — не отказ, а молчание: у продавца
			// может не быть этой схемы работы вовсе.
			scan.failure = fmt.Errorf("ответил пустым телом")
			return scan
		}
		scan.failure = err
		return scan
	}
	scan.postings = len(postings)
	for _, posting := range postings {
		status := strings.ToLower(strings.TrimSpace(posting.Status))
		if status == "" {
			status = "без статуса"
		}
		scan.statuses[status]++
		if status != "delivered" {
			continue
		}
		scan.delivered++
		for _, product := range posting.Products {
			scan.items++
			if strings.TrimSpace(product.OfferID) != "" && product.Quantity > 0 {
				scan.coded++
			}
		}
	}
	return scan
}
