package procurement

import (
	"fmt"
	"os"
	"strings"

	"github.com/signintech/gopdf"
)

const (
	receivingPDFWidth  = 842.0
	receivingPDFHeight = 595.0
)

type receivingPDFRow struct {
	Name        string
	InvoiceName string
	Pot         *float64
	Height      *float64
	Packages    *int
	Units       *int
	Quantity    int
	Price       int64
}

func receivingPDFRows(detail OrderDetail) ([]receivingPDFRow, error) {
	rows := make([]receivingPDFRow, 0, len(detail.Lines))
	for _, line := range detail.Lines {
		if line.InvoiceExcluded || line.ReconciliationStatus == "superseded" || line.ReconciliationStatus == "missing" {
			continue
		}
		if line.ReconciliationStatus == "added" && !line.ComparisonAccepted {
			continue
		}
		if line.MatchStatus != "confirmed" || line.InvoicedQuantity == nil || *line.InvoicedQuantity <= 0 || line.ProposedRetailRUB == nil || *line.ProposedRetailRUB <= 0 {
			return nil, &UserFacingError{Message: "PDF приёмки доступен только после сопоставления и расчёта всех позиций"}
		}
		name := strings.TrimSpace(line.SabyName)
		if name == "" {
			name = strings.TrimSpace(line.RawName)
		}
		rows = append(rows, receivingPDFRow{
			Name: name, InvoiceName: strings.TrimSpace(line.InvoiceRawName), Pot: line.PotDiameterCM, Height: line.HeightCM,
			Packages: line.PackageCount, Units: line.UnitsPerPackage, Quantity: *line.InvoicedQuantity, Price: *line.ProposedRetailRUB,
		})
	}
	if len(rows) == 0 {
		return nil, &UserFacingError{Message: "В закупке нет рассчитанных позиций для PDF приёмки"}
	}
	return rows, nil
}

