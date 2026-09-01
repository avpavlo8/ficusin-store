package procurement

import (
	"context"
	"errors"
	"testing"
)

// salesLinkStoreStub — хранилище, которое умеет разбирать продажи руками.
// Остальные методы Store берутся из встроенного интерфейса: сервис их здесь
// не зовёт, а выписывать три десятка заглушек ради этого незачем.
type salesLinkStoreStub struct {
	Store
	channel    string
	limit      int
	links      []SalesLink
	queries    []string
	remembered map[string][]ChannelProduct
	cached     map[string][]ChannelProduct
}

func (stub *salesLinkStoreStub) ListUnlinkedSales(_ context.Context, channel string, limit int) ([]UnlinkedSale, error) {
	stub.channel, stub.limit = channel, limit
	return []UnlinkedSale{{Channel: channel, ExternalID: "fikus-benjamina-12", Units: 4}}, nil
}

func (stub *salesLinkStoreStub) IgnoreSalesProduct(context.Context, Actor, string, string, bool) error { return nil }
func (stub *salesLinkStoreStub) ListUnlinkedSalesIncludingIgnored(ctx context.Context, channel string, limit int) ([]UnlinkedSale, error) {
	return stub.ListUnlinkedSales(ctx, channel, limit)
}

func (stub *salesLinkStoreStub) LinkSalesProduct(_ context.Context, _ Actor, input SalesLink) (SalesLinkResult, error) {
	stub.links = append(stub.links, input)
	return SalesLinkResult{
		Channel: input.Channel, ExternalID: input.ExternalID, SabyID: input.SabyID, LinkedRows: 3,
	}, nil
}

func (stub *salesLinkStoreStub) SearchLinkableNomenclature(_ context.Context, query string) ([]NomenclatureCandidate, error) {
	stub.queries = append(stub.queries, query)
	return []NomenclatureCandidate{{SabyID: "S-1", Name: "Фикус Бенджамина D12"}}, nil
}

func (stub *salesLinkStoreStub) RememberChannelProducts(_ context.Context, channel string, items []ChannelProduct) error {
	if stub.remembered == nil {
		stub.remembered = map[string][]ChannelProduct{}
	}
	stub.remembered[channel] = items
	return nil
}

func (stub *salesLinkStoreStub) CachedChannelProducts(_ context.Context, channel string) ([]ChannelProduct, error) {
	return stub.cached[channel], nil
}

func (stub *salesLinkStoreStub) LinkChannelProducts(_ context.Context, _ Actor, channel string, items []ChannelProduct) (ChannelLinkResult, error) {
	return ChannelLinkResult{Channel: channel, Fetched: len(items), Linked: len(items)}, nil
}

// plainStoreStub — хранилище прежнего образца, без разбора продаж.
type plainStoreStub struct{ Store }

// channelCatalogStub — площадка, которая отдаёт свой справочник карточек.
type channelCatalogStub struct {
	items []ChannelProduct
	calls *int
}

func (channelCatalogStub) Configured(string) bool { return true }
func (channelCatalogStub) Execute(context.Context, ActionItem) (ActionExecution, error) {
	return ActionExecution{}, nil
}
func (stub channelCatalogStub) FetchCatalog(context.Context, string) ([]ChannelProduct, error) {
	if stub.calls != nil {
		(*stub.calls)++
	}
	return stub.items, nil
}

func TestUnlinkedSalesOnlyForChannelsWithForeignCodes(t *testing.T) {
	t.Parallel()
	service := NewService(&salesLinkStoreStub{})
	for _, channel := range []string{"", "site", "avito"} {
		if _, err := service.UnlinkedSales(context.Background(), channel); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("channel %q: err = %v, want %v", channel, err, ErrInvalidInput)
		}
	}
}

