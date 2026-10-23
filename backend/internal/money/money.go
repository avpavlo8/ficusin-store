// Package money keeps payable RUB amounts in integer kopecks. Ruble float64
// values are compatibility boundaries for existing JSON contracts, never sums.
package money

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

const MaxMinor int64 = 100_000_000_000_000 // 1 trillion RUB; also exactly representable in JSON.

// Decimal interprets the shortest decimal representation, rather than the
// binary float's approximation. Procurement rates and fractional unit costs
// retain their precision until an actual monetary total is rounded.
func Decimal(value float64) *big.Rat {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic("non-finite decimal")
	}
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	if !ok {
		panic("invalid decimal")
	}
	return r
}

// Round rounds a rational to the nearest integer, ties away from zero.
func Round(value *big.Rat) int64 {
	n := new(big.Int).Abs(value.Num())
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, value.Denom(), r)
	if r.Lsh(r, 1).Cmp(value.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if value.Sign() < 0 {
		q.Neg(q)
	}
	if !q.IsInt64() {
		panic("money overflow")
	}
	return q.Int64()
}

func Minor(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > float64(MaxMinor)/100 {
		return 0, errors.New("некорректная денежная сумма")
	}
	return Round(new(big.Rat).Mul(Decimal(value), big.NewRat(100, 1))), nil
}

// Kopecks is for trusted, bounded persisted/catalog values. User/provider
// input must use Exact or Parse first and handle the error.
func Kopecks(value float64) int64 {
	amount, err := Minor(value)
	if err != nil {
		panic(err)
	}
	return amount
}

func Exact(value float64) (int64, error) {
	n, err := Minor(value)
	if err != nil {
		return 0, err
	}
	if Rubles(n) != value {
		return 0, errors.New("сумма должна содержать не больше двух знаков после запятой")
	}
	return n, nil
}

// Parse accepts a finite decimal RUB amount without rounding provider money.
func Parse(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("пустая сумма")
	}
	// No exponent, whitespace, fractions or non-decimal representations.
	dot := false
	digits := 0
	for i, c := range value {
		if i == 0 && c == '-' {
			continue
		}
		if c == '.' && !dot {
			dot = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, errors.New("некорректная сумма")
		}
		digits++
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok || digits == 0 {
		return 0, errors.New("некорректная сумма")
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() || !r.Num().IsInt64() || r.Num().Cmp(big.NewInt(MaxMinor)) > 0 || r.Num().Cmp(big.NewInt(-MaxMinor)) < 0 {
		return 0, errors.New("сумма вне диапазона или содержит доли копейки")
	}
	return r.Num().Int64(), nil
}

func Rubles(minor int64) float64 { return float64(minor) / 100 }
func Format(minor int64) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}
