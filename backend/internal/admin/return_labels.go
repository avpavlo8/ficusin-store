package admin

import (
	"fmt"
	"github.com/signintech/gopdf"
	"os"
	"strings"
	"time"
)

func BuildReturnLabels(items []MarketplaceReturn, includeDate bool) ([]byte, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("выберите растения для этикеток")
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 58 * 72 / 25.4, H: 40 * 72 / 25.4}})
	font := strings.TrimSpace(os.Getenv("PROCUREMENT_PDF_FONT"))
	if font == "" {
		font = "/app/fonts/DejaVuSans.ttf"
	}
	if err := pdf.AddTTFFont("label", font); err != nil {
		return nil, err
	}
	for _, item := range items {
		pdf.AddPage()
		text := func(y, size float64, value string) error {
			if err := pdf.SetFont("label", "", size); err != nil {
				return err
			}
			pdf.SetXY(8, y)
			return pdf.Cell(nil, value)
		}
		if err := text(6, 7, "ФИКУСИН · НА ВОССТАНОВЛЕНИИ"); err != nil {
			return nil, err
		}
		// Fit a full name in three lines, shrinking long words without clipping.
		name := strings.TrimSpace(item.ProductName)
		if item.Label != "" {
			name += " · " + item.Label
		}
		size := 10.0
		var lines []string
		for {
			_ = pdf.SetFont("label", "", size)
			lines = []string{}
			line := ""
			for _, word := range strings.Fields(name) {
				candidate := strings.TrimSpace(line + " " + word)
				w, err := pdf.MeasureTextWidth(candidate)
				if err != nil {
					return nil, err
				}
				if w > 148 && line != "" {
					lines = append(lines, line)
					line = word
				} else {
					line = candidate
				}
			}
			if line != "" {
				lines = append(lines, line)
			}
			max := 0.0
			for _, line := range lines {
				w, _ := pdf.MeasureTextWidth(line)
				if w > max {
					max = w
				}
			}
			if len(lines) <= 3 && max <= 148 {
				break
			}
			size -= .5
			if size < 4 {
				return nil, fmt.Errorf("слишком длинное название для этикетки")
			}
		}
		for index, line := range lines {
			if err := text(21+float64(index)*12, size, line); err != nil {
				return nil, err
			}
		}
		if err := text(60, 8, item.Article); err != nil {
			return nil, err
		}
		if includeDate {
			date, err := time.Parse("2006-01-02", item.ReturnedAt)
			if err != nil {
				return nil, err
			}
			if err = text(72, 7, "Получено "+date.Format("02.01.2006")); err != nil {
				return nil, err
			}
		}
		if err := text(84, 13, fmt.Sprintf("%g руб.  (−%d%%)", item.DiscountedPrice, item.DiscountPercent)); err != nil {
			return nil, err
		}
		if err := text(102, 6, "Цена этого экземпляра с учётом состояния"); err != nil {
			return nil, err
		}
	}
	return pdf.GetBytesPdfReturnErr()
}
