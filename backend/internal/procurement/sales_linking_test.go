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
	channel string
	limit   int
	links   []SalesLink
}

func (stub *salesLinkStoreStub) ListUnlinkedSales(_ context.Context, channel string, limit int) ([]UnlinkedSale, error) {
	stub.channel, stub.limit = channel, limit
	return []UnlinkedSale{{Channel: channel, ExternalID: "fikus-benjamina-12", Units: 4}}, nil
}

func (stub *salesLinkStoreStub) LinkSalesProduct(_ context.Context, _ Actor, input SalesLink) (SalesLinkResult, error) {
	stub.links = append(stub.links, input)
	return SalesLinkResult{
		Channel: input.Channel, ExternalID: input.ExternalID, SabyID: input.SabyID, LinkedRows: 3,
	}, nil
}

// plainStoreStub — хранилище прежнего образца, без разбора продаж.
type plainStoreStub struct{ Store }

func TestUnlinkedSalesOnlyForChannelsWithForeignCodes(t *testing.T) {
	t.Parallel()
	service := NewService(&salesLinkStoreStub{})
	// Сайт и СБИС кладут в продажи сам saby_id, связывать там нечего.
	for _, channel := range []string{"", "site", "saby", "avito"} {
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

func TestSalesLinkingNeedsCapableStore(t *testing.T) {
	t.Parallel()
	service := NewService(&plainStoreStub{})
	if _, err := service.UnlinkedSales(context.Background(), "ozon"); !errors.Is(err, ErrSalesLinkUnsupported) {
		t.Fatalf("list err = %v, want %v", err, ErrSalesLinkUnsupported)
	}
	_, err := service.LinkSalesProduct(context.Background(), Actor{}, SalesLink{
		Channel: "ozon", ExternalID: "fikus-12", SabyID: "S-1",
	})
	if !errors.Is(err, ErrSalesLinkUnsupported) {
		t.Fatalf("link err = %v, want %v", err, ErrSalesLinkUnsupported)
	}
}
