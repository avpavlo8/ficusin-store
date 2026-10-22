package admin

import (
	"errors"
	"math"
)

type OrderDiscount struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

func discountMinor(gross int64, discount OrderDiscount) (int64, error) {
	if math.IsNaN(discount.Value) || math.IsInf(discount.Value, 0) || discount.Value < 0 || discount.Value > 100000000 {
		return 0, errors.New("проверьте размер скидки")
	}
	var amount int64
	switch discount.Type {
	case "fixed":
		amount = int64(math.Round(discount.Value * 100))
	case "percent":
		if discount.Value > 100 {
			return 0, errors.New("скидка не может превышать 100%")
		}
		amount = (gross*int64(math.Round(discount.Value*100)) + 5000) / 10000
	default:
		return 0, errors.New("выберите скидку в рублях или процентах")
	}
	if amount > gross {
		return 0, errors.New("скидка не может быть больше стоимости товаров")
	}
	return amount, nil
}
