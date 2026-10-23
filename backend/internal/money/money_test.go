package money

import (
	"math"
	"testing"
)

func TestDecimalKopecks(t *testing.T) {
	for _, tt := range []struct {
		in   float64
		want int64
	}{{1.005, 101}, {2.675, 268}, {-1.005, -101}, {0.29, 29}, {999999999999.99, 99999999999999}} {
		got, err := Minor(tt.in)
		if err != nil || got != tt.want {
			t.Fatalf("%v: %d %v", tt.in, got, err)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), 1e14} {
		if _, err := Minor(v); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	if _, err := Exact(1.005); err == nil {
		t.Fatal("fractional kopeck accepted")
	}
}
func TestProviderDecimal(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "1e3", "1.001", "", " 1.00", "1/2", "999999999999999999"} {
		if _, err := Parse(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	for _, v := range []int64{0, 1, 29, 101, -1, MaxMinor} {
		got, err := Parse(Format(v))
		if err != nil || got != v {
			t.Fatalf("roundtrip %d: %d %v", v, got, err)
		}
	}
}
