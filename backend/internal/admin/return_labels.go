package admin

import (
	"fmt"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/ean"
	"github.com/signintech/gopdf"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// Saby-style thermal labels: centred shop, prominent name and price, barcode.
// The product barcode is authoritative; RET identifies units without one.
func BuildReturnLabels(items []MarketplaceReturn, includeDate bool) ([]byte, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("выберите растения для этикеток")
	}
	const pageWidth = 58 * 72 / 25.4
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageWidth, H: 40 * 72 / 25.4}})
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
	centre := func(y, size float64, font, value string) error {
		if err := pdf.SetFont(font, "", size); err != nil {
			return err
		}
		width, err := pdf.MeasureTextWidth(value)
		if err != nil {
			return err
		}
		pdf.SetXY((pageWidth-width)/2, y)
		return pdf.Cell(nil, value)
	}
	for _, item := range items {
		pdf.AddPage()
		if err := centre(3, 7.8, "regular", "Магазин комнатных растений"); err != nil {
			return nil, err
		}
		if err := centre(12, 9, "regular", "«Фикусин»"); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(item.ProductName + " " + item.Label)
		var lines []string
		size := 12.0
		for ; size >= 6; size -= .25 {
			if err := pdf.SetFont("bold", "", size); err != nil {
				return nil, err
			}
			lines = nil
			line := ""
			for _, word := range strings.Fields(name) {
				candidate := strings.TrimSpace(line + " " + word)
				w, err := pdf.MeasureTextWidth(candidate)
				if err != nil {
					return nil, err
				}
				if w <= 148 {
					line = candidate
					continue
				}
				if line != "" {
					lines = append(lines, line)
				}
				line = ""
				// Long uninterrupted catalogue values must wrap as well as words.
				for _, letter := range word {
					candidate = line + string(letter)
					w, err = pdf.MeasureTextWidth(candidate)
					if err != nil {
						return nil, err
					}
					if w > 148 && line != "" {
						lines = append(lines, line)
						line = string(letter)
					} else {
						line = candidate
					}
				}
			}
			if line != "" {
				lines = append(lines, line)
			}
			if len(lines) <= 2 {
				break
			}
		}
		if size < 6 {
			return nil, fmt.Errorf("сократите название растения «%s» для этикетки 58×40 мм", name)
		}
		y := 30.0
		if len(lines) > 1 {
			y = 24
		}
		for i, line := range lines {
			if err := centre(y+float64(i)*12, size, "bold", line); err != nil {
				return nil, err
			}
		}
		price := item.Price
		if item.DiscountPercent > 0 {
			price = item.DiscountedPrice
		}
		priceText := returnLabelMoney(price)
		priceSize := 21.0
		for {
			if err := pdf.SetFont("bold", "", priceSize); err != nil {
				return nil, err
			}
			w, err := pdf.MeasureTextWidth(priceText)
			if err != nil {
				return nil, err
			}
			if w <= 148 {
				break
			}
			priceSize -= .5
			if priceSize < 9 {
				return nil, fmt.Errorf("цена слишком длинная для этикетки")
			}
		}
		if err := centre(49, priceSize, "bold", priceText); err != nil {
			return nil, err
		}
		if item.DiscountPercent > 0 {
			if err := centre(71, 6.2, "regular", fmt.Sprintf("Скидка %d%% · было %s", item.DiscountPercent, returnLabelMoney(item.Price))); err != nil {
				return nil, err
			}
		}
		value := item.Barcode
		if value == "" {
			value = item.Article
		}
		if value == "" {
			value = fmt.Sprintf("RET-%06d", item.ID)
		}
		encoded, err := ean.Encode(value)
		if err != nil {
			encoded, err = code128.Encode(value)
		}
		if err != nil {
			return nil, fmt.Errorf("не удалось напечатать штрихкод экземпляра %s", item.Article)
		}
		modules := encoded.Bounds().Dx()
		// Leave at least eleven narrow modules of quiet space for EAN13.
		const barcodeWidth = 126.0
		moduleWidth := barcodeWidth / float64(modules)
		pdf.SetFillColor(0, 0, 0)
		for x := 0; x < modules; x++ {
			r, g, b, _ := encoded.At(x, 0).RGBA()
			if r+g+b < 3*32768 {
				pdf.RectFromUpperLeftWithStyle((pageWidth-barcodeWidth)/2+float64(x)*moduleWidth, 81, moduleWidth, 15, "F")
			}
		}
		if err = centre(97, 7, "regular", value); err != nil {
			return nil, err
		}
		footer := item.Article
		if includeDate {
			date, parseErr := time.Parse("2006-01-02", item.ReturnedAt)
			if parseErr != nil {
				return nil, fmt.Errorf("проверьте дату возврата для %s", item.Article)
			}
			footer += " · " + date.Format("02.01.2006")
		}
		if err = centre(106, 5.8, "regular", footer); err != nil {
			return nil, err
		}
	}
	return pdf.GetBytesPdfReturnErr()
}
