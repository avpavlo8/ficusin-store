package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

// salesLinkStub — сервис закупок, который умеет ещё и разбирать продажи.
// Обычные методы берутся у procurementStub, чтобы тесты разбора не зависели
// от остального раздела.
type salesLinkStub struct {
	procurementStub
	channels []string
	links    []procurement.SalesLink
}

func (stub *salesLinkStub) UnlinkedSales(_ context.Context, channel string) ([]procurement.UnlinkedSale, error) {
	stub.channels = append(stub.channels, channel)
	return []procurement.UnlinkedSale{{
		Channel: channel, ExternalID: "fikus-benjamina-12", Days: 3,
		Units: 7, GrossRUB: 4900, LastSale: "2026-08-10",
	}}, nil
}

func (stub *salesLinkStub) LinkSalesProduct(_ context.Context, _ procurement.Actor, input procurement.SalesLink) (procurement.SalesLinkResult, error) {
	stub.links = append(stub.links, input)
	return procurement.SalesLinkResult{
		Channel: input.Channel, ExternalID: input.ExternalID, SabyID: input.SabyID,
		SabyName: "Фикус Бенджамина D12", LinkedRows: 5, LinkedUnits: 7, Remaining: 12,
	}, nil
}

func TestUnlinkedSalesListReachesTheManager(t *testing.T) {
	t.Parallel()
	service := &salesLinkStub{}
	request := adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/unlinked?channel=ozon", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(service.channels) != 1 || service.channels[0] != "ozon" {
		t.Fatalf("channels = %+v", service.channels)
	}
	if !strings.Contains(response.Body.String(), `"externalId":"fikus-benjamina-12"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestSalesLinkCarriesTheDecisionToTheService(t *testing.T) {
	t.Parallel()
	service := &salesLinkStub{}
	request := adminRequest(
		http.MethodPost, "/api/v1/admin/procurement/sales/link",
		`{"channel":"ozon","externalId":"fikus-benjamina-12","sabyId":"S-1"}`,
	)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(service.links) != 1 || service.links[0].SabyID != "S-1" || service.links[0].ExternalID != "fikus-benjamina-12" {
		t.Fatalf("links = %+v", service.links)
	}
	// Экран показывает, сколько продаж вернулось в расчёт, поэтому счётчики
	// обязаны доезжать до браузера.
	if !strings.Contains(response.Body.String(), `"linkedRows":5`) ||
		!strings.Contains(response.Body.String(), `"remaining":12`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestSalesLinkAnswersHonestlyWithoutSupport(t *testing.T) {
	t.Parallel()
	router := NewRouter(discardLogger(), procurementDependencies(&procurementStub{}, admin.RoleManager))
	for _, request := range []*http.Request{
		adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/unlinked?channel=ozon", ""),
		adminRequest(http.MethodPost, "/api/v1/admin/procurement/sales/link", `{"channel":"ozon","externalId":"X","sabyId":"S-1"}`),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want %d", request.URL.Path, response.Code, http.StatusServiceUnavailable)
		}
	}
}

func TestSalesLinkClosedForUnknownRole(t *testing.T) {
	t.Parallel()
	request := adminRequest(http.MethodPost, "/api/v1/admin/procurement/sales/link", `{"channel":"ozon","externalId":"X","sabyId":"S-1"}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(&salesLinkStub{}, "unknown")).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
