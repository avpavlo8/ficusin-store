package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type FinanceImport struct {
	ID            int64     `json:"id"`
	Bank          string    `json:"bank"`
	AccountNumber string    `json:"accountNumber"`
	FileName      string    `json:"fileName"`
	Status        string    `json:"status"`
	RowsTotal     int       `json:"rowsTotal"`
	RowsNew       int       `json:"rowsNew"`
	RowsDuplicate int       `json:"rowsDuplicate"`
	RowsReview    int       `json:"rowsReview"`
	CreatedAt     time.Time `json:"createdAt"`
}
type FinanceTransaction struct {
	ID             int64   `json:"id"`
	ImportID       int64   `json:"importId"`
	Bank           string  `json:"bank"`
	AccountNumber  string  `json:"accountNumber"`
	OperationDate  string  `json:"operationDate"`
	DocumentNumber string  `json:"documentNumber"`
	Counterparty   string  `json:"counterparty"`
	Purpose        string  `json:"purpose"`
	Currency       string  `json:"currency"`
	Classification string  `json:"classification"`
	PnlEffect      string  `json:"pnlEffect"`
	ReviewReason   string  `json:"reviewReason"`
	Debit          float64 `json:"debit"`
	Credit         float64 `json:"credit"`
	Confirmed      bool    `json:"confirmed"`
}
type FinanceCashEntry struct {
	ID                  int64   `json:"id"`
	OperationDate       string  `json:"operationDate"`
	Kind                string  `json:"kind"`
	Direction           string  `json:"direction"`
	Purpose             string  `json:"purpose"`
	Amount              float64 `json:"amount"`
	LinkedTransactionID *int64  `json:"linkedTransactionId,omitempty"`
}
type FinanceOverview struct {
	Imports      []FinanceImport      `json:"imports"`
	Transactions []FinanceTransaction `json:"transactions"`
	Cash         []FinanceCashEntry   `json:"cash"`
	Income       float64              `json:"income"`
	Expense      float64              `json:"expense"`
	CashBalance  float64              `json:"cashBalance"`
	ReviewCount  int                  `json:"reviewCount"`
}
type FinanceClassification struct{ Classification, PnlEffect, Reason string }
type FinanceCashInput struct {
	OperationDate, Kind, Direction, Purpose string
	Amount                                  float64
	LinkedTransactionID                     *int64 `json:"linkedTransactionId"`
}
type FinanceReconciliation struct {
	Date    string  `json:"date"`
	Actual  float64 `json:"actual"`
	Comment string  `json:"comment"`
}

