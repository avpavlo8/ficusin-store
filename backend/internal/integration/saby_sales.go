package integration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

// SalesExecutor routes every sales read through the application's coordinated
// worker. Saby and marketplaces therefore share leases and request pacing.
type SalesExecutor struct {
	marketplaces *MarketplaceExecutor
	saby         *SabyClient
}

func NewSalesExecutor(marketplaces *MarketplaceExecutor, saby *SabyClient) *SalesExecutor {
	return &SalesExecutor{marketplaces: marketplaces, saby: saby}
}

func (executor *SalesExecutor) Configured(channel string) bool {
	if channel == "saby" {
		return executor.saby != nil && executor.saby.Configured()
	}
	return executor.marketplaces != nil && executor.marketplaces.Configured(channel)
}

func (executor *SalesExecutor) FetchSales(ctx context.Context, channel string, from, to time.Time) ([]procurement.SalesRecord, error) {
	if channel == "saby" {
		if executor.saby == nil {
			return nil, errors.New("Saby не настроен")
		}
		return executor.saby.FetchSales(ctx, from, to)
	}
	if executor.marketplaces == nil {
		return nil, errors.New("маркетплейсы не настроены")
	}
	return executor.marketplaces.FetchSales(ctx, channel, from, to)
}

func (executor *SalesExecutor) DescribeSalesFailure(ctx context.Context, channel string, from, to time.Time, cause error) error {
	if channel == "saby" || executor.marketplaces == nil {
		return cause
	}
	return executor.marketplaces.DescribeSalesFailure(ctx, channel, from, to, cause)
}

// FetchSales reads retail receipts directly from Saby. It does not fetch the
// catalogue: receipt identifiers are resolved through the cached canonical
// mappings already maintained by the catalogue lane.
func (client *SabyClient) FetchSales(ctx context.Context, from, to time.Time) ([]procurement.SalesRecord, error) {
	if !client.Configured() || from.After(to) {
		return nil, errors.New("Saby продажи не настроены")
	}
	const pageSize = 100
	records := make([]procurement.SalesRecord, 0, pageSize)
	previousPage := ""
	for page := 0; page < 1000; page++ {
		query := url.Values{
			"pointId":     {strconv.FormatInt(client.pointID, 10)},
			"fromDateTime": {from.Format("2006-01-02") + " 00:00:00"},
			"toDateTime":   {to.Format("2006-01-02") + " 23:59:59"},
			"page":         {strconv.Itoa(page)},
			"pageSize":     {strconv.Itoa(pageSize)},
		}
		var payload map[string]any
		endpoint := client.apiBase + "/retail/order/list?" + query.Encode()
		if err := client.authorizedJSON(ctx, http.MethodGet, endpoint, nil, &payload); err != nil {
			return nil, fmt.Errorf("получить продажи Saby, страница %d: %w", page, err)
		}
		orders := sabyObjectList(sabyFirst(payload, "orders", "sales", "result", "data"))
		if len(orders) == 0 {
			break
		}
		signature := sabyPageSignature(orders)
		if signature != "" && signature == previousPage {
			return nil, errors.New("Saby повторил страницу продаж")
		}
		previousPage = signature
		for orderIndex, order := range orders {
			if sabyTrue(order, "Deleted", "deleted") {
				continue
			}
			rawDate := sabyValue(sabyFirst(order, "ClosedWTZ", "DateWTZ", "OpenedWTZ", "date"))
			if len(rawDate) < 10 {
				continue
			}
			saleDate, err := time.Parse("2006-01-02", rawDate[:10])
			if err != nil || saleDate.Before(from) || saleDate.After(to) {
				continue
			}
			documentID := sabyValue(sabyFirst(order, "UUID", "uuid", "ID", "id", "Number", "number"))
			if documentID == "" {
				documentID = fmt.Sprintf("%s:%d:%d", saleDate.Format("2006-01-02"), page, orderIndex)
			}
			orderReturn := sabyTrue(order, "Return", "return", "IsReturn")
			positions := sabyObjectList(sabyFirst(order, "SaleNomenclatures", "Positions", "positions", "items"))
			for lineIndex, position := range positions {
				if sabyTrue(position, "Refused", "refused", "IsModifier") {
					continue
				}
				externalID := sabyValue(sabyFirst(position,
					"NomenclatureUUID", "NomenclatureID", "Nomenclature",
					"NomenclatureNumber", "NomNumber", "Article", "Barcode"))
				if externalID == "" {
					continue
				}
				quantity, _ := sabyScalarFloat(sabyFirst(position, "Quantity", "quantity"))
				total, _ := sabyScalarFloat(sabyFirst(position, "TotalPrice", "totalPrice", "Total", "total"))
				units := int(math.Round(math.Abs(quantity)))
				if units == 0 && total == 0 {
					continue
				}
				lineID := sabyValue(sabyFirst(position, "UUID", "uuid", "ID", "id", "LineID", "lineId"))
				if lineID == "" {
					lineID = fmt.Sprintf("%s:%d", externalID, lineIndex)
				}
				eventType := "sale"
				if orderReturn || sabyTrue(position, "IsReturn", "Return", "return") {
					eventType = "return"
				}
				records = append(records, procurement.SalesRecord{
					Date:             saleDate,
					ExternalID:       externalID,
					SabyID:           externalID,
					Units:            units,
					GrossRUB:         math.Abs(total),
					SourceEventID:    documentID,
					SourceDocumentID: documentID,
					SourceLineID:     lineID,
					EventType:        eventType,
					EventStatus:      "confirmed",
				})
			}
		}
		if len(orders) < pageSize {
			break
		}
		if page == 999 {
			return nil, errors.New("Saby превысил предел страниц продаж")
		}
	}
	return records, nil
}

func sabyFirst(record map[string]any, names ...string) any {
	for _, name := range names {
		if value, ok := record[name]; ok && !emptySabyValue(value) {
			return value
		}
	}
	for key, value := range record {
		for _, name := range names {
			if strings.EqualFold(key, name) && !emptySabyValue(value) {
				return value
			}
		}
	}
	return nil
}

func sabyTrue(record map[string]any, names ...string) bool {
	for _, name := range names {
		if sabyBool(sabyFirst(record, name)) {
			return true
		}
	}
	return false
}

func sabyObjectList(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				result = append(result, row)
			}
		}
		return result
	case []map[string]any:
		return typed
	case map[string]any:
		for _, key := range []string{"items", "orders", "sales", "result", "data"} {
			if rows := sabyObjectList(typed[key]); len(rows) > 0 {
				return rows
			}
		}
		if len(typed) > 0 {
			return []map[string]any{typed}
		}
	}
	return nil
}

func sabyPageSignature(orders []map[string]any) string {
	if len(orders) == 0 {
		return ""
	}
	first := sabyValue(sabyFirst(orders[0], "UUID", "uuid", "ID", "id", "Number", "number"))
	last := sabyValue(sabyFirst(orders[len(orders)-1], "UUID", "uuid", "ID", "id", "Number", "number"))
	if first == "" && last == "" {
		return ""
	}
	return fmt.Sprintf("%d:%s:%s", len(orders), first, last)
}
