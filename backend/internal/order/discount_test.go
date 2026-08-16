package order

import "testing"

// Персональная скидка лежала в базе, показывалась покупателю в кабинете
// словами «персональная скидка N%» и правилась владельцем — но к цене не
// применялась нигде. Считаем в копейках: рубли с плавающей точкой дают
// расхождение с чеком ЮKassa на копейку, а сверять чек будет бухгалтер.
func TestDiscountedMinor(t *testing.T) {
	tests := []struct {
		name  string
		price int64
		bps   int
		want  int64
	}{
		{name: "без скидки цена не меняется", price: 149000, bps: 0, want: 149000},
		{name: "отрицательная скидка не поднимает цену", price: 149000, bps: -500, want: 149000},
		{name: "пять процентов", price: 149000, bps: 500, want: 141550},
		{name: "десять процентов", price: 149000, bps: 1000, want: 134100},
		// 1490.00 минус 3.33% = 1440.383, округляем до 1440.38.
		{name: "округление до копейки вверх по половине", price: 149000, bps: 333, want: 144038},
		{name: "копеечный товар не уходит в минус", price: 1, bps: 5000, want: 1},
		// Опечатка в панели: 9000 вместо 900. Отдать растение даром нельзя.
		{name: "скидка ограничена девяноста процентами", price: 149000, bps: 9900, want: 14900},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := discountedMinor(test.price, test.bps); got != test.want {
				t.Fatalf("discountedMinor(%d, %d) = %d, ожидали %d", test.price, test.bps, got, test.want)
			}
		})
	}
}

// Скидка не должна съедать больше, чем обещано: цена после скидки всегда
// меньше исходной, но больше нуля.
func TestDiscountNeverExceedsPrice(t *testing.T) {
	for bps := 1; bps <= 9000; bps += 137 {
		got := discountedMinor(149000, bps)
		if got <= 0 || got > 149000 {
			t.Fatalf("скидка %d bps дала цену %d", bps, got)
		}
	}
}