func (repository *PostgresRepository) PreviewFinanceImport(ctx context.Context, actor Actor, bank, account, name string, content []byte) (FinanceImport, error) {
	if actor.Role != RoleOwner {
		return FinanceImport{}, ErrForbidden
	}
	bank = strings.ToLower(strings.TrimSpace(bank))
	if bank != "sber" && bank != "tochka" && bank != "ozon" {
		return FinanceImport{}, errors.New("выберите банк")
	}
	if len(content) == 0 || len(content) > 25<<20 {
		return FinanceImport{}, errors.New("файл пустой или больше 25 МБ")
	}
	rows, format, err := ParseFinanceStatement(ctx, name, content)
	if err != nil {
		return FinanceImport{}, err
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return FinanceImport{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var item FinanceImport
	err = tx.QueryRow(ctx, `INSERT INTO finance_imports(bank,account_number,file_name,file_sha256,source_file,source_format,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(bank,account_number,file_sha256) DO UPDATE SET file_name=finance_imports.file_name RETURNING id,bank,account_number,file_name,status,rows_total,rows_new,rows_duplicate,rows_review,created_at`, bank, strings.TrimSpace(account), name, hash, content, format, actor.CustomerID).Scan(&item.ID, &item.Bank, &item.AccountNumber, &item.FileName, &item.Status, &item.RowsTotal, &item.RowsNew, &item.RowsDuplicate, &item.RowsReview, &item.CreatedAt)
	if err != nil {
		return FinanceImport{}, err
	}
	if item.RowsTotal > 0 {
		tx.Commit(ctx)
		return item, nil
	}
	occurrences := map[string]int{}
	for _, row := range rows {
		class, effect, reason := classifyFinance(row)
		base := financeDedupe(row)
		occurrences[base]++
		dedupe := fmt.Sprintf("%s:%d", base, occurrences[base])
		var transactionID int64
		err := tx.QueryRow(ctx, `INSERT INTO finance_transactions(import_id,bank,account_number,source_row,operation_date,document_number,counterparty,counterparty_account,purpose,debit_rub,credit_rub,currency,dedupe_key,classification,pnl_effect,review_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(bank,account_number,dedupe_key) DO NOTHING RETURNING id`, item.ID, bank, account, row.Row, row.Date, row.Number, row.Counterparty, row.CounterpartyAccount, row.Purpose, row.Debit, row.Credit, row.Currency, dedupe, class, effect, reason).Scan(&transactionID)
		item.RowsTotal++
		if errors.Is(err, pgx.ErrNoRows) {
			item.RowsDuplicate++
			continue
		}
		if err != nil {
			return FinanceImport{}, err
		}
		item.RowsNew++
		if class == "review" {
			item.RowsReview++
		}
		if _, err = tx.Exec(ctx, `INSERT INTO finance_classification_versions(transaction_id,version,classification,pnl_effect,reason,changed_by) VALUES($1,1,$2,$3,$4,$5)`, transactionID, class, effect, reason, actor.CustomerID); err != nil {
			return FinanceImport{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE finance_imports SET rows_total=$2,rows_new=$3,rows_duplicate=$4,rows_review=$5 WHERE id=$1`, item.ID, item.RowsTotal, item.RowsNew, item.RowsDuplicate, item.RowsReview)
	if err != nil {
		return FinanceImport{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return FinanceImport{}, err
	}
	return item, nil
}

func (repository *PostgresRepository) ConfirmFinanceImport(ctx context.Context, actor Actor, id int64) (FinanceImport, error) {
	if actor.Role != RoleOwner {
		return FinanceImport{}, ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return FinanceImport{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var changed int64
	if err = tx.QueryRow(ctx, `UPDATE finance_imports SET status='confirmed',confirmed_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='preview' RETURNING id`, id).Scan(&changed); errors.Is(err, pgx.ErrNoRows) {
		return FinanceImport{}, errors.New("импорт не найден или уже подтверждён")
	}
	if err != nil {
		return FinanceImport{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE finance_transactions SET confirmed=TRUE WHERE import_id=$1`, id); err != nil {
		return FinanceImport{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return FinanceImport{}, err
	}
	return repository.financeImport(ctx, id)
}

func (repository *PostgresRepository) ClassifyFinanceTransaction(ctx context.Context, actor Actor, id int64, input FinanceClassification) (FinanceTransaction, error) {
	if actor.Role != RoleOwner {
		return FinanceTransaction{}, ErrForbidden
	}
	valid := map[string]bool{"sale": true, "marketplace_payout": true, "acquiring": true, "commission": true, "supplier": true, "tax": true, "own_transfer": true, "loan_principal": true, "loan_interest": true, "owner_contribution": true, "owner_withdrawal": true, "packaging_material": true, "other": true, "review": true}
	if !valid[input.Classification] || (input.PnlEffect != "income" && input.PnlEffect != "expense" && input.PnlEffect != "none" && input.PnlEffect != "review") {
		return FinanceTransaction{}, errors.New("неизвестная статья")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return FinanceTransaction{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var version int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM finance_classification_versions WHERE transaction_id=$1`, id).Scan(&version); err != nil {
		return FinanceTransaction{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO finance_classification_versions(transaction_id,version,classification,pnl_effect,reason,changed_by) VALUES($1,$2,$3,$4,$5,$6)`, id, version, input.Classification, input.PnlEffect, strings.TrimSpace(input.Reason), actor.CustomerID); err != nil {
		return FinanceTransaction{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE finance_transactions SET classification=$2,pnl_effect=$3,review_reason=$4 WHERE id=$1`, id, input.Classification, input.PnlEffect, strings.TrimSpace(input.Reason)); err != nil {
		return FinanceTransaction{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return FinanceTransaction{}, err
	}
	return repository.financeTransaction(ctx, id)
}

func (repository *PostgresRepository) CreateFinanceCash(ctx context.Context, actor Actor, input FinanceCashInput) (FinanceCashEntry, error) {
	if actor.Role != RoleOwner {
		return FinanceCashEntry{}, ErrForbidden
	}
	date, err := time.Parse("2006-01-02", input.OperationDate)
	if err != nil || input.Amount <= 0 {
		return FinanceCashEntry{}, errors.New("проверьте дату и сумму")
	}
	if date.Before(time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)) {
		return FinanceCashEntry{}, errors.New("бизнес-учёт начинается 1 сентября 2026")
	}
	validKind := map[string]bool{"opening": true, "saby_sale": true, "expense": true, "bank_transfer": true, "adjustment": true}
	if !validKind[input.Kind] || (input.Direction != "in" && input.Direction != "out") {
		return FinanceCashEntry{}, errors.New("неизвестный тип кассовой операции")
	}
	if input.Kind == "bank_transfer" && (input.Direction != "out" || input.LinkedTransactionID == nil) {
		return FinanceCashEntry{}, errors.New("для внесения в банк выберите связанное поступление")
	}
	if input.Kind != "bank_transfer" && input.LinkedTransactionID != nil {
		return FinanceCashEntry{}, errors.New("связать банковскую операцию можно только с внесением наличных")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return FinanceCashEntry{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if input.LinkedTransactionID != nil {
		var credit float64
		var confirmed bool
		if err = tx.QueryRow(ctx, `SELECT credit_rub::double precision,confirmed FROM finance_transactions WHERE id=$1 FOR UPDATE`, *input.LinkedTransactionID).Scan(&credit, &confirmed); errors.Is(err, pgx.ErrNoRows) {
			return FinanceCashEntry{}, errors.New("банковская операция не найдена")
		}
		if err != nil {
			return FinanceCashEntry{}, err
		}
		if !confirmed || credit != input.Amount {
			return FinanceCashEntry{}, errors.New("сумма должна совпадать с подтверждённым поступлением")
		}
		if _, err = tx.Exec(ctx, `UPDATE finance_transactions SET classification='own_transfer',pnl_effect='none',review_reason='' WHERE id=$1`, *input.LinkedTransactionID); err != nil {
			return FinanceCashEntry{}, err
		}
		var version int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM finance_classification_versions WHERE transaction_id=$1`, *input.LinkedTransactionID).Scan(&version); err != nil {
			return FinanceCashEntry{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO finance_classification_versions(transaction_id,version,classification,pnl_effect,reason,changed_by) VALUES($1,$2,'own_transfer','none','Связано с внесением наличных в банк',$3)`, *input.LinkedTransactionID, version, actor.CustomerID); err != nil {
			return FinanceCashEntry{}, err
		}
	}
	var item FinanceCashEntry
	err = tx.QueryRow(ctx, `INSERT INTO finance_cash_entries(operation_date,kind,direction,amount_rub,purpose,linked_transaction_id,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,operation_date::text,kind,direction,amount_rub::double precision,purpose,linked_transaction_id`, date, input.Kind, input.Direction, input.Amount, strings.TrimSpace(input.Purpose), input.LinkedTransactionID, actor.CustomerID).Scan(&item.ID, &item.OperationDate, &item.Kind, &item.Direction, &item.Amount, &item.Purpose, &item.LinkedTransactionID)
	if err != nil {
		return FinanceCashEntry{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return FinanceCashEntry{}, err
	}
	return item, nil
}

func (repository *PostgresRepository) ReconcileFinanceCash(ctx context.Context, actor Actor, input FinanceReconciliation) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil || input.Actual < 0 {
		return errors.New("проверьте дату и фактический остаток")
	}
	if date.Before(time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)) {
		return errors.New("бизнес-учёт начинается 1 сентября 2026")
	}
	var expected float64
	if err = repository.pool.QueryRow(ctx, `SELECT COALESCE(SUM(CASE direction WHEN 'in' THEN amount_rub ELSE -amount_rub END),0)::double precision FROM finance_cash_entries WHERE operation_date<=$1`, date).Scan(&expected); err != nil {
		return err
	}
	_, err = repository.pool.Exec(ctx, `INSERT INTO finance_cash_reconciliations(reconciliation_date,expected_rub,actual_rub,comment,created_by) VALUES($1,$2,$3,$4,$5)`, date, expected, input.Actual, strings.TrimSpace(input.Comment), actor.CustomerID)
	return err
}

func (repository *PostgresRepository) FinanceOverview(ctx context.Context) (FinanceOverview, error) {
	var out FinanceOverview
	rows, err := repository.pool.Query(ctx, `SELECT id,bank,account_number,file_name,status,rows_total,rows_new,rows_duplicate,rows_review,created_at FROM finance_imports ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var x FinanceImport
		if err = rows.Scan(&x.ID, &x.Bank, &x.AccountNumber, &x.FileName, &x.Status, &x.RowsTotal, &x.RowsNew, &x.RowsDuplicate, &x.RowsReview, &x.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Imports = append(out.Imports, x)
	}
	rows.Close()
	rows, err = repository.pool.Query(ctx, `SELECT id,import_id,bank,account_number,operation_date::text,document_number,counterparty,purpose,currency,debit_rub::double precision,credit_rub::double precision,classification,pnl_effect,review_reason,confirmed FROM finance_transactions ORDER BY operation_date DESC,id DESC LIMIT 300`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var x FinanceTransaction
		if err = rows.Scan(&x.ID, &x.ImportID, &x.Bank, &x.AccountNumber, &x.OperationDate, &x.DocumentNumber, &x.Counterparty, &x.Purpose, &x.Currency, &x.Debit, &x.Credit, &x.Classification, &x.PnlEffect, &x.ReviewReason, &x.Confirmed); err != nil {
			rows.Close()
			return out, err
		}
		out.Transactions = append(out.Transactions, x)
		current := x.OperationDate >= "2026-09-01"
		if current && x.Classification == "review" {
			out.ReviewCount++
		}
		if current && x.Confirmed && x.PnlEffect == "income" {
			out.Income += x.Credit - x.Debit
		}
		if current && x.Confirmed && x.PnlEffect == "expense" {
			out.Expense += x.Debit - x.Credit
		}
	}
	rows.Close()
	rows, err = repository.pool.Query(ctx, `SELECT id,operation_date::text,kind,direction,amount_rub::double precision,purpose,linked_transaction_id FROM finance_cash_entries ORDER BY operation_date DESC,id DESC LIMIT 100`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var x FinanceCashEntry
		if err = rows.Scan(&x.ID, &x.OperationDate, &x.Kind, &x.Direction, &x.Amount, &x.Purpose, &x.LinkedTransactionID); err != nil {
			rows.Close()
			return out, err
		}
		out.Cash = append(out.Cash, x)
		if x.Direction == "in" {
			out.CashBalance += x.Amount
		} else {
			out.CashBalance -= x.Amount
		}
	}
	rows.Close()
	return out, rows.Err()
}

func (repository *PostgresRepository) financeImport(ctx context.Context, id int64) (FinanceImport, error) {
	var x FinanceImport
	err := repository.pool.QueryRow(ctx, `SELECT id,bank,account_number,file_name,status,rows_total,rows_new,rows_duplicate,rows_review,created_at FROM finance_imports WHERE id=$1`, id).Scan(&x.ID, &x.Bank, &x.AccountNumber, &x.FileName, &x.Status, &x.RowsTotal, &x.RowsNew, &x.RowsDuplicate, &x.RowsReview, &x.CreatedAt)
	return x, err
}
func (repository *PostgresRepository) financeTransaction(ctx context.Context, id int64) (FinanceTransaction, error) {
	var x FinanceTransaction
	err := repository.pool.QueryRow(ctx, `SELECT id,import_id,bank,account_number,operation_date::text,document_number,counterparty,purpose,currency,debit_rub::double precision,credit_rub::double precision,classification,pnl_effect,review_reason,confirmed FROM finance_transactions WHERE id=$1`, id).Scan(&x.ID, &x.ImportID, &x.Bank, &x.AccountNumber, &x.OperationDate, &x.DocumentNumber, &x.Counterparty, &x.Purpose, &x.Currency, &x.Debit, &x.Credit, &x.Classification, &x.PnlEffect, &x.ReviewReason, &x.Confirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, fmt.Errorf("операция не найдена")
	}
	return x, err
}
