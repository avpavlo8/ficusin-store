package catalog

import (
	"testing"
	"time"
)

func TestPopularityWeightUsesOnlyRealSalesAndDecays(t *testing.T) {
	tests := []struct {
		name                       string
		age                        time.Duration
		completed, paid, cancelled bool
		want                       float64
	}{
		{"unconfirmed", time.Hour, false, false, false, 0},
		{"cancelled paid", time.Hour, false, true, true, 0},
		{"recent paid", 10 * 24 * time.Hour, false, true, false, 1},
		{"quarter completed", 60 * 24 * time.Hour, true, false, false, .5},
		{"year", 200 * 24 * time.Hour, true, false, false, .2},
		{"historic", 500 * 24 * time.Hour, true, false, false, .05},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PopularityWeight(test.age, test.completed, test.paid, test.cancelled); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
