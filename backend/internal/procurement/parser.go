package procurement

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const parserVersion = 1

var (
	hollandLinePattern    = regexp.MustCompile(`^\s*(\d+)\s+(.+?)\s+(\d[\d.]*,\d{2})\s+(\d[\d.]*,\d{2})\s*$`)
	hollandBoxPattern     = regexp.MustCompile(`(?i)Box\s+no:\s*(\d+)`)
	potPattern            = regexp.MustCompile(`(?i)(?:Pot\s*[ØO]|\bd|диаметр)\s*:?\s*([0-9]+(?:[.,][0-9]+)?)`)
	heightPattern         = regexp.MustCompile(`(?i)Height\s*:\s*([0-9]+(?:[.,][0-9]+)?)`)
	domesticLinePattern   = regexp.MustCompile(`^\s*\d+\s+(.+?)\s+(\d+)\s+шт\s+([\d ]+,\d{2})\s+(?:\d+%\s+)?(?:[\d ]+,\d{2}\s+)?([\d ]+,\d{2})\s*$`)
	domesticHeaderPattern = regexp.MustCompile(`(?i)Счет на оплату №\s*([^\s]+)\s+от\s+(\d{1,2})\s+([а-яё]+)\s+(\d{4})`)
)

type Parser interface {
	Parse([]byte) (ParsedDocument, error)
}

type PDFParser struct{}

func NewPDFParser() PDFParser { return PDFParser{} }

func (PDFParser) Parse(content []byte) (ParsedDocument, error) {
	text, err := extractPDFText(content)
	if err != nil {
		return ParsedDocument{}, fmt.Errorf("extract pdf text: %w", err)
	}
	return ParseDocumentText(text)
}

func extractPDFText(content []byte) (string, error) {
	file, err := os.CreateTemp("", "ficusin-procurement-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temporary pdf: %w", err)
	}
	path := file.Name()
	defer os.Remove(path) //nolint:errcheck
	if _, err := file.Write(content); err != nil {
		file.Close() //nolint:errcheck
		return "", fmt.Errorf("write temporary pdf: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close temporary pdf: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", path, "-").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("pdftotext timeout: %w", ctx.Err())
		}
		return "", fmt.Errorf("pdftotext: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func ParseDocumentText(text string) (ParsedDocument, error) {
	clean := strings.ReplaceAll(text, "\u00a0", " ")
	switch {
	case strings.Contains(strings.ToLower(clean), "packinglist"):
		return parseHolland(clean)
	case strings.Contains(strings.ToLower(clean), "счет на оплату"):
		return parseDomestic(clean)
	default:
		return ParsedDocument{}, ErrUnsupportedDocument
	}
}

func parseHolland(text string) (ParsedDocument, error) {
	result := ParsedDocument{ParserKind: "holland_packing_list", Currency: "EUR", ExtractedText: text}
	if match := regexp.MustCompile(`(?i)List\s+no\.:\s*(\d+)`).FindStringSubmatch(text); len(match) > 1 {
		result.DocumentNumber = match[1]
	}
	if match := regexp.MustCompile(`(?i)Date:\s*(\d{2}-\d{2}-\d{4})`).FindStringSubmatch(text); len(match) > 1 {
		if value, err := time.Parse("02-01-2006", match[1]); err == nil {
			result.DocumentDate = &value
		}
	}

	lineNumber := 0
	loadUnit := ""
	productSection := true
	for pageIndex, page := range strings.Split(text, "\f") {
		lastLine := -1
		scanner := bufio.NewScanner(strings.NewReader(page))
		for scanner.Scan() {
			row := strings.TrimSpace(scanner.Text())
			if row == "" {
				continue
			}
			if strings.Contains(row, "Subtotal product") || (strings.Contains(row, "Quan") && strings.Contains(row, "Packing")) {
				productSection = false
				lastLine = -1
				continue
			}
			if match := hollandBoxPattern.FindStringSubmatch(row); len(match) > 1 {
				loadUnit = match[1]
				productSection = true
				lastLine = -1
				continue
			}
			if !productSection {
				continue
			}
			if match := hollandLinePattern.FindStringSubmatch(row); len(match) > 4 {
				quantity, _ := strconv.Atoi(match[1])
				unitPrice, priceOK := parseMoney(match[3])
				lineTotal, totalOK := parseMoney(match[4])
				name := strings.TrimSpace(match[2])
				if !priceOK || !totalOK || quantity <= 0 || name == "" || quantity > 10000 {
					continue
				}
				lineNumber++
				result.Lines = append(result.Lines, ParsedLine{
					SourcePage: pageIndex + 1, SourceLine: lineNumber, RawName: name,
					Quantity: quantity, UnitPrice: unitPrice, LineTotal: lineTotal, LoadUnit: loadUnit,
				})
				lastLine = len(result.Lines) - 1
				continue
			}
			if lastLine >= 0 {
				if value := parseDimension(row, potPattern); value != nil {
					result.Lines[lastLine].PotDiameterCM = value
				}
				if value := parseDimension(row, heightPattern); value != nil {
					result.Lines[lastLine].HeightCM = value
				}
			}
		}
	}
	result.ProductSubtotal, _ = findMoney(text, `(?i)Subtotal\s+product\s+([\d .]+,\d{2})`)
	result.DocumentTotal, _ = findMoney(text, `(?i)Total\s+Amount\s+(?:€\s*)?([\d .]+,\d{2})`)
	result.CalculatedTotal = sumLines(result.Lines)
	if result.ProductSubtotal == 0 {
		result.ProductSubtotal = result.CalculatedTotal
	}
	if result.DocumentTotal > result.ProductSubtotal {
		result.PackageTotal = result.DocumentTotal - result.ProductSubtotal
	}
	result.ArithmeticOK = linesAreValid(result.Lines) && almostEqual(result.ProductSubtotal, result.CalculatedTotal)
	if len(result.Lines) == 0 {
		return ParsedDocument{}, ErrUnsupportedDocument
	}
	return result, nil
}