func TestUnlinkedSalesAsksStoreForBoundedList(t *testing.T) {
	t.Parallel()
	store := &salesLinkStoreStub{}
	items, err := NewService(store).UnlinkedSales(context.Background(), " ozon ")
	if err != nil || len(items) != 1 || items[0].ExternalID != "fikus-benjamina-12" {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	if store.channel != "ozon" {
		t.Fatalf("channel = %q, want ozon", store.channel)
	}
	// Разбор ручной, но список приходит из базы и без потолка редкий канал
	// с тысячами разовых кодов повесил бы таблицу.
	if store.limit <= 0 {
		t.Fatalf("limit = %d, want positive", store.limit)
	}
}

func TestLinkSalesProductRejectsUnusableDecisions(t *testing.T) {
	t.Parallel()
	service := NewService(&salesLinkStoreStub{})
	cases := map[string]SalesLink{
		"канал без внешних кодов":  {Channel: "site", ExternalID: "X", SabyID: "S1"},
		"пустой внешний код":       {Channel: "ozon", ExternalID: "  ", SabyID: "S1"},
		"пустой товар":             {Channel: "ozon", ExternalID: "fikus-12", SabyID: ""},
		"нечисловой nmID для WB":   {Channel: "wb", ExternalID: "fikus-12", SabyID: "S1"},
		"внешний код длиннее поля": {Channel: "ozon", ExternalID: string(make([]byte, 201)), SabyID: "S1"},
	}
	for name, input := range cases {
		if _, err := service.LinkSalesProduct(context.Background(), Actor{}, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: err = %v, want %v", name, err, ErrInvalidInput)
		}
	}
}

func TestLinkSalesProductPassesTrimmedDecisionToStore(t *testing.T) {
	t.Parallel()
	store := &salesLinkStoreStub{}
	result, err := NewService(store).LinkSalesProduct(context.Background(), Actor{CustomerID: 5}, SalesLink{
		Channel: " ozon ", ExternalID: " fikus-benjamina-12 ", SabyID: " S-1 ",
	})
	if err != nil || result.LinkedRows != 3 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(store.links) != 1 || store.links[0] != (SalesLink{
		Channel: "ozon", ExternalID: "fikus-benjamina-12", SabyID: "S-1",
	}) {
		t.Fatalf("links = %+v", store.links)
	}
}

func TestLinkSalesProductAcceptsNumericWildberriesCode(t *testing.T) {
	t.Parallel()
	store := &salesLinkStoreStub{}
	if _, err := NewService(store).LinkSalesProduct(context.Background(), Actor{}, SalesLink{
		Channel: "wb", ExternalID: "1851256804", SabyID: "S-1",
	}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(store.links) != 1 || store.links[0].ExternalID != "1851256804" {
		t.Fatalf("links = %+v", store.links)
	}
}

func TestLinkSalesProductUsesCanonicalVariantWithoutRemoteIdentity(t *testing.T) {
	t.Parallel()
	store := &salesLinkStoreStub{}
	if _, err := NewService(store).LinkSalesProduct(context.Background(), Actor{}, SalesLink{
		Channel: "ozon", ExternalID: "olive-d12-old", VariantID: 42,
	}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(store.links) != 1 || store.links[0].VariantID != 42 || store.links[0].SabyID != "" {
		t.Fatalf("links = %+v", store.links)
	}
}

func TestLinkableNomenclatureNeedsAMeaningfulQuery(t *testing.T) {
	t.Parallel()
	service := NewService(&salesLinkStoreStub{})
	for _, query := range []string{"", " ", "ф"} {
		if _, err := service.SearchLinkableNomenclature(context.Background(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("query %q: err = %v, want %v", query, err, ErrInvalidInput)
		}
	}
	store := &salesLinkStoreStub{}
	items, err := NewService(store).SearchLinkableNomenclature(context.Background(), "  фикус  ")
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	if len(store.queries) != 1 || store.queries[0] != "фикус" {
		t.Fatalf("queries = %+v", store.queries)
	}
}

// Подпись карточки — единственное, по чему человек узнаёт растение на
// вкладке Wildberries: внешний код там числовой nmID.
func TestChannelCatalogSyncRemembersCardLabels(t *testing.T) {
	t.Parallel()
	items := []ChannelProduct{
		{ExternalID: "1851256804", Article: "muholovka", Name: "Венерина мухоловка"},
	}
	store := &salesLinkStoreStub{cached: map[string][]ChannelProduct{"wb": items}}
	remoteCalls := 0
	source := channelCatalogStub{items: []ChannelProduct{{ExternalID: "must-not-be-read"}}, calls: &remoteCalls}
	result, err := NewServiceWithExecutor(store, source).SyncChannelCatalog(context.Background(), Actor{}, "wb")
	if err != nil || result.Fetched != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	remembered := store.remembered["wb"]
	if len(remembered) != 1 || remembered[0].Name != "Венерина мухоловка" {
		t.Fatalf("remembered = %+v", store.remembered)
	}
	if remoteCalls != 0 {
		t.Fatalf("WB API calls = %d, want 0", remoteCalls)
	}
}

func TestSalesLinkingNeedsCapableStore(t *testing.T) {
	t.Parallel()
	service := NewService(&plainStoreStub{})
	if _, err := service.UnlinkedSales(context.Background(), "ozon"); !errors.Is(err, ErrSalesLinkUnsupported) {
		t.Fatalf("list err = %v, want %v", err, ErrSalesLinkUnsupported)
	}
	if _, err := service.SearchLinkableNomenclature(context.Background(), "фикус"); !errors.Is(err, ErrSalesLinkUnsupported) {
		t.Fatalf("search err = %v, want %v", err, ErrSalesLinkUnsupported)
	}
	_, err := service.LinkSalesProduct(context.Background(), Actor{}, SalesLink{
		Channel: "ozon", ExternalID: "fikus-12", SabyID: "S-1",
	})
	if !errors.Is(err, ErrSalesLinkUnsupported) {
		t.Fatalf("link err = %v, want %v", err, ErrSalesLinkUnsupported)
	}
}