func BuildReceivingPDF(detail OrderDetail) ([]byte, int, error) {
	rows, err := receivingPDFRows(detail)
	if err != nil {
		return nil, 0, err
	}
	fontPath := strings.TrimSpace(os.Getenv("PROCUREMENT_PDF_FONT"))
	if fontPath == "" {
		fontPath = "/app/fonts/DejaVuSans.ttf"
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: receivingPDFWidth, H: receivingPDFHeight}})
	if err := pdf.AddTTFFont("DejaVu", fontPath); err != nil {
		return nil, 0, fmt.Errorf("load receiving PDF font: %w", err)
	}

	const margin = 30.0
	widths := []float64{28, 320, 85, 90, 60, 95, 70}
	tableWidth := 0.0
	for _, width := range widths {
		tableWidth += width
	}
	page := 0
	y := 0.0
	addPage := func() error {
		page++
		pdf.AddPage()
		pdf.SetFillColor(30, 69, 53)
		pdf.RectFromUpperLeftWithStyle(margin, 24, tableWidth, 42, "F")
		pdf.SetTextColor(255, 255, 255)
		if err := pdf.SetFont("DejaVu", "", 17); err != nil {
			return err
		}
		pdf.SetXY(margin+14, 34)
		if err := pdf.Cell(nil, "Лист приёмки · Фикусин"); err != nil {
			return err
		}

		pdf.SetTextColor(45, 55, 49)
		if err := pdf.SetFont("DejaVu", "", 9); err != nil {
			return err
		}
		orderTitle := detail.Order.OrderNumber
		if strings.TrimSpace(orderTitle) == "" {
			orderTitle = fmt.Sprintf("№%d", detail.Order.ID)
		}
		meta := fmt.Sprintf("Закупка %s  ·  Инвойс %s  ·  %s  ·  %d позиций / %d шт.", orderTitle, blankDash(detail.Order.DocumentNumber), detail.Order.SupplierName, len(rows), receivingTotalUnits(rows))
		pdf.SetXY(margin, 76)
		if err := pdf.Cell(nil, meta); err != nil {
			return err
		}
		if detail.Order.DocumentDate != nil {
			pdf.SetXY(margin, 90)
			if err := pdf.Cell(nil, "Дата инвойса: "+detail.Order.DocumentDate.Format("02.01.2006")); err != nil {
				return err
			}
		}

		y = 112
		pdf.SetFillColor(238, 242, 239)
		pdf.SetStrokeColor(199, 207, 201)
		pdf.SetLineWidth(.45)
		pdf.RectFromUpperLeftWithStyle(margin, y, tableWidth, 24, "FD")
		if err := pdf.SetFont("DejaVu", "", 8.5); err != nil {
			return err
		}
		pdf.SetTextColor(45, 55, 49)
		headers := []string{"№", "Название СБИС", "Размер", "Упаковка", "Кол-во", "Цена, ₽", "Проверено"}
		x := margin
		for index, header := range headers {
			pdf.SetXY(x+4, y+7)
			if err := pdf.CellWithOption(&gopdf.Rect{W: widths[index] - 8, H: 12}, header, gopdf.CellOption{Align: gopdf.Left}); err != nil {
				return err
			}
			x += widths[index]
		}
		y += 24
		return nil
	}
	if err := addPage(); err != nil {
		return nil, 0, err
	}

	for index, row := range rows {
		const rowHeight = 34.0
		if y+rowHeight > receivingPDFHeight-36 {
			pdf.SetTextColor(100, 105, 102)
			_ = pdf.SetFont("DejaVu", "", 8)
			pdf.SetXY(receivingPDFWidth-105, receivingPDFHeight-22)
			_ = pdf.Cell(nil, fmt.Sprintf("Страница %d", page))
			if err := addPage(); err != nil {
				return nil, 0, err
			}
		}
		if index%2 == 1 {
			pdf.SetFillColor(249, 250, 249)
			pdf.RectFromUpperLeftWithStyle(margin, y, tableWidth, rowHeight, "F")
		}
		pdf.SetStrokeColor(218, 223, 219)
		pdf.Line(margin, y+rowHeight, margin+tableWidth, y+rowHeight)
		x := margin
		values := []string{
			fmt.Sprintf("%d", index+1), shortenPDFText(row.Name, 58), sizePDFText(row), packagePDFText(row), fmt.Sprintf("%d", row.Quantity), formatPDFRUB(row.Price), "□",
		}
		for column, value := range values {
			pdf.SetTextColor(40, 48, 43)
			size := 9.0
			if column == 1 {
				size = 9.2
			}
			if column == 6 {
				size = 15
			}
			if err := pdf.SetFont("DejaVu", "", size); err != nil {
				return nil, 0, err
			}
			pdf.SetXY(x+4, y+7)
			if err := pdf.CellWithOption(&gopdf.Rect{W: widths[column] - 8, H: 13}, value, gopdf.CellOption{Align: gopdf.Left}); err != nil {
				return nil, 0, err
			}
			if column == 1 && row.InvoiceName != "" && !strings.EqualFold(strings.TrimSpace(row.InvoiceName), strings.TrimSpace(row.Name)) {
				pdf.SetTextColor(112, 118, 114)
				if err := pdf.SetFont("DejaVu", "", 6.8); err != nil {
					return nil, 0, err
				}
				pdf.SetXY(x+4, y+21)
				if err := pdf.CellWithOption(&gopdf.Rect{W: widths[column] - 8, H: 9}, "Инвойс: "+shortenPDFText(row.InvoiceName, 72), gopdf.CellOption{Align: gopdf.Left}); err != nil {
					return nil, 0, err
				}
			}
			x += widths[column]
		}
		y += rowHeight
	}
	pdf.SetTextColor(100, 105, 102)
	_ = pdf.SetFont("DejaVu", "", 8)
	pdf.SetXY(receivingPDFWidth-105, receivingPDFHeight-22)
	_ = pdf.Cell(nil, fmt.Sprintf("Страница %d", page))
	pdf.SetXY(margin, receivingPDFHeight-22)
	_ = pdf.Cell(nil, fmt.Sprintf("Итого: %d позиций · %d шт.", len(rows), receivingTotalUnits(rows)))

	content, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, 0, fmt.Errorf("render receiving PDF: %w", err)
	}
	return content, len(rows), nil
}

func receivingTotalUnits(rows []receivingPDFRow) int {
	total := 0
	for _, row := range rows {
		total += row.Quantity
	}
	return total
}

func blankDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return strings.TrimSpace(value)
}

func shortenPDFText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit < 2 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func sizePDFText(row receivingPDFRow) string {
	parts := make([]string, 0, 2)
	if row.Pot != nil && *row.Pot > 0 {
		parts = append(parts, fmt.Sprintf("D%g", *row.Pot))
	}
	if row.Height != nil && *row.Height > 0 {
		parts = append(parts, fmt.Sprintf("H%g см", *row.Height))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

func packagePDFText(row receivingPDFRow) string {
	if row.Units == nil || row.Packages == nil || *row.Units <= 0 || *row.Packages <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d × %d", *row.Units, *row.Packages)
}

func formatPDFRUB(value int64) string {
	return fmt.Sprintf("%d ₽", value)
}