func parseDomestic(text string) (ParsedDocument, error) {
	result := ParsedDocument{ParserKind: "domestic_payment_invoice", Currency: "RUB", ExtractedText: text}
	if match := domesticHeaderPattern.FindStringSubmatch(text); len(match) > 4 {
		result.DocumentNumber = match[1]
		day, _ := strconv.Atoi(match[2])
		if month := russianMonth(match[3]); month > 0 {
			year, _ := strconv.Atoi(match[4])
			value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			result.DocumentDate = &value
		}
	}
	lineNumber := 0
	for pageIndex, page := range strings.Split(text, "\f") {
		scanner := bufio.NewScanner(strings.NewReader(page))
		for scanner.Scan() {
			match := domesticLinePattern.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
			if len(match) < 5 {
				continue
			}
			quantity, _ := strconv.Atoi(match[2])
			unitPrice, priceOK := parseMoney(match[3])
			lineTotal, totalOK := parseMoney(match[4])
			if quantity <= 0 || !priceOK || !totalOK {
				continue
			}
			lineNumber++
			name := strings.TrimSpace(match[1])
			result.Lines = append(result.Lines, ParsedLine{
				SourcePage: pageIndex + 1, SourceLine: lineNumber, RawName: name,
				Quantity: quantity, UnitPrice: unitPrice, LineTotal: lineTotal,
				PotDiameterCM: parseDimension(name, potPattern),
			})
		}
	}
	result.DocumentTotal, _ = findMoney(text, `(?m)^\s*Итого:\s*([\d ]+,\d{2})`)
	result.ProductSubtotal = result.DocumentTotal
	result.CalculatedTotal = sumLines(result.Lines)
	result.ArithmeticOK = linesAreValid(result.Lines) && almostEqual(result.DocumentTotal, result.CalculatedTotal)
	if len(result.Lines) == 0 {
		return ParsedDocument{}, ErrUnsupportedDocument
	}
	return result, nil
}

func parseMoney(raw string) (float64, bool) {
	value := strings.NewReplacer(" ", "", ".", "", ",", ".").Replace(strings.TrimSpace(raw))
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

func findMoney(text, pattern string) (float64, bool) {
	match := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(match) < 2 {
		return 0, false
	}
	return parseMoney(match[1])
}

func parseDimension(text string, pattern *regexp.Regexp) *float64 {
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return nil
	}
	value, ok := parseMoney(match[1])
	if !ok {
		return nil
	}
	return &value
}

func sumLines(lines []ParsedLine) float64 {
	var total float64
	for _, line := range lines {
		total += line.LineTotal
	}
	return math.Round(total*100) / 100
}

func linesAreValid(lines []ParsedLine) bool {
	for _, line := range lines {
		if !almostEqual(float64(line.Quantity)*line.UnitPrice, line.LineTotal) {
			return false
		}
	}
	return len(lines) > 0
}

func almostEqual(left, right float64) bool { return math.Abs(left-right) <= 0.05 }

func russianMonth(value string) time.Month {
	months := map[string]time.Month{
		"января": time.January, "февраля": time.February, "марта": time.March,
		"апреля": time.April, "мая": time.May, "июня": time.June,
		"июля": time.July, "августа": time.August, "сентября": time.September,
		"октября": time.October, "ноября": time.November, "декабря": time.December,
	}
	return months[strings.ToLower(value)]
}
