package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

// salesLinkService — узкое дополнение к procurementService: разбор продаж,
// которым не нашлось товара.
//
// Отдельным интерфейсом с приведением типа, чтобы не расширять
// procurementService: его реализует ещё и заглушка в тестах, и каждый новый
// метод там стоит правки, никак с задачей не связанной.
type salesLinkService interface {
	UnlinkedSales(context.Context, string, ...bool) ([]procurement.UnlinkedSale, error)
	SearchLinkableNomenclature(context.Context, string) ([]procurement.NomenclatureCandidate, error)
	LinkSalesProduct(context.Context, procurement.Actor, procurement.SalesLink) (procurement.SalesLinkResult, error)
	IgnoreSalesProduct(context.Context, procurement.Actor, string, string, bool) error
}

// salesLinking отвечает, умеет ли текущая сборка разбирать продажи руками.
func (handlers procurementHandlers) salesLinking(response http.ResponseWriter) (salesLinkService, bool) {
	service, able := handlers.service.(salesLinkService)
	if !able {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{
			Error: "Разбор продаж пока недоступен",
		})
		return nil, false
	}
	return service, true
}

func (handlers procurementHandlers) unlinkedSales(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	service, able := handlers.salesLinking(response)
	if !able {
		return
	}
	items, err := service.UnlinkedSales(request.Context(), request.URL.Query().Get("channel"), request.URL.Query().Get("ignored") == "1")
	if err != nil {
		handlers.failed(response, "list unlinked procurement sales", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (handlers procurementHandlers) ignoreSales(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok { return }
	service, able := handlers.salesLinking(response)
	if !able { return }
	var input struct { Channel string `json:"channel"`; ExternalID string `json:"externalId"`; Ignored bool `json:"ignored"` }
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный товар для исключения"})
		return
	}
	if err := service.IgnoreSalesProduct(request.Context(), procurement.Actor{CustomerID: actor.CustomerID, Role: actor.Role}, input.Channel, input.ExternalID, input.Ignored); err != nil {
		handlers.failed(response, "ignore procurement sales", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true})
}

// linkableNomenclature — поиск товара для связывания.
//
// Отдельно от общего поиска по справочнику: здесь не показываются позиции,
// пропавшие из выгрузки СБИС. Выбрать такую — значит приписать продажи
// карточке, которой в магазине уже нет.
func (handlers procurementHandlers) linkableNomenclature(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementRead); !ok {
		return
	}
	service, able := handlers.salesLinking(response)
	if !able {
		return
	}
	items, err := service.SearchLinkableNomenclature(request.Context(), request.URL.Query().Get("q"))
	if err != nil {
		handlers.failed(response, "search linkable procurement nomenclature", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (handlers procurementHandlers) linkSales(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.admin.authorize(response, request, admin.PermissionProcurementEdit)
	if !ok {
		return
	}
	service, able := handlers.salesLinking(response)
	if !able {
		return
	}
	var input procurement.SalesLink
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная связь продажи с товаром"})
		return
	}
	item, err := service.LinkSalesProduct(request.Context(), procurement.Actor{
		CustomerID: actor.CustomerID, Role: actor.Role,
	}, input)
	// Общий текст «Поставщик или закупка не найдены» здесь врёт: не нашёлся
	// товар в номенклатуре, и человеку нужно вернуться к поиску, а не искать
	// пропавшую закупку.
	if errors.Is(err, procurement.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, errorResponse{Error: "Товар не найден в номенклатуре СБИС"})
		return
	}
	if err != nil {
		handlers.failed(response, "link procurement sales", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"link": item})
}
