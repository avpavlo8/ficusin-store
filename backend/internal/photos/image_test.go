package photos

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func solid(width, height int, shade color.RGBA) image.Image {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, shade)
		}
	}
	return picture
}

func TestFitKeepsProportions(t *testing.T) {
	small := Fit(solid(3000, 1500, color.RGBA{R: 10, G: 120, B: 40, A: 255}), 600)
	bounds := small.Bounds()
	if bounds.Dx() != 600 || bounds.Dy() != 300 {
		t.Fatalf("ожидали 600x300, получили %dx%d", bounds.Dx(), bounds.Dy())
	}
}

// Растягивать нельзя: увеличенная фотография выглядит хуже исходной.
func TestFitLeavesSmallAlone(t *testing.T) {
	source := solid(100, 50, color.RGBA{A: 255})
	if Fit(source, 600).Bounds() != source.Bounds() {
		t.Fatal("маленькую картинку растянули")
	}
}

// Усреднение по площади не должно менять цвет однотонного снимка.
func TestFitKeepsColour(t *testing.T) {
	want := color.RGBA{R: 10, G: 120, B: 40, A: 255}
	small := Fit(solid(900, 900, want), 90)
	r, g, b, _ := small.At(45, 45).RGBA()
	if r>>8 != uint32(want.R) || g>>8 != uint32(want.G) || b>>8 != uint32(want.B) {
		t.Fatalf("цвет поплыл: %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestPrepareMakesJpeg(t *testing.T) {
	var source bytes.Buffer
	if err := jpeg.Encode(&source, solid(1200, 800, color.RGBA{R: 200, G: 200, B: 200, A: 255}), nil); err != nil {
		t.Fatalf("не подготовить исходник: %v", err)
	}
	ready, err := Prepare(source.Bytes(), 200)
	if err != nil {
		t.Fatalf("Prepare не справился: %v", err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(ready))
	if err != nil || format != "jpeg" {
		t.Fatalf("на выходе не JPEG: %v %s", err, format)
	}
	if decoded.Bounds().Dx() != 200 {
		t.Fatalf("ожидали ширину 200, получили %d", decoded.Bounds().Dx())
	}
	if len(ready) >= source.Len() {
		t.Errorf("уменьшенный файл не легче исходного: %d против %d", len(ready), source.Len())
	}
}

func TestPrepareRejectsGarbage(t *testing.T) {
	if _, err := Prepare([]byte("это не картинка"), 200); err == nil {
		t.Fatal("мусор приняли за изображение")
	}
}
