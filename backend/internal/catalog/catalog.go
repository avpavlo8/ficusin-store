package catalog

import "context"

type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Latin    string  `json:"latin"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
	Image    string  `json:"image"`
	Light    string  `json:"light"`
	Size     string  `json:"size"`
	Stock    int     `json:"stock"`
}

type Repository interface {
	ListAvailable(context.Context) ([]Product, error)
}
