package photos

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	// Форматы регистрируются ради их декодировщиков: СБИС отдаёт и то, и другое.
	_ "image/gif"
	_ "image/png"
)

// Size — один из размеров, в которых храним фотографию.
type Size struct {
	Name    string
	MaxSide int
}

// Три размера закрывают все места, где показывается растение: карточка
// товара, плитка каталога и совсем мелкая картинка в корзине.
var Sizes = []Size{
	{Name: "large", MaxSide: 1200},
	{Name: "card", MaxSide: 600},
	{Name: "thumb", MaxSide: 200},
}

// Fit уменьшает картинку так, чтобы длинная сторона не превышала maxSide.
// Растягивать не станет: увеличенная фотография выглядит хуже исходной.
//
// Считаем средний цвет по площади, а не берём каждый n-й пиксель. При
// уменьшении в пять раз выборка теряет тонкие линии — прожилки на листе
// осыпаются в грязь, — а усреднение их сохраняет.
func Fit(source image.Image, maxSide int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || maxSide <= 0 {
		return source
	}
	longest := width
	if height > longest {
		longest = height
	}
	if longest <= maxSide {
		return source
	}

	targetWidth := width * maxSide / longest
	targetHeight := height * maxSide / longest
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetHeight < 1 {
		targetHeight = 1
	}

	target := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		fromY := bounds.Min.Y + y*height/targetHeight
		toY := bounds.Min.Y + (y+1)*height/targetHeight
		if toY <= fromY {
			toY = fromY + 1
		}
		for x := 0; x < targetWidth; x++ {
			fromX := bounds.Min.X + x*width/targetWidth
			toX := bounds.Min.X + (x+1)*width/targetWidth
			if toX <= fromX {
				toX = fromX + 1
			}
			var red, green, blue, alpha, count uint64
			for sourceY := fromY; sourceY < toY; sourceY++ {
				for sourceX := fromX; sourceX < toX; sourceX++ {
					r, g, b, a := source.At(sourceX, sourceY).RGBA()
					red += uint64(r)
					green += uint64(g)
					blue += uint64(b)
					alpha += uint64(a)
					count++
				}
			}
			if count == 0 {
				count = 1
			}
			target.Set(x, y, color.RGBA64{
				R: uint16(red / count),
				G: uint16(green / count),
				B: uint16(blue / count),
				A: uint16(alpha / count),
			})
		}
	}
	return target
}

// Prepare разбирает исходный файл и возвращает готовый к отправке JPEG.
func Prepare(raw []byte, maxSide int) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("не разобрать изображение: %w", err)
	}
	var out bytes.Buffer
	// Восемьдесят два — граница, за которой глаз перестаёт замечать разницу,
	// а вес растёт заметно.
	if err := jpeg.Encode(&out, Fit(source, maxSide), &jpeg.Options{Quality: 82}); err != nil {
		return nil, fmt.Errorf("не собрать JPEG: %w", err)
	}
	return out.Bytes(), nil
}
