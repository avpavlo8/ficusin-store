package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type FinanceSourceRow struct {
	Row int
	Date time.Time
	Number, Account, CounterpartyAccount, Counterparty, Purpose, Currency string
	Debit, Credit float64
}

var financeMoney = regexp.MustCompile(`[-+]?\d[\d\s]*(?:[.,]\d{1,2})`)
var financeDate = regexp.MustCompile(`\b(\d{2}[.]\d{2}[.]\d{4})\b`)

func ParseFinanceStatement(ctx context.Context, name string, content []byte) ([]FinanceSourceRow, string, error) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".xlsx"):
		rows, err := parseFinanceXLSX(content)
		return rows, "xlsx", err
	case strings.HasSuffix(lower, ".pdf"):
		text, err := financePDFText(ctx, content)
		if err != nil { return nil, "pdf", err }
		rows := parseFinancePDFText(text)
		if len(rows) == 0 { return nil, "pdf", errors.New("в PDF не найдены операции; загрузите подробную выписку") }
		return rows, "pdf", nil
	default:
		return nil, "", errors.New("поддерживаются PDF и XLSX")
	}
}

func financePDFText(ctx context.Context, content []byte) (string, error) {
	f, err := os.CreateTemp("", "ficusin-bank-*.pdf")
	if err != nil { return "", err }
	name := f.Name(); defer os.Remove(name) //nolint:errcheck
	if _, err = f.Write(content); err != nil { f.Close(); return "", err }
	if err = f.Close(); err != nil { return "", err }
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second); defer cancel()
	out, err := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", name, "-").CombinedOutput()
	if err != nil { return "", fmt.Errorf("прочитать PDF: %w", err) }
	return strings.ReplaceAll(string(out), "\u00a0", " "), nil
}

// PDF layouts differ, but all three banks start a logical operation with a
// date and place debit/credit amounts on that first line. Continuation lines
// are retained as the purpose, so ambiguous short Sber exports go to review.
func parseFinancePDFText(text string) []FinanceSourceRow {
	lines := strings.Split(text, "\n")
	result := make([]FinanceSourceRow, 0)
	var current *FinanceSourceRow
	for i, line := range lines {
		m := financeDate.FindStringSubmatch(line)
		if len(m) > 1 && strings.HasPrefix(strings.TrimSpace(line), m[1]) {
			if current != nil { result = append(result, *current) }
			date, _ := time.Parse("02.01.2006", m[1])
			fields := strings.Fields(strings.TrimSpace(line))
			current = &FinanceSourceRow{Row:i+1, Date:date, Currency:"RUB"}
			if len(fields) > 1 { current.Number = fields[1] }
			amounts := financeMoney.FindAllString(line[len(m[0]):], -1)
			if len(amounts) > 0 {
				value := parseFinanceAmount(amounts[len(amounts)-1])
				// Wide detailed exports have separate debit/credit columns. The
				// amount's horizontal position reliably identifies the side.
				pos := strings.LastIndex(line, amounts[len(amounts)-1])
				if pos > len(line)*3/4 { current.Credit = value } else { current.Debit = value }
			}
			continue
		}
		if current != nil && strings.TrimSpace(line) != "" {
			piece := strings.TrimSpace(line)
			if current.Counterparty == "" && !strings.Contains(strings.ToLower(piece), "инн:") { current.Counterparty = piece } else { current.Purpose = strings.TrimSpace(current.Purpose+" "+piece) }
		}
	}
	if current != nil { result = append(result, *current) }
	return result
}

type xlsxCell struct { Ref string `xml:"r,attr"`; Type string `xml:"t,attr"`; V string `xml:"v"`; Inline struct { Texts []string `xml:"r>t"`; Text string `xml:"t"` } `xml:"is"` }
type xlsxRow struct { Number int `xml:"r,attr"`; Cells []xlsxCell `xml:"c"` }
type xlsxSheet struct { Rows []xlsxRow `xml:"sheetData>row"` }
type sharedItem struct { Texts []string `xml:"r>t"`; Text string `xml:"t"` }

