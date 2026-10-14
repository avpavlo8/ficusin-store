package admin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/signintech/gopdf"
)

func returnLabelMoney(value float64) string {
	parts := strings.Split(fmt.Sprintf("%.2f", value), ".")
	whole := parts[0]
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + " " + whole[i:]
	}
	if parts[1] != "00" {
		whole += "," + parts[1]
	}
	return whole + " руб."
}

// Thermal output stays black and white. Bold embedded fonts keep the name and
// price legible on a physical 58 x 40 mm label, not only on a zoomed preview.
func BuildReturnLabels(items []MarketplaceReturn, includeDate bool) ([]byte, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("выберите растения для этикеток")
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 58 * 72 / 25.4, H: 40 * 72 / 25.4}})
	regular := strings.TrimSpace(os.Getenv("PROCUREMENT_PDF_FONT"))
	if regular == "" {
		regular = "/app/fonts/DejaVuSans.ttf"
	}
	bold := strings.TrimSpace(os.Getenv("RETURN_LABEL_BOLD_FONT"))
	if bold == "" {
		bold = filepath.Join(filepath.Dir(regular), "DejaVuSans-Bold.ttf")
		if filepath.Base(regular) == "Arial.ttf" {
			bold = filepath.Join(filepath.Dir(regular), "Arial Bold.ttf")
		}
	}
	if err := pdf.AddTTFFont("regular", regular); err != nil {
		return nil, err
	}
	if err := pdf.AddTTFFont("bold", bold); err != nil {
		return nil, err
	}
	text := func(x, y, size float64, font, value string) error {
		if err := pdf.SetFont(font, "", size); err != nil {
			return err
		}
		pdf.SetXY(x, y)
		return pdf.Cell(nil, value)
	}
	for _, item := range items {
		pdf.AddPage()
		if err := text(8, 5, 7.2, "bold", "ФИКУСИН"); err != nil {
			return nil, err
		}
		if err := text(8, 14, 6.5, "regular", "На восстановлении · ещё расцветёт"); err != nil {
			return nil, err
		}
		// A small outlined sprout gives the label a botanical identity without a
		// photograph, grey background or heavy ink consumption.
		pdf.SetLineWidth(.7)
		pdf.Curve(150, 17, 146, 9, 154, 5, 157, 6, "D")
		pdf.Curve(157, 6, 157, 13, 153, 16, 150, 17, "D")
		pdf.Line(150, 17, 155, 9)
		var lines []string
		size := 12.0
		for ; size >= 7; size -= .25 {
			if err := pdf.SetFont("bold", "", size); err != nil {
				return nil, err
			}
			lines = []string{}
			line := ""
			for _, word := range strings.Fields(item.ProductName) {
				candidate := strings.TrimSpace(line + " " + word)
				width, err := pdf.MeasureTextWidth(candidate)
				if err != nil {
					return nil, err
				}
				if width > 148 && line != "" {
					lines = append(lines, line)
					line = word
				} else {
					line = candidate
				}
			}
			if line != "" {
				lines = append(lines, line)
			}
			fits := len(lines) <= 3
			for _, line := range lines {
				width, err := pdf.MeasureTextWidth(line)
				if err != nil {
					return nil, err
				}
				fits = fits && width <= 148
			}
			if fits {
				break
			}
		}
		if size < 7 {
			return nil, fmt.Errorf("название «%s» слишком длинное для этикетки 58×40 мм", item.ProductName)
		}
		for i, line := range lines {
			if err := text(8, 25+float64(i)*10.5, size, "bold", line); err != nil {
				return nil, err
			}
		}
		pdf.SetLineWidth(.45)
		pdf.Line(8, 60, 156, 60)
		price := returnLabelMoney(item.DiscountedPrice)
		priceSize := 22.0
		for {
			if err := pdf.SetFont("bold", "", priceSize); err != nil {
				return nil, err
			}
			width, err := pdf.MeasureTextWidth(price)
			if err != nil {
				return nil, err
			}
			if width <= 148 {
				break
			}
			priceSize -= .5
			if priceSize < 10 {
				return nil, fmt.Errorf("слишком большая цена для этикетки")
			}
		}
		if err := text(8, 63, priceSize, "bold", price); err != nil {
			return nil, err
		}
		note := "Цена этого экземпляра"
		if item.DiscountPercent > 0 {
			note = "Скидка " + strconv.Itoa(item.DiscountPercent) + "% · было " + returnLabelMoney(item.Price)
		}
		if err := text(8, 87, 6.8, "regular", note); err != nil {
			return nil, err
		}
		if err := text(8, 97, 7, "bold", item.Article); err != nil {
			return nil, err
		}
		right := item.Label
		if includeDate {
			date, err := time.Parse("2006-01-02", item.ReturnedAt)
			if err != nil {
				return nil, err
			}
			if right != "" {
				right += " · "
			}
			right += date.Format("02.01.06")
		}
		if err := pdf.SetFont("regular", "", 6.8); err != nil {
			return nil, err
		}
		width, err := pdf.MeasureTextWidth(right)
		if err != nil {
			return nil, err
		}
		if width > 75 {
			return nil, fmt.Errorf("размер растения слишком длинный для этикетки")
		}
		if err := text(156-width, 97, 6.8, "regular", right); err != nil {
			return nil, err
		}
	}
	return pdf.GetBytesPdfReturnErr()
}
