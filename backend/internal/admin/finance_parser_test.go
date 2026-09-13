package admin

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestFinanceXLSXIgnoresBrokenDimensionAndReadsAllRows(t *testing.T) {
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	file, err := w.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte(`<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1"/><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>Номер</t></is></c><c r="B1" t="inlineStr"><is><t>Номер счёта</t></is></c><c r="C1" t="inlineStr"><is><t>Дата</t></is></c><c r="D1" t="inlineStr"><is><t>Контрагент cчёт</t></is></c><c r="E1" t="inlineStr"><is><t>Контрагент</t></is></c><c r="F1" t="inlineStr"><is><t>Поступление</t></is></c><c r="H1" t="inlineStr"><is><t>Списание</t></is></c><c r="J1" t="inlineStr"><is><t>Назначение</t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>42</t></is></c><c r="B2" t="inlineStr"><is><t>40802</t></is></c><c r="C2" t="inlineStr"><is><t>01.09.2026</t></is></c><c r="E2" t="inlineStr"><is><t>Банк</t></is></c><c r="F2" t="n"><v>1234.56</v></c><c r="J2" t="inlineStr"><is><t>Эквайринг комиссия 12,34</t></is></c></row>
<row r="3"><c r="A3" t="inlineStr"><is><t>42</t></is></c><c r="C3" t="inlineStr"><is><t>02.09.2026</t></is></c><c r="E3" t="inlineStr"><is><t>Поставщик</t></is></c><c r="H3" t="n"><v>-200.10</v></c><c r="J3" t="inlineStr"><is><t>Оплата товара</t></is></c></row>
</sheetData></worksheet>`))
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := parseFinanceXLSX(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Credit != 1234.56 || rows[1].Debit != 200.10 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestFinanceDedupeUsesMoreThanDocumentNumber(t *testing.T) {
	rows, err := parseFinanceXLSX(financeTestWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	if financeDedupe(rows[0]) == financeDedupe(rows[1]) {
		t.Fatal("different payments with the same number collapsed")
	}
	if financeDedupe(rows[0]) != financeDedupe(rows[0]) {
		t.Fatal("dedupe is not stable")
	}
}

func TestFinanceClassificationKeepsTransfersOutOfPnL(t *testing.T) {
	class, effect, _ := classifyFinance(FinanceSourceRow{Counterparty: "ИП Владелец", Purpose: "Перевод собственных средств", Credit: 100})
	if class != "own_transfer" || effect != "none" {
		t.Fatalf("got %s/%s", class, effect)
	}
	class, effect, _ = classifyFinance(FinanceSourceRow{Counterparty: "Банк", Purpose: "Комиссия за обслуживание", Debit: 490})
	if class != "commission" || effect != "expense" {
		t.Fatalf("got %s/%s", class, effect)
	}
	class, effect, _ = classifyFinance(FinanceSourceRow{Counterparty: "Упаковка Про", Purpose: "Коробки для отправлений", Debit: 3000})
	if class != "packaging_material" || effect != "none" {
		t.Fatalf("packaging purchase got %s/%s", class, effect)
	}
}

func TestShortSberPDFNeedsReview(t *testing.T) {
	rows := parseFinancePDFText("01.09.2026  123  046126614  30233810653000117000  4 567,86\n")
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	class, effect, _ := classifyFinance(rows[0])
	if class != "review" || effect != "review" {
		t.Fatalf("got %s/%s", class, effect)
	}
}

func financeTestWorkbook(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, e := w.Create("xl/worksheets/sheet1.xml")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.Write([]byte(`<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Номер</t></is></c><c r="C1" t="inlineStr"><is><t>Дата</t></is></c><c r="E1" t="inlineStr"><is><t>Контрагент</t></is></c><c r="F1" t="inlineStr"><is><t>Поступление</t></is></c><c r="J1" t="inlineStr"><is><t>Назначение</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>7</t></is></c><c r="C2" t="inlineStr"><is><t>01.09.2026</t></is></c><c r="E2" t="inlineStr"><is><t>А</t></is></c><c r="F2"><v>10.01</v></c><c r="J2" t="inlineStr"><is><t>Первая</t></is></c></row><row r="3"><c r="A3" t="inlineStr"><is><t>7</t></is></c><c r="C3" t="inlineStr"><is><t>01.09.2026</t></is></c><c r="E3" t="inlineStr"><is><t>Б</t></is></c><c r="F3"><v>10.01</v></c><c r="J3" t="inlineStr"><is><t>Вторая</t></is></c></row></sheetData></worksheet>`))
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
