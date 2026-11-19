package integration

import (
	"image"
	"image/color"
)

// WB can fit a landscape source into a portrait canvas. Strip paired black
// bars before comparing the seller CDN copy with the original site photo.
func wildberriesPhotoContentBounds(img image.Image) image.Rectangle {
	bounds := img.Bounds()
	y := blackPhotoBorders(bounds.Dy(), bounds.Dx(), func(axis, across int) color.Color {
		return img.At(bounds.Min.X+across, bounds.Min.Y+axis)
	})
	if y > 0 {
		return image.Rect(bounds.Min.X, bounds.Min.Y+y, bounds.Max.X, bounds.Max.Y-y)
	}
	x := blackPhotoBorders(bounds.Dx(), bounds.Dy(), func(axis, across int) color.Color {
		return img.At(bounds.Min.X+axis, bounds.Min.Y+across)
	})
	return image.Rect(bounds.Min.X+x, bounds.Min.Y, bounds.Max.X-x, bounds.Max.Y)
}

func blackPhotoBorders(length, width int, pixel func(axis, across int) color.Color) int {
	isBlack := func(line int) bool {
		stride := max(1, width/80)
		for across := 0; across < width; across += stride {
			if color.GrayModel.Convert(pixel(line, across)).(color.Gray).Y > 16 {
				return false
			}
		}
		return true
	}
	// The remaining source image must occupy at least half of the canvas.
	limit := length / 4
	leading, trailing := 0, 0
	for leading <= limit && isBlack(leading) {
		leading++
	}
	for trailing <= limit && isBlack(length-1-trailing) {
		trailing++
	}
	if leading < 3 || trailing < 3 || max(leading, trailing) > limit || max(leading, trailing)-min(leading, trailing) > max(2, length/100) {
		return 0
	}
	return min(leading, trailing)
}
