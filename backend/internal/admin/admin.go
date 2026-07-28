package admin

import (
	"context"
	"time"
)

type Dashboard struct {
	Products         int           `json:"products"`
	Variants         int           `json:"variants"`
	Orders           int           `json:"orders"`
	Customers        int           `json:"customers"`
	WholesalePending int           `json:"wholesalePending"`
	LastSync         *SyncRun      `json:"lastSync"`
	RecentOrders     []RecentOrder `json:"recentOrders"`
}

type SyncRun struct {
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"startedAt"`
	ItemsUpdated int       `json:"itemsUpdated"`
	ErrorsCount  int       `json:"errorsCount"`
}

type RecentOrder struct {
	OrderNumber  string    `json:"orderNumber"`
	CustomerName string    `json:"customerName"`
	Total        float64   `json:"total"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Repository interface {
	Dashboard(context.Context) (Dashboard, error)
}
