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

func TestPrepareAppliesCameraOrientationBeforeJPEGEncoding(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 36, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 36; x++ {
			shade := uint8(40 + x/18*60 + y/12*120)
			source.SetRGBA(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	for orientation, want := range [][4]uint8{
		{40, 100, 160, 220}, {100, 40, 220, 160},
		{220, 160, 100, 40}, {160, 220, 40, 100},
		{40, 160, 100, 220}, {160, 40, 220, 100},
		{220, 100, 160, 40}, {100, 220, 40, 160},
	} {
		ifd := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0,
			0x12, 0x01, 3, 0, 1, 0, 0, 0, byte(orientation + 1), 0, 0, 0, 0, 0, 0, 0}
		jpegWithExif := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(ifd) + 8)}, []byte("Exif\x00\x00")...)
		jpegWithExif = append(jpegWithExif, ifd...)
		jpegWithExif = append(jpegWithExif, encoded.Bytes()[2:]...)
		result, err := Prepare(jpegWithExif, 18)
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(bytes.NewReader(result))
		if err != nil {
			t.Fatal(err)
		}
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if orientation < 4 && (w != 18 || h != 12) || orientation >= 4 && (w != 12 || h != 18) {
			t.Fatalf("orientation %d: got %dx%d", orientation+1, w, h)
		}
		for i, p := range []image.Point{{X: w / 4, Y: h / 4}, {X: 3 * w / 4, Y: h / 4}, {X: w / 4, Y: 3 * h / 4}, {X: 3 * w / 4, Y: 3 * h / 4}} {
			value := color.GrayModel.Convert(img.At(p.X, p.Y)).(color.Gray).Y
			if diff := int(value) - int(want[i]); diff < -5 || diff > 5 {
				t.Fatalf("orientation %d corner %d: got %d want %d", orientation+1, i, value, want[i])
			}
		}
	}
}
