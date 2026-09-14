package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type PnLLine struct {
	Channel string  `json:"channel"`
	Revenue float64 `json:"revenue"`
	COGS    float64 `json:"cogs"`
	Costs   float64 `json:"costs"`
	Profit  float64 `json:"profit"`
}

type PnLProduct struct {
	VariantID    int64   `json:"variantId"`
	Product      string  `json:"product"`
	SKU          string  `json:"sku"`
	Units        int     `json:"units"`
	Revenue      float64 `json:"revenue"`
	COGS         float64 `json:"cogs"`
	Packaging    float64 `json:"packaging"`
	CostSource   string  `json:"costSource"`
	Estimated    bool    `json:"estimated"`
	UnknownUnits int     `json:"unknownUnits"`
}

type PnLReport struct {
	From                string       `json:"from"`
	To                  string       `json:"to"`
	Preliminary         bool         `json:"preliminary"`
	Revenue             float64      `json:"revenue"`
	COGS                float64      `json:"cogs"`
	GrossProfit         float64      `json:"grossProfit"`
	MarketplaceCosts    float64      `json:"marketplaceCosts"`
	Packaging           float64      `json:"packaging"`
	ReturnLoss          float64      `json:"returnLoss"`
	OperatingExpenses   float64      `json:"operatingExpenses"`
	ProfitBeforeTax     float64      `json:"profitBeforeTax"`
	ActualTax           *float64     `json:"actualTax"`
	NetProfit           *float64     `json:"netProfit"`
	CashFlow            float64      `json:"cashFlow"`
	TaxRegime           string       `json:"taxRegime"`
	TaxRate             float64      `json:"taxRate"`
	ActualCostUnits     int          `json:"actualCostUnits"`
	EstimatedCostUnits  int          `json:"estimatedCostUnits"`
	UnknownCostUnits    int          `json:"unknownCostUnits"`
	ActualCostAmount    float64      `json:"actualCostAmount"`
	EstimatedCostAmount float64      `json:"estimatedCostAmount"`
	Channels            []PnLLine    `json:"channels"`
	Products            []PnLProduct `json:"products"`
	Warnings            []string     `json:"warnings"`
}

type TaxPeriodInput struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	ActualTax float64 `json:"actualTax"`
	Source    string  `json:"source"`
}

type MarketplaceAdjustmentInput struct {
	Channel          string  `json:"channel"`
	SourceDocumentID string  `json:"sourceDocumentId"`
	SourceLineID     string  `json:"sourceLineId"`
	OperationDate    string  `json:"operationDate"`
	Kind             string  `json:"kind"`
	Amount           float64 `json:"amount"`
	Effect           int     `json:"effect"`
	Status           string  `json:"status"`
}

type SupplierAccountOperation struct {
	ID                  int64   `json:"id"`
	OperationDate       string  `json:"operationDate"`
	Kind                string  `json:"kind"`
	OriginalAmountEUR   float64 `json:"originalAmountEur"`
	NormalizedAmountEUR float64 `json:"normalizedAmountEur"`
	LinkedTopupID       *int64  `json:"linkedTopupId,omitempty"`
	ProcurementOrderID  *int64  `json:"procurementOrderId,omitempty"`
	SourceReference     string  `json:"sourceReference"`
	Comment             string  `json:"comment"`
}

type SupplierAccountOverview struct {
	BalanceEUR                  float64                    `json:"balanceEur"`
	OriginalBalanceEUR          *float64                   `json:"originalBalanceEur"`
	ReservedEUR                 *float64                   `json:"reservedEur"`
	AvailableEUR                *float64                   `json:"availableEur"`
	ReconciliationDifferenceEUR *float64                   `json:"reconciliationDifferenceEur"`
	LastReconciledAt            *string                    `json:"lastReconciledAt"`
	Operations                  []SupplierAccountOperation `json:"operations"`
}

type SupplierAccountInput struct {
	OperationDate      string  `json:"operationDate"`
	Kind               string  `json:"kind"`
	OriginalAmountEUR  float64 `json:"originalAmountEur"`
	LinkedTopupID      *int64  `json:"linkedTopupId"`
	ProcurementOrderID *int64  `json:"procurementOrderId"`
	SourceReference    string  `json:"sourceReference"`
	Comment            string  `json:"comment"`
}

