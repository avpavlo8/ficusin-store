package config

import "testing"

func TestOrderRealizationConfigurationGates(t *testing.T) {
	for _, name := range []string{"ORDER_REALIZATION_BUYER_ID", "ORDER_REALIZATION_MODE", "ORDER_REALIZATION_LIVE_ENABLED", "ORDER_REALIZATION_SITE_WAREHOUSE_ID", "ORDER_REALIZATION_NATIVE_WAREHOUSE_ID", "ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID", "ORDER_REALIZATION_ORGANIZATION_ID", "ORDER_REALIZATION_REGULATION_ID"} {
		t.Setenv(name, "")
	}
	cfg := Config{Environment: "production"}
	if err := configureOrderRealization(&cfg); err != nil || cfg.OrderRealization.Mode != "disabled" {
		t.Fatal("must be disabled by default", err)
	}
	t.Setenv("ORDER_REALIZATION_MODE", "mock")
	for _, environment := range []string{"production", "development"} {
		cfg = Config{Environment: environment}
		if configureOrderRealization(&cfg) == nil {
			t.Fatal("mock accepted outside isolated staging", environment)
		}
	}
	cfg = Config{Environment: "staging"}
	if err := configureOrderRealization(&cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORDER_REALIZATION_MODE", "live")
	t.Setenv("ORDER_REALIZATION_LIVE_ENABLED", "true")
	for _, name := range []string{"ORDER_REALIZATION_BUYER_ID", "ORDER_REALIZATION_SITE_WAREHOUSE_ID", "ORDER_REALIZATION_NATIVE_WAREHOUSE_ID", "ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID", "ORDER_REALIZATION_ORGANIZATION_ID", "ORDER_REALIZATION_REGULATION_ID"} {
		t.Setenv(name, "1")
	}
	credentials := Saby{AppClientID: "test", AppSecret: "test", SecretKey: "test", PointID: 1, PriceListID: 1}
	cfg = Config{Environment: "staging", Saby: credentials}
	if configureOrderRealization(&cfg) == nil {
		t.Fatal("live bypassed staging isolation")
	}
	cfg = Config{Environment: "production"}
	if configureOrderRealization(&cfg) == nil {
		t.Fatal("live accepted missing credentials")
	}
	cfg.Saby = credentials
	t.Setenv("ORDER_REALIZATION_LIVE_ENABLED", "false")
	if configureOrderRealization(&cfg) == nil {
		t.Fatal("live accepted missing opt in")
	}
	t.Setenv("ORDER_REALIZATION_LIVE_ENABLED", "true")
	t.Setenv("ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID", "")
	if configureOrderRealization(&cfg) == nil {
		t.Fatal("guessed ledger warehouse accepted")
	}
	t.Setenv("ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID", "284")
	if err := configureOrderRealization(&cfg); err != nil || cfg.OrderRealization.MovementWarehouseID != 284 {
		t.Fatal("verified live configuration unavailable", err)
	}
	t.Setenv("ORDER_REALIZATION_NATIVE_WAREHOUSE_ID", "saby-ryazan-main")
	if configureOrderRealization(&cfg) == nil {
		t.Fatal("synthetic warehouse alias accepted")
	}
}
