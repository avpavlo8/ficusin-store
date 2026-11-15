package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type CashierReservation struct {
	Mode                                                           string
	WarehouseID, MovementWarehouseID, OrganizationID, RegulationID int64
	BuyerID                                                        int64
}

func configureCashierReservation(cfg *Config) error {
	c := CashierReservation{Mode: defaultString(strings.TrimSpace(os.Getenv("SABY_CASHIER_RESERVE_MODE")), "disabled")}
	if c.Mode != "disabled" && c.Mode != "live" {
		return errors.New("SABY_CASHIER_RESERVE_MODE must be disabled or live")
	}
	for name, dest := range map[string]*int64{
		"SABY_CASHIER_RESERVE_WAREHOUSE_ID":          &c.WarehouseID,
		"SABY_CASHIER_RESERVE_MOVEMENT_WAREHOUSE_ID": &c.MovementWarehouseID,
		"SABY_CASHIER_RESERVE_ORGANIZATION_ID":       &c.OrganizationID,
		"SABY_CASHIER_RESERVE_REGULATION_ID":         &c.RegulationID,
		"SABY_CASHIER_RESERVE_BUYER_ID":              &c.BuyerID,
	} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 {
			return errors.New(name + " must be a positive verified identifier")
		}
		*dest = n
	}
	if c.Mode == "live" {
		if cfg.Environment != "production" || os.Getenv("SABY_CASHIER_RESERVE_LIVE_ENABLED") != "true" {
			return errors.New("live cashier reserve requires production and SABY_CASHIER_RESERVE_LIVE_ENABLED=true")
		}
		if cfg.Saby.AppClientID == "" || cfg.Saby.AppSecret == "" || cfg.Saby.SecretKey == "" || cfg.Saby.PointID <= 0 || cfg.Saby.PriceListID <= 0 {
			return errors.New("live cashier reserve requires configured Saby credentials")
		}
		if c.WarehouseID <= 0 || c.MovementWarehouseID <= 0 || c.OrganizationID <= 0 || c.RegulationID <= 0 || c.BuyerID <= 0 {
			return errors.New("live cashier reserve requires explicit verified buyer, warehouse, movement, organization and invoice regulation identifiers")
		}
	}
	cfg.CashierReservation = c
	return nil
}
