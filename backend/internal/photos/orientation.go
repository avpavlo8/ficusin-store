package photos

import (
	"bytes"
	"encoding/binary"
	"image"
)

// Decode applies the EXIF transform before the pixels are resized or re-encoded.
func Decode(raw []byte) (image.Image, error) {
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	orientation := exifOrientation(exifData(raw))
	if orientation < 2 || orientation > 8 {
		return source, nil
	}

	b := source.Bounds()
	w, h := b.Dx(), b.Dy()
	targetBounds := image.Rect(0, 0, w, h)
	if orientation >= 5 {
		targetBounds = image.Rect(0, 0, h, w)
	}
	target := image.NewRGBA(targetBounds)
	for y := 0; y < targetBounds.Dy(); y++ {
		for x := 0; x < targetBounds.Dx(); x++ {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			target.Set(x, y, source.At(sx+b.Min.X, sy+b.Min.Y))
		}
	}
	return target, nil
}

func exifData(raw []byte) []byte {
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xd8}):
		raw = raw[2:]
		for len(raw) >= 4 && raw[0] == 0xff {
			marker := raw[1]
			if marker == 0xff {
				raw = raw[1:]
				continue
			}
			if marker == 0xda || marker == 0xd9 {
				break
			}
			size := int(binary.BigEndian.Uint16(raw[2:4]))
			if size < 2 || size > len(raw)-2 {
				break
			}
			if marker == 0xe1 && bytes.HasPrefix(raw[4:size+2], []byte("Exif\x00\x00")) {
				return raw[10 : size+2]
			}
			raw = raw[size+2:]
		}
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		raw = raw[8:]
		for len(raw) >= 12 {
			size := int(binary.BigEndian.Uint32(raw[:4]))
			if size > len(raw)-12 {
				break
			}
			if string(raw[4:8]) == "eXIf" {
				return raw[8 : 8+size]
			}
			raw = raw[size+12:]
		}
	case len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		raw = raw[12:]
		for len(raw) >= 8 {
			size := int(binary.LittleEndian.Uint32(raw[4:8]))
			padded := size + size%2
			if padded > len(raw)-8 {
				break
			}
			if string(raw[:4]) == "EXIF" {
				return bytes.TrimPrefix(raw[8:8+size], []byte("Exif\x00\x00"))
			}
			raw = raw[padded+8:]
		}
	}
	return nil
}

func exifOrientation(tiff []byte) uint16 {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	offset := int(order.Uint32(tiff[4:8]))
	if order.Uint16(tiff[2:4]) != 42 || offset < 8 || offset > len(tiff)-2 {
		return 1
	}
	ifd := tiff[offset:]
	count := int(order.Uint16(ifd[:2]))
	ifd = ifd[2:]
	for i := 0; i < count && len(ifd) >= 12; i++ {
		if order.Uint16(ifd[:2]) == 0x0112 && order.Uint16(ifd[2:4]) == 3 && order.Uint32(ifd[4:8]) == 1 {
			return order.Uint16(ifd[8:10])
		}
		ifd = ifd[12:]
	}
	return 1
}