func parseFinanceXLSX(content []byte) ([]FinanceSourceRow, error) {
	z, err := zip.NewReader(bytes.NewReader(content), int64(len(content))); if err != nil { return nil, errors.New("повреждённый XLSX") }
	var sheet []byte; shared := []string{}
	for _, f := range z.File {
		if f.Name == "xl/worksheets/sheet1.xml" { sheet, err = readZipFile(f) }
		if f.Name == "xl/sharedStrings.xml" { var raw []byte; raw, err = readZipFile(f); if err == nil { var root struct{ Items []sharedItem `xml:"si"` }; _=xml.Unmarshal(raw,&root); for _, item := range root.Items { shared=append(shared, item.Text+strings.Join(item.Texts,"")) } }
	}
	if err != nil || len(sheet)==0 { return nil, errors.New("в XLSX нет первого листа") }
	var doc xlsxSheet; if err=xml.Unmarshal(sheet,&doc); err != nil { return nil, errors.New("не удалось прочитать XLSX") }
	// Deliberately iterate sheetData instead of dimension: Sber declares A1
	// even when the sheet contains hundreds of rows.
	result:=make([]FinanceSourceRow,0,len(doc.Rows)); headers:=map[string]int{}
	for _, row := range doc.Rows {
		values:=map[int]string{}
		for _, c:=range row.Cells { col:=columnIndex(c.Ref); value:=c.V; if c.Type=="inlineStr" { value=c.Inline.Text+strings.Join(c.Inline.Texts,"") }; if c.Type=="s" { n,_:=strconv.Atoi(c.V); if n>=0&&n<len(shared){value=shared[n]} }; values[col]=strings.TrimSpace(value) }
		if len(headers)==0 { for col,v:=range values { headers[strings.ToLower(v)]=col }; continue }
		date, e:=time.Parse("02.01.2006", values[headers["дата"]]); if e!=nil { continue }
		item:=FinanceSourceRow{Row:row.Number,Date:date,Number:values[headers["номер"]],Account:values[headers["номер счёта"]],CounterpartyAccount:values[headers["контрагент cчёт"]],Counterparty:values[headers["контрагент"]],Purpose:values[headers["назначение"]],Currency:"RUB",Credit:parseFinanceAmount(values[headers["поступление"]]),Debit:parseFinanceAmount(values[headers["списание"]])}
		if item.Credit>0||item.Debit>0 { result=append(result,item) }
	}
	if len(result)==0 { return nil, errors.New("в XLSX не найдены операции") }; return result,nil
}

func readZipFile(f *zip.File)([]byte,error){ r,e:=f.Open();if e!=nil{return nil,e};defer r.Close();return io.ReadAll(io.LimitReader(r,32<<20)) }
func columnIndex(ref string) int { n:=0;for _,r:=range ref { if r<'A'||r>'Z'{break};n=n*26+int(r-'A'+1) };return n }
func parseFinanceAmount(s string) float64 { s=strings.NewReplacer(" ","","\u00a0","",",",".","+","").Replace(strings.TrimSpace(s));v,_:=strconv.ParseFloat(s,64);if v<0{return -v};return v }
func financeDedupe(row FinanceSourceRow) string { raw:=fmt.Sprintf("%s|%s|%s|%s|%.2f|%.2f|%s",row.Date.Format("2006-01-02"),row.Number,compact(row.CounterpartyAccount),compact(row.Purpose),row.Debit,row.Credit,compact(row.Counterparty));sum:=sha256.Sum256([]byte(raw));return hex.EncodeToString(sum[:]) }
func compact(v string)string{return strings.Join(strings.Fields(strings.ToLower(v))," ")}

func classifyFinance(row FinanceSourceRow)(string,string,string){
	v:=compact(row.Counterparty+" "+row.Purpose)
	switch {
	case strings.Contains(v,"комисси"): return "commission","expense",""
	case strings.Contains(v,"эквайринг"): return "acquiring","income",""
	case strings.Contains(v,"рвб")||strings.Contains(v,"вайлдбер")||strings.Contains(v,"озон инвест"): return "marketplace_payout","income",""
	case strings.Contains(v,"налог")||strings.Contains(v,"фнс"): return "tax","expense",""
	case strings.Contains(v,"процент")&&strings.Contains(v,"займ"): return "loan_interest","expense",""
	case strings.Contains(v,"займ")||strings.Contains(v,"кредит"): return "loan_principal","none",""
	case strings.Contains(v,"собственн")||strings.Contains(v,"между своими"): return "own_transfer","none",""
	case row.Counterparty==""||row.Purpose=="": return "review","review","В выписке недостаточно реквизитов"
	default: return "review","review","Требуется выбрать статью"
	}
}
