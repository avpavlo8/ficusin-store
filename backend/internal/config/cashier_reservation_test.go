package config

import "testing"

func TestCashierReservationRequiresExplicitVerifiedProductionCutover(t *testing.T) {
	for _, name := range []string{"SABY_CASHIER_RESERVE_MODE", "SABY_CASHIER_RESERVE_LIVE_ENABLED", "SABY_CASHIER_RESERVE_WAREHOUSE_ID", "SABY_CASHIER_RESERVE_MOVEMENT_WAREHOUSE_ID", "SABY_CASHIER_RESERVE_ORGANIZATION_ID", "SABY_CASHIER_RESERVE_REGULATION_ID"} {
		t.Setenv(name, "")
	}
	var cfg Config
	if err := configureCashierReservation(&cfg); err != nil || cfg.CashierReservation.Mode != "disabled" {
		t.Fatal("default unexpectedly enables native writes", err)
	}
	t.Setenv("SABY_CASHIER_RESERVE_MODE", "live")
	if configureCashierReservation(&cfg) == nil {
		t.Fatal("live mode allowed outside production")
	}
	cfg.Environment = "production"
	t.Setenv("SABY_CASHIER_RESERVE_LIVE_ENABLED", "true")
	if configureCashierReservation(&cfg) == nil {
		t.Fatal("missing credentials allowed")
	}
	cfg.Saby.AppClientID = "fixture"
	cfg.Saby.AppSecret = "fixture"
	cfg.Saby.SecretKey = "fixture"
	cfg.Saby.PointID = 278
	cfg.Saby.PriceListID = 19
	if configureCashierReservation(&cfg) == nil {
		t.Fatal("unverified native identities allowed")
	}
	for name, value := range map[string]string{"SABY_CASHIER_RESERVE_WAREHOUSE_ID": "283", "SABY_CASHIER_RESERVE_MOVEMENT_WAREHOUSE_ID": "284", "SABY_CASHIER_RESERVE_ORGANIZATION_ID": "259", "SABY_CASHIER_RESERVE_REGULATION_ID": "121326"} {
		t.Setenv(name, value)
	}
	if err := configureCashierReservation(&cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Environment = "staging"
	if configureCashierReservation(&cfg) == nil {
		t.Fatal("staging allowed live inventory reservations")
	}
}
