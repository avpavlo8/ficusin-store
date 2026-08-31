package integration

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// pacedTransport выдерживает паузу между обращениями к одному хосту.
//
// Площадки ограничивают не только объём, но и частоту: Ozon отвечает
// «rate limit exceeded for seller-api client, current max rate per sec.: 2»,
// Wildberries — «Limited by global limiter, per seller». Обход каталога
// страницами и выгрузка продаж шлют десятки запросов подряд, упираются в
// это на втором же и возвращаются ни с чем.
//
// Пауза живёт в транспорте, а не в вызывающем коде: так её не забудет ни
// один новый запрос, какой бы метод площадки ни понадобился завтра.
type pacedTransport struct {
	base   http.RoundTripper
	sleep  func(time.Duration)
	now    func() time.Time
	mu     sync.Mutex
	lastAt map[string]time.Time
}

// Паузы взяты с запасом к объявленным лимитам: Ozon разрешает два запроса в
// секунду, мы просим один в шестьсот миллисекунд. У Wildberries предел не
// назван числом и считается на продавца целиком, вместе с чужими
// интеграциями, поэтому секунда.
func marketplacePace(host string) time.Duration {
	switch {
	case strings.HasSuffix(host, "ozon.ru"):
		return 600 * time.Millisecond
	// Оперативные продажи имеют строгий лимит на продавца. Часовое зеркало
	// делает один такой запрос, а эта пауза страхует ручную диагностику.
	case strings.HasSuffix(host, "statistics-api.wildberries.ru"):
		return 65 * time.Second
	case strings.HasSuffix(host, "discounts-prices-api.wildberries.ru"):
		// The price endpoint shares a seller-wide limiter with other
		// integrations. A one-second cadence still produced bursts of 429 in
		// production; ten seconds keeps this worker deliberately gradual.
		return 10 * time.Second
	case strings.HasSuffix(host, "wildberries.ru"):
		return time.Second
	default:
		return 0
	}
}

func newPacedTransport(base http.RoundTripper) *pacedTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &pacedTransport{
		base: base, sleep: time.Sleep, now: time.Now, lastAt: map[string]time.Time{},
	}
}

func (transport *pacedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	host := request.URL.Hostname()
	if pause := marketplacePace(host); pause > 0 {
		// Держим замок на время ожидания: два запроса к одной площадке не
		// должны выйти одновременно, иначе пауза перестаёт что-либо значить.
		transport.mu.Lock()
		if last, known := transport.lastAt[host]; known {
			if wait := pause - transport.now().Sub(last); wait > 0 {
				transport.sleep(wait)
			}
		}
		transport.lastAt[host] = transport.now()
		transport.mu.Unlock()
	}
	return transport.base.RoundTrip(request)
}
