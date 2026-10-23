package procurement

import (
	"context"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"math/big"

	"github.com/jackc/pgx/v5"
)

// A refundable trolley deposit is cash tied up with the supplier, not the
// consumed cost of this batch. Single-use packaging and rent are consumed.
// Refuse an unexplained package balance rather than silently dropping it.
func supplierPackageCharges(text string, total float64) (expense, deposit float64, err error) {
	single, _ := findMoney(text, `(?i)Single\s+Package\s+([\d .]+,\d{2})`)
	rent, _ := findMoney(text, `(?i)Package\s+Rent\s+([\d .]+,\d{2})`)
	deposit, _ = findMoney(text, `(?i)Returnable\s+Package\s+([\d .]+,\d{2})`)
	expense = money.Rubles(money.Kopecks(single) + money.Kopecks(rent))
	difference := money.Kopecks(expense) + money.Kopecks(deposit) - money.Kopecks(total)
	if difference > 2 || difference < -2 {
		return 0, 0, &UserFacingError{Message: "Не удалось разделить упаковку поставщика на расход и возвратный залог. Проверьте строки Single Package, Package Rent и Returnable Package в PDF."}
	}
	return expense, deposit, nil
}

func loadSupplierCharges(ctx context.Context, tx pgx.Tx, orderID int64, exchangeRate float64) (expense, deposit float64, err error) {
	rows, err := tx.Query(ctx, `SELECT extracted_text,package_total::DOUBLE PRECISION,currency
	 FROM procurement_documents WHERE procurement_order_id=$1 AND superseded_at IS NULL AND parser_kind='holland_packing_list'`, orderID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	expenseDecimal, depositDecimal := new(big.Rat), new(big.Rat)
	for rows.Next() {
		var text, currency string
		var total float64
		if err = rows.Scan(&text, &total, &currency); err != nil {
			return 0, 0, err
		}
		e, d, eErr := supplierPackageCharges(text, total)
		if eErr != nil {
			return 0, 0, eErr
		}
		rate := 1.
		if currency != "RUB" {
			rate = exchangeRate
		}
		expenseDecimal.Add(expenseDecimal, new(big.Rat).Mul(money.Decimal(e), money.Decimal(rate)))
		depositDecimal.Add(depositDecimal, new(big.Rat).Mul(money.Decimal(d), money.Decimal(rate)))
	}
	if err = rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("read supplier package charges: %w", err)
	}
	return money.Rubles(money.Round(expenseDecimal.Mul(expenseDecimal, big.NewRat(100, 1)))), money.Rubles(money.Round(depositDecimal.Mul(depositDecimal, big.NewRat(100, 1)))), nil
}