type SupplierReconciliationInput struct {
	Date               string   `json:"date"`
	OriginalBalanceEUR float64  `json:"originalBalanceEur"`
	ReservedEUR        *float64 `json:"reservedEur"`
	SourceReference    string   `json:"sourceReference"`
	Comment            string   `json:"comment"`
}

func financePeriod(fromText, toText string) (time.Time, time.Time, error) {
	from, err := time.Parse("2006-01-02", fromText)
	if err != nil {
		return from, time.Time{}, errors.New("проверьте начало периода")
	}
	to, err := time.Parse("2006-01-02", toText)
	if err != nil || to.Before(from) {
		return from, to, errors.New("проверьте конец периода")
	}
	start := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	if from.Before(start) {
		return from, to, errors.New("бизнес-учёт начинается 1 сентября 2026")
	}
	return from, to, nil
}

func (repository *PostgresRepository) FinancePnL(ctx context.Context, fromText, toText string) (PnLReport, error) {
	from, to, err := financePeriod(fromText, toText)
	if err != nil {
		return PnLReport{}, err
	}
	out := PnLReport{From: fromText, To: toText, TaxRegime: "АУСН · доходы", TaxRate: .08}
	toExclusive := to.AddDate(0, 0, 1)
	base := `event_status='confirmed' AND reconciliation_status='counted' AND (event_at AT TIME ZONE 'Europe/Moscow')::DATE >= $1::DATE AND (event_at AT TIME ZONE 'Europe/Moscow')::DATE < $2::DATE`
	err = repository.pool.QueryRow(ctx, `SELECT
		COALESCE(SUM(gross_rub*effect),0)::DOUBLE PRECISION,
		COALESCE(SUM(unit_cost_rub_snapshot*units) FILTER(WHERE event_type='sale'),0)::DOUBLE PRECISION,
		COALESCE(SUM(units) FILTER(WHERE event_type='sale' AND cost_quality='actual'),0)::INTEGER,
		COALESCE(SUM(units) FILTER(WHERE event_type='sale' AND cost_quality='estimated'),0)::INTEGER,
		COALESCE(SUM(units) FILTER(WHERE event_type='sale' AND unit_cost_rub_snapshot IS NULL),0)::INTEGER,
		COALESCE(SUM(unit_cost_rub_snapshot*units) FILTER(WHERE event_type='sale' AND cost_quality='actual'),0)::DOUBLE PRECISION,
		COALESCE(SUM(unit_cost_rub_snapshot*units) FILTER(WHERE event_type='sale' AND cost_quality='estimated'),0)::DOUBLE PRECISION
		FROM sales_events WHERE `+base, from, toExclusive).Scan(&out.Revenue, &out.COGS, &out.ActualCostUnits, &out.EstimatedCostUnits, &out.UnknownCostUnits, &out.ActualCostAmount, &out.EstimatedCostAmount)
	if err != nil {
		return out, err
	}
	var restored float64
	err = repository.pool.QueryRow(ctx, `SELECT
		COALESCE(SUM(unit_cost_rub_snapshot) FILTER(WHERE condition='ready'),0)::DOUBLE PRECISION,
		COALESCE(SUM(unit_cost_rub_snapshot) FILTER(WHERE condition='dead'),0)::DOUBLE PRECISION
		FROM marketplace_returns r WHERE EXISTS (SELECT 1 FROM sales_events s WHERE s.id=r.sales_event_id AND s.event_type='sale' AND s.event_status='confirmed' AND s.reconciliation_status='counted') AND returned_at >= $1 AND returned_at <= $2`, from, to).Scan(&restored, &out.ReturnLoss)
	if err != nil {
		return out, err
	}
	out.COGS -= restored
	err = repository.pool.QueryRow(ctx, `SELECT COALESCE(SUM(pack.amount_rub),0)::DOUBLE PRECISION
		FROM finance_packaging_snapshots pack JOIN sales_events event ON event.id=pack.sales_event_id
		WHERE (event.event_at AT TIME ZONE 'Europe/Moscow')::DATE >= $1::DATE AND (event.event_at AT TIME ZONE 'Europe/Moscow')::DATE < $2::DATE`, from, toExclusive).Scan(&out.Packaging)
	if err != nil {
		return out, err
	}
	var marketplaceNet float64
	err = repository.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount_rub*effect),0)::DOUBLE PRECISION
		FROM finance_marketplace_adjustments WHERE status='confirmed' AND operation_date >= $1 AND operation_date <= $2`, from, to).Scan(&marketplaceNet)
	if err != nil {
		return out, err
	}
	out.MarketplaceCosts = -marketplaceNet
	err = repository.pool.QueryRow(ctx, `SELECT
		COALESCE((SELECT SUM(debit_rub-credit_rub) FROM finance_transactions WHERE confirmed AND pnl_effect='expense' AND classification NOT IN ('tax','supplier','packaging_material','own_transfer','loan_principal','owner_contribution','owner_withdrawal') AND operation_date >= $1 AND operation_date <= $2),0)::DOUBLE PRECISION
		+ COALESCE((SELECT SUM(CASE direction WHEN 'out' THEN amount_rub ELSE -amount_rub END) FROM finance_cash_entries WHERE kind IN ('expense','adjustment') AND operation_date >= $1 AND operation_date <= $2),0)::DOUBLE PRECISION`, from, to).Scan(&out.OperatingExpenses)
	if err != nil {
		return out, err
	}
	err = repository.pool.QueryRow(ctx, `SELECT
		COALESCE((SELECT SUM(credit_rub-debit_rub) FROM finance_transactions WHERE confirmed AND operation_date >= $1 AND operation_date <= $2),0)::DOUBLE PRECISION
		+ COALESCE((SELECT SUM(CASE direction WHEN 'in' THEN amount_rub ELSE -amount_rub END) FROM finance_cash_entries WHERE operation_date >= $1 AND operation_date <= $2),0)::DOUBLE PRECISION`, from, to).Scan(&out.CashFlow)
	if err != nil {
		return out, err
	}
	out.GrossProfit = out.Revenue - out.COGS
	out.ProfitBeforeTax = out.GrossProfit - out.MarketplaceCosts - out.Packaging - out.OperatingExpenses
	var tax sql.NullFloat64
	err = repository.pool.QueryRow(ctx, `SELECT actual_tax_rub::DOUBLE PRECISION FROM finance_tax_periods WHERE period_start=$1 AND period_end=$2`, from, to).Scan(&tax)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if tax.Valid {
		out.ActualTax = &tax.Float64
		net := out.ProfitBeforeTax - tax.Float64
		out.NetProfit = &net
	}
	rows, err := repository.pool.Query(ctx, `SELECT event.channel,
		COALESCE(SUM(event.gross_rub*event.effect),0)::DOUBLE PRECISION,
		COALESCE(SUM(event.unit_cost_rub_snapshot*event.units) FILTER(WHERE event.event_type='sale'),0)::DOUBLE PRECISION,
		COALESCE(SUM(pack.amount_rub),0)::DOUBLE PRECISION
		FROM sales_events event LEFT JOIN finance_packaging_snapshots pack ON pack.sales_event_id=event.id WHERE `+strings.ReplaceAll(base, "event_", "event.event_")+` GROUP BY event.channel ORDER BY event.channel`, from, toExclusive)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var line PnLLine
		if err = rows.Scan(&line.Channel, &line.Revenue, &line.COGS, &line.Costs); err != nil {
			rows.Close()
			return out, err
		}
		var restored, adjustmentNet float64
		if err = repository.pool.QueryRow(ctx, `SELECT COALESCE(SUM(r.unit_cost_rub_snapshot),0)::DOUBLE PRECISION FROM marketplace_returns r JOIN sales_events s ON s.id=r.sales_event_id WHERE r.condition='ready' AND r.returned_at >= $1 AND r.returned_at <= $2 AND s.channel=$3 AND s.event_type='sale' AND s.event_status='confirmed' AND s.reconciliation_status='counted'`, from, to, line.Channel).Scan(&restored); err != nil {
			return out, err
		}
		if err = repository.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount_rub*effect),0)::DOUBLE PRECISION FROM finance_marketplace_adjustments WHERE status='confirmed' AND operation_date >= $1 AND operation_date <= $2 AND channel=$3`, from, to, line.Channel).Scan(&adjustmentNet); err != nil {
			return out, err
		}
		line.COGS -= restored
		line.Costs -= adjustmentNet
		line.Profit = line.Revenue - line.COGS - line.Costs
		out.Channels = append(out.Channels, line)
	}
	rows.Close()
	if err = rows.Err(); err != nil { return out, err }
	rows, err = repository.pool.Query(ctx, `SELECT event.canonical_variant_id,p.name,v.sku,
		COALESCE(SUM(event.units*event.effect),0)::INTEGER,COALESCE(SUM(event.gross_rub*event.effect),0)::DOUBLE PRECISION,
		COALESCE(SUM(event.unit_cost_rub_snapshot*event.units) FILTER(WHERE event.event_type='sale'),0)::DOUBLE PRECISION,
		COALESCE(SUM(pack.amount_rub),0)::DOUBLE PRECISION,
		CASE WHEN BOOL_OR(event.unit_cost_rub_snapshot IS NULL) THEN 'Не определён' WHEN BOOL_OR(event.cost_quality='estimated') THEN 'Оценка' ELSE 'Закупка' END,
		COALESCE(BOOL_OR(event.cost_quality='estimated'),FALSE),COALESCE(SUM(event.units) FILTER(WHERE event.event_type='sale' AND event.unit_cost_rub_snapshot IS NULL),0)::INTEGER,
		COALESCE((SELECT SUM(r.unit_cost_rub_snapshot) FROM marketplace_returns r WHERE r.canonical_variant_id=event.canonical_variant_id AND r.condition='ready' AND EXISTS (SELECT 1 FROM sales_events original WHERE original.id=r.sales_event_id AND original.event_type='sale' AND original.event_status='confirmed' AND original.reconciliation_status='counted') AND r.returned_at >= $1 AND r.returned_at < $2),0)::DOUBLE PRECISION
		FROM sales_events event JOIN product_variants v ON v.id=event.canonical_variant_id JOIN products p ON p.id=v.product_id
		LEFT JOIN finance_packaging_snapshots pack ON pack.sales_event_id=event.id
		WHERE `+strings.ReplaceAll(base, "event_", "event.event_")+` GROUP BY event.canonical_variant_id,p.name,v.sku ORDER BY SUM(event.gross_rub*event.effect) DESC LIMIT 100`, from, toExclusive)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item PnLProduct
		var restored float64
		if err = rows.Scan(&item.VariantID, &item.Product, &item.SKU, &item.Units, &item.Revenue, &item.COGS, &item.Packaging, &item.CostSource, &item.Estimated, &item.UnknownUnits, &restored); err != nil {
			rows.Close()
			return out, err
		}
		item.COGS -= restored
		out.Products = append(out.Products, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil { return out, err }
	var marketplaceSales, unlinkedReturns, unclassified int
	if err = repository.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sales_events WHERE event_type='sale' AND event_status='confirmed' AND reconciliation_status='counted' AND channel IN ('ozon','wb') AND (event_at AT TIME ZONE 'Europe/Moscow')::DATE >= $1::DATE AND (event_at AT TIME ZONE 'Europe/Moscow')::DATE < $2::DATE`, from, toExclusive).Scan(&marketplaceSales); err != nil { return out, err }
	if err = repository.pool.QueryRow(ctx, `SELECT COUNT(*) FROM marketplace_returns r WHERE returned_at >= $1 AND returned_at <= $2 AND NOT EXISTS (SELECT 1 FROM sales_events s WHERE s.id=r.sales_event_id AND s.event_type='sale' AND s.event_status='confirmed' AND s.reconciliation_status='counted')`, from, to).Scan(&unlinkedReturns); err != nil { return out, err }
	if err = repository.pool.QueryRow(ctx, `SELECT COUNT(*) FROM finance_transactions WHERE confirmed AND (classification='review' OR pnl_effect='review') AND operation_date >= $1 AND operation_date <= $2`, from, to).Scan(&unclassified); err != nil { return out, err }
	if unlinkedReturns > 0 { out.Warnings = append(out.Warnings, "Есть возвраты без подтверждённой исходной продажи; себестоимость по ним не восстановлена") }
	if unclassified > 0 { out.Warnings = append(out.Warnings, "Есть неразобранные банковские операции") }
	if out.UnknownCostUnits > 0 {
		out.Warnings = append(out.Warnings, "Есть продажи без себестоимости")
	}
	if out.EstimatedCostUnits > 0 {
		out.Warnings = append(out.Warnings, "Часть себестоимости оценочная")
	}
	if marketplaceSales > 0 {
		out.Warnings = append(out.Warnings, "Полнота комиссий и логистики маркетплейсов за период не подтверждена; отдельные строки отчёта не закрывают весь период")
	}
	if out.ActualTax == nil {
		out.Warnings = append(out.Warnings, "Фактический налог АУСН не сверен")
	}
	out.Preliminary = len(out.Warnings) > 0
	return out, rows.Err()
}

func (repository *PostgresRepository) SaveFinanceTax(ctx context.Context, actor Actor, input TaxPeriodInput) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	from, to, err := financePeriod(input.From, input.To)
	if err != nil {
		return err
	}
	if input.ActualTax < 0 {
		return errors.New("налог не может быть отрицательным")
	}
	_, err = repository.pool.Exec(ctx, `INSERT INTO finance_tax_periods(period_start,period_end,actual_tax_rub,source,confirmed_by,confirmed_at) VALUES($1,$2,$3,$4,$5,CURRENT_TIMESTAMP) ON CONFLICT(period_start,period_end) DO UPDATE SET actual_tax_rub=EXCLUDED.actual_tax_rub,source=EXCLUDED.source,confirmed_by=EXCLUDED.confirmed_by,confirmed_at=CURRENT_TIMESTAMP`, from, to, input.ActualTax, strings.TrimSpace(input.Source), actor.CustomerID)
	return err
}

func (repository *PostgresRepository) CreateMarketplaceAdjustment(ctx context.Context, actor Actor, input MarketplaceAdjustmentInput) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	date, err := time.Parse("2006-01-02", input.OperationDate)
	if err != nil || input.Amount <= 0 {
		return errors.New("проверьте дату и сумму")
	}
	validChannel := map[string]bool{"ozon": true, "wb": true, "avito": true}
	validKind := map[string]bool{"commission": true, "logistics": true, "withholding": true, "compensation": true, "acquiring": true, "other": true}
	if !validChannel[input.Channel] || !validKind[input.Kind] || (input.Effect != -1 && input.Effect != 1) || input.SourceDocumentID == "" || (input.Status != "review" && input.Status != "confirmed") {
		return errors.New("проверьте реквизиты отчёта")
	}
	_, err = repository.pool.Exec(ctx, `INSERT INTO finance_marketplace_adjustments(channel,source_document_id,source_line_id,operation_date,kind,amount_rub,effect,status,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(channel,source_document_id,source_line_id,kind) DO UPDATE SET amount_rub=EXCLUDED.amount_rub,effect=EXCLUDED.effect,status=EXCLUDED.status`, input.Channel, strings.TrimSpace(input.SourceDocumentID), strings.TrimSpace(input.SourceLineID), date, input.Kind, input.Amount, input.Effect, input.Status, actor.CustomerID)
	return err
}

func (repository *PostgresRepository) SupplierAccount(ctx context.Context) (SupplierAccountOverview, error) {
	var out SupplierAccountOverview
	if err := repository.pool.QueryRow(ctx, `SELECT COALESCE(SUM(normalized_amount_eur),0)::DOUBLE PRECISION FROM supplier_account_operations`).Scan(&out.BalanceEUR); err != nil {
		return out, err
	}
	rows, err := repository.pool.Query(ctx, `SELECT id,operation_date::TEXT,kind,original_amount_eur::DOUBLE PRECISION,normalized_amount_eur::DOUBLE PRECISION,linked_topup_id,procurement_order_id,source_reference,comment FROM supplier_account_operations ORDER BY operation_date DESC,id DESC LIMIT 200`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item SupplierAccountOperation
		if err = rows.Scan(&item.ID, &item.OperationDate, &item.Kind, &item.OriginalAmountEUR, &item.NormalizedAmountEUR, &item.LinkedTopupID, &item.ProcurementOrderID, &item.SourceReference, &item.Comment); err != nil {
			rows.Close()
			return out, err
		}
		out.Operations = append(out.Operations, item)
	}
	rows.Close()
	var original, reserved sql.NullFloat64
	var date sql.NullString
	err = repository.pool.QueryRow(ctx, `SELECT original_balance_eur::DOUBLE PRECISION,reserved_eur::DOUBLE PRECISION,reconciliation_date::TEXT FROM supplier_account_reconciliations ORDER BY reconciliation_date DESC,id DESC LIMIT 1`).Scan(&original, &reserved, &date)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if original.Valid {
		out.OriginalBalanceEUR = &original.Float64
		normalized := -original.Float64
		difference := normalized - out.BalanceEUR
		out.ReconciliationDifferenceEUR = &difference
	}
	if reserved.Valid {
		out.ReservedEUR = &reserved.Float64
		available := out.BalanceEUR - reserved.Float64
		out.AvailableEUR = &available
	}
	if date.Valid {
		out.LastReconciledAt = &date.String
	}
	return out, rows.Err()
}

func (repository *PostgresRepository) CreateSupplierAccountOperation(ctx context.Context, actor Actor, input SupplierAccountInput) (SupplierAccountOperation, error) {
	if actor.Role != RoleOwner {
		return SupplierAccountOperation{}, ErrForbidden
	}
	date, err := time.Parse("2006-01-02", input.OperationDate)
	if err != nil || input.OriginalAmountEUR == 0 {
		return SupplierAccountOperation{}, errors.New("проверьте дату и сумму")
	}
	valid := map[string]bool{"opening": true, "topup": true, "invoice": true, "trolley_return": true, "correction": true, "other": true}
	if !valid[input.Kind] {
		return SupplierAccountOperation{}, errors.New("неизвестный тип операции")
	}
	if input.Kind == "correction" && input.LinkedTopupID == nil {
		return SupplierAccountOperation{}, errors.New("корректировку нужно связать с пополнением")
	}
	if input.Kind != "correction" && input.LinkedTopupID != nil {
		return SupplierAccountOperation{}, errors.New("связь с пополнением допустима только для корректировки")
	}
	if (input.Kind == "invoice" || input.Kind == "trolley_return") && strings.TrimSpace(input.SourceReference) == "" {
		return SupplierAccountOperation{}, errors.New("для инвойса или возврата телег укажите документ")
	}
	if input.LinkedTopupID != nil {
		var kind string
		if err = repository.pool.QueryRow(ctx, `SELECT kind FROM supplier_account_operations WHERE id=$1`, *input.LinkedTopupID).Scan(&kind); err != nil || kind != "topup" {
			return SupplierAccountOperation{}, errors.New("связанное пополнение не найдено")
		}
	}
	var out SupplierAccountOperation
	normalized := -input.OriginalAmountEUR
	err = repository.pool.QueryRow(ctx, `INSERT INTO supplier_account_operations(operation_date,kind,original_amount_eur,normalized_amount_eur,linked_topup_id,procurement_order_id,source_reference,comment,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,operation_date::TEXT,kind,original_amount_eur::DOUBLE PRECISION,normalized_amount_eur::DOUBLE PRECISION,linked_topup_id,procurement_order_id,source_reference,comment`, date, input.Kind, input.OriginalAmountEUR, normalized, input.LinkedTopupID, input.ProcurementOrderID, strings.TrimSpace(input.SourceReference), strings.TrimSpace(input.Comment), actor.CustomerID).Scan(&out.ID, &out.OperationDate, &out.Kind, &out.OriginalAmountEUR, &out.NormalizedAmountEUR, &out.LinkedTopupID, &out.ProcurementOrderID, &out.SourceReference, &out.Comment)
	return out, err
}

func (repository *PostgresRepository) ReconcileSupplierAccount(ctx context.Context, actor Actor, input SupplierReconciliationInput) error {
	if actor.Role != RoleOwner {
		return ErrForbidden
	}
	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		return errors.New("проверьте дату сверки")
	}
	if input.ReservedEUR != nil && *input.ReservedEUR < 0 {
		return errors.New("резерв не может быть отрицательным")
	}
	_, err = repository.pool.Exec(ctx, `INSERT INTO supplier_account_reconciliations(reconciliation_date,original_balance_eur,normalized_balance_eur,reserved_eur,source_reference,comment,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, date, input.OriginalBalanceEUR, -input.OriginalBalanceEUR, input.ReservedEUR, strings.TrimSpace(input.SourceReference), strings.TrimSpace(input.Comment), actor.CustomerID)
	return err
}
