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

// Record sales review calls independently of the other procurement endpoints.
type salesLinkStub struct {
	procurementStub
	channels        []string
	ignoredChannels []string
	ignoredInputs   []ignoredSalesDecision
	queries         []string
	links           []procurement.SalesLink
}

type ignoredSalesDecision struct {
	actor               procurement.Actor
	channel, externalID string
	ignored             bool
}

func (stub *salesLinkStub) UnlinkedSalesWithIgnored(_ context.Context, channel string) ([]procurement.UnlinkedSale, error) {
	stub.ignoredChannels = append(stub.ignoredChannels, channel)
	return []procurement.UnlinkedSale{{Channel: channel, ExternalID: "ignored-offer", Ignored: true}}, nil
}

func (stub *salesLinkStub) IgnoreSalesProduct(_ context.Context, actor procurement.Actor, channel, externalID string, ignored bool) error {
	stub.ignoredInputs = append(stub.ignoredInputs, ignoredSalesDecision{actor, channel, externalID, ignored})
	return nil
}

func (stub *salesLinkStub) UnlinkedSales(_ context.Context, channel string) ([]procurement.UnlinkedSale, error) {
	stub.channels = append(stub.channels, channel)
	return []procurement.UnlinkedSale{{
		Channel: channel, ExternalID: "1851256804", Article: "muholovka",
		Name: "Венерина мухоловка", Days: 3, Units: 7, GrossRUB: 4900, LastSale: "2026-08-10",
	}}, nil
}

func (stub *salesLinkStub) SearchLinkableNomenclature(_ context.Context, query string) ([]procurement.NomenclatureCandidate, error) {
	stub.queries = append(stub.queries, query)
	return []procurement.NomenclatureCandidate{{SabyID: "S-1", Name: "Венерина мухоловка со стволом"}}, nil
}

func (stub *salesLinkStub) LinkSalesProduct(_ context.Context, _ procurement.Actor, input procurement.SalesLink) (procurement.SalesLinkResult, error) {
	stub.links = append(stub.links, input)
	return procurement.SalesLinkResult{
		Channel: input.Channel, ExternalID: input.ExternalID, SabyID: "S-1",
		SabyName: "Фикус Бенджамина D12", LinkedRows: 5, LinkedUnits: 7, Remaining: 12,
	}, nil
}

func TestUnlinkedSalesListReachesTheOwner(t *testing.T) {
	t.Parallel()
	service := &salesLinkStub{}
	request := adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/unlinked?channel=wb", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleOwner)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(service.channels) != 1 || service.channels[0] != "wb" {
		t.Fatalf("channels = %+v", service.channels)
	}
	// У Wildberries внешний код числовой, и без подписи карточки строка
	// ничего не говорит человеку — она обязана доезжать до браузера.
	if !strings.Contains(response.Body.String(), `"externalId":"1851256804"`) ||
		!strings.Contains(response.Body.String(), `"name":"Венерина мухоловка"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestLinkableNomenclatureSearchReachesTheService(t *testing.T) {
	t.Parallel()
	service := &salesLinkStub{}
	request := adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/nomenclature?q=мухоловка", "")
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleOwner)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(service.queries) != 1 || service.queries[0] != "мухоловка" {
		t.Fatalf("queries = %+v", service.queries)
	}
	// Общий поиск по справочнику остаётся на своём месте и в разбор продаж
	// не зовётся: он показывает и пропавшие из выгрузки позиции.
	if len(service.searchInputs) != 0 {
		t.Fatalf("directory search was used: %+v", service.searchInputs)
	}
}

func TestSalesLinkCarriesTheDecisionToTheService(t *testing.T) {
	t.Parallel()
	service := &salesLinkStub{}
	request := adminRequest(
		http.MethodPost, "/api/v1/admin/procurement/sales/link",
		`{"channel":"ozon","externalId":"fikus-benjamina-12","variantId":42}`,
	)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleOwner)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if len(service.links) != 1 || service.links[0].VariantID != 42 || service.links[0].ExternalID != "fikus-benjamina-12" {
		t.Fatalf("links = %+v", service.links)
	}
	// Экран показывает, сколько продаж вернулось в расчёт, поэтому счётчики
	// обязаны доезжать до браузера.
	if !strings.Contains(response.Body.String(), `"linkedRows":5`) ||
		!strings.Contains(response.Body.String(), `"remaining":12`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestSalesLinkRejectsRetiredRemoteIdentityInput(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"channel":"ozon","externalId":"fikus-12","sabyId":"S-1"}`, `{"channel":"ozon","externalId":"fikus-12","variantId":42,"sabyId":"S-1"}`} {
		service := &salesLinkStub{}
		request := adminRequest(http.MethodPost, "/api/v1/admin/procurement/sales/link", body)
		response := httptest.NewRecorder()
		NewRouter(discardLogger(), procurementDependencies(service, admin.RoleOwner)).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || len(service.links) != 0 {
			t.Fatalf("retired remote-id alias reached linking: %d/%v", response.Code, service.links)
		}
	}
}

