package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type OrderRealization struct {
	BuyerID             int64
	Mode                string
	SiteWarehouseID     int64
	NativeWarehouseID   int64
	MovementWarehouseID int64
	OrganizationID      int64
	RegulationID        int64
}

func configureOrderRealization(cfg *Config) error {
	c := OrderRealization{Mode: defaultString(strings.TrimSpace(os.Getenv("ORDER_REALIZATION_MODE")), "disabled")}
	if c.Mode != "disabled" && c.Mode != "mock" && c.Mode != "live" {
		return errors.New("ORDER_REALIZATION_MODE must be disabled, mock or live")
	}
	if c.Mode == "mock" && cfg.Environment != "staging" {
		return errors.New("mock order realization requires isolated APP_ENV=staging")
	}
	for name, destination := range map[string]*int64{
		"ORDER_REALIZATION_BUYER_ID":              &c.BuyerID,
		"ORDER_REALIZATION_SITE_WAREHOUSE_ID":     &c.SiteWarehouseID,
		"ORDER_REALIZATION_NATIVE_WAREHOUSE_ID":   &c.NativeWarehouseID,
		"ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID": &c.MovementWarehouseID,
		"ORDER_REALIZATION_ORGANIZATION_ID":       &c.OrganizationID,
		"ORDER_REALIZATION_REGULATION_ID":         &c.RegulationID,
	} {
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" {
			continue
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return errors.New(name + " must be a positive verified identifier")
		}
		*destination = n
	}
	if c.Mode == "live" {
		if cfg.Environment != "production" || os.Getenv("ORDER_REALIZATION_LIVE_ENABLED") != "true" {
			return errors.New("live order realization requires production and ORDER_REALIZATION_LIVE_ENABLED=true")
		}
		if cfg.Saby.AppClientID == "" || cfg.Saby.AppSecret == "" || cfg.Saby.SecretKey == "" || cfg.Saby.PointID <= 0 || cfg.Saby.PriceListID <= 0 {
			return errors.New("live order realization requires configured Saby credentials")
		}
		if c.BuyerID <= 0 || c.SiteWarehouseID <= 0 || c.NativeWarehouseID <= 0 || c.MovementWarehouseID <= 0 || c.OrganizationID <= 0 || c.RegulationID <= 0 {
			return errors.New("live order realization requires explicit verified buyer, site, native and movement warehouse, organization and regulation identifiers")
		}
	}
	cfg.OrderRealization = c
	return nil
}
