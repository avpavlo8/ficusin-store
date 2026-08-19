package order

import "testing"

// Ступени скидки — обещание покупателю, а не внутренняя деталь: каждые
// 10 000 ₽ выполненных заказов — один процент, потолок десять процентов.
// Если ступень съедет, магазин обманет людей в ту или другую сторону.
func TestDiscountBPS(t *testing.T) {
	tests := []struct {
		name  string
		spend int64
		want  int
	}{
		{name: "новый покупатель — без скидки", spend: 0, want: 0},
		{name: "почти до первой ступени", spend: 999_999, want: 0},
		{name: "10 000 ₽ — один процент", spend: 1_000_000, want: 100},
		{name: "19 999 ₽ — всё ещё один", spend: 1_999_900, want: 100},
		{name: "20 000 ₽ — два", spend: 2_000_000, want: 200},
		{name: "30 000 ₽ — три", spend: 3_000_000, want: 300},
		{name: "100 000 ₽ — десять", spend: 10_000_000, want: 1000},
		{name: "выше потолка скидка не растёт", spend: 500_000_000, want: 1000},
		{name: "отрицательная сумма не даёт скидки", spend: -100, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DiscountBPS(test.spend); got != test.want {
				t.Fatalf("DiscountBPS(%d) = %d, ожидали %d", test.spend, got, test.want)
			}
		})
	}
}

// Скидка обязана расти вместе с суммой и никогда не падать по дороге:
// покупатель, потративший больше, не может получить меньше.
func TestDiscountNeverFallsWithSpend(t *testing.T) {
	previous := 0
	for spend := int64(0); spend <= 15_000_000; spend += 137_000 {
		got := DiscountBPS(spend)
		if got < previous {
			t.Fatalf("при сумме %d скидка упала с %d до %d", spend, previous, got)
		}
		if got > 1000 {
			t.Fatalf("при сумме %d скидка %d превысила потолок", spend, got)
		}
		previous = got
	}
}
