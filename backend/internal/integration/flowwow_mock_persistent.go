package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This transport never delegates HTTP to the network. The separate mock table
// preserves cards across deployments so existing staging bindings remain usable.
type persistentFlowwowMock struct{ pool *pgxpool.Pool }

func NewPersistentMockFlowwowClient(ctx context.Context, pool *pgxpool.Pool) (*FlowwowClient, error) {
	seed := FlowwowProduct{ProductID: 990000000, OfferID: "staging-flowwow-demo", Name: "Тестовая карточка Flowwow", Description: "Карточка эмулятора СТГ. Реального товара на Flowwow нет.", Price: "100.00", Discount: "0", Type: 1, SubCategoryID: 101, Stock: 1, Images: []string{"https://images.example.test/flowwow-demo.jpg"}, Currency: "RUB", Properties: map[string]any{"height": map[string]any{"val": 30, "measure": 3}, "stock": 1}}
	raw, _ := json.Marshal(seed)
	if _, e := pool.Exec(ctx, `INSERT INTO flowwow_mock_catalog(offer_id,payload) VALUES($1,$2) ON CONFLICT DO NOTHING`, seed.OfferID, raw); e != nil {
		return nil, e
	}
	c := NewFlowwowClient("mock-flowwow", "1")
	c.client.Transport = &persistentFlowwowMock{pool}
	return c, nil
}
func (m *persistentFlowwowMock) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "apis.flowwow.com" {
		return nil, fmt.Errorf("mock Flowwow refuses host")
	}
	tx, e := m.pool.Begin(r.Context())
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(620000000000000000)`); e != nil {
		return nil, e
	}
	memory := &flowwowMockTransport{cards: map[string]FlowwowProduct{}}
	rows, e := tx.Query(r.Context(), `SELECT payload FROM flowwow_mock_catalog ORDER BY offer_id`)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var raw []byte
		var p FlowwowProduct
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			rows.Close()
			return nil, e
		}
		memory.cards[p.OfferID] = p
		if p.ProductID-900000 > memory.next {
			memory.next = p.ProductID - 900000
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	response, e := memory.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	// Categories and product reads leave emulator data untouched.
	switch r.URL.Path {
	case "/apiseller/products/create", "/apiseller/products/update", "/apiseller/products/offersMappings", "/apiseller/prices/put", "/apiseller/stocks/put":
		if _, e = tx.Exec(r.Context(), `DELETE FROM flowwow_mock_catalog`); e != nil {
			return nil, e
		}
		for offer, card := range memory.cards {
			raw, _ := json.Marshal(card)
			if _, e = tx.Exec(r.Context(), `INSERT INTO flowwow_mock_catalog(offer_id,payload) VALUES($1,$2)`, offer, raw); e != nil {
				return nil, e
			}
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		return nil, e
	}
	return response, nil
}