func TestSalesLinkUnavailableWithoutProcurementService(t *testing.T) {
	t.Parallel()
	router := NewRouter(discardLogger(), procurementDependencies(nil, admin.RoleOwner))
	for _, request := range []*http.Request{
		adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/unlinked?channel=ozon", ""),
		adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/unlinked?channel=ozon&ignored=1", ""),
		adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/nomenclature?q=фикус", ""),
		adminRequest(http.MethodPost, "/api/v1/admin/procurement/sales/link", `{"channel":"ozon","externalId":"X","variantId":42}`),
		adminRequest(http.MethodPut, "/api/v1/admin/procurement/sales/ignore", `{"channel":"ozon","externalId":"X","ignored":true}`),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want %d", request.URL.Path, response.Code, http.StatusServiceUnavailable)
		}
	}
}

func TestIgnoredSalesFilterAndDecisionUseCurrentService(t *testing.T) {
	t.Parallel()
	service := &salesLinkStub{}
	router := NewRouter(discardLogger(), procurementDependencies(service, admin.RoleOwner))
	for _, query := range []string{"channel=ozon", "channel=ozon&ignored=1"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest(http.MethodGet, "/api/v1/admin/procurement/sales/unlinked?"+query, ""))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, response.Code, response.Body.String())
		}
		if strings.Contains(query, "ignored=1") && !strings.Contains(response.Body.String(), `"ignored":true`) {
			t.Fatal("ignored filter silently fell back to the ordinary list")
		}
	}
	if len(service.channels) != 1 || service.channels[0] != "ozon" || len(service.ignoredChannels) != 1 || service.ignoredChannels[0] != "ozon" {
		t.Fatalf("filter routed to wrong list: %v / %v", service.channels, service.ignoredChannels)
	}
	for _, decision := range []string{"true", "false"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, adminRequest(http.MethodPut, "/api/v1/admin/procurement/sales/ignore", `{"channel":"ozon","externalId":"offer-42","ignored":`+decision+`}`))
		if response.Code != http.StatusOK {
			t.Fatalf("ignore=%s: %d %s", decision, response.Code, response.Body.String())
		}
	}
	if len(service.ignoredInputs) != 2 || !service.ignoredInputs[0].ignored || service.ignoredInputs[1].ignored {
		t.Fatalf("exclude/restore lost: %+v", service.ignoredInputs)
	}
	for _, input := range service.ignoredInputs {
		if input.channel != "ozon" || input.externalID != "offer-42" || input.actor.Role != admin.RoleOwner || input.actor.CustomerID <= 0 {
			t.Fatalf("decision lost identity or actor: %+v", input)
		}
	}
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(service, admin.RoleManager)).ServeHTTP(response, adminRequest(http.MethodPut, "/api/v1/admin/procurement/sales/ignore", `{"channel":"ozon","externalId":"offer-42","ignored":true}`))
	if response.Code != http.StatusForbidden || len(service.ignoredInputs) != 2 {
		t.Fatal("read-only manager changed the sales exclusion")
	}
}

func TestSalesLinkClosedForUnknownRole(t *testing.T) {
	t.Parallel()
	request := adminRequest(http.MethodPost, "/api/v1/admin/procurement/sales/link", `{"channel":"ozon","externalId":"X","variantId":42}`)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), procurementDependencies(&salesLinkStub{}, "unknown")).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
