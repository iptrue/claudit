package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// jpegOrientation reads only the TIFF Orientation entry in an EXIF APP1 segment.
func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return 1
		}
		for i < len(data) && data[i] == 0xff {
			i++
		}
		if i >= len(data) || data[i] == 0xda || data[i] == 0xd9 {
			return 1
		}
		marker := data[i]
		i++
		if i+2 > len(data) {
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[i : i+2]))
		if n < 2 || n > len(data)-i {
			return 1
		}
		segment := data[i+2 : i+n]
		i += n
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
	}
	return 1
}

func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset+2 > uint64(len(data)) {
		return 1
	}
	i := int(offset)
	count := int(order.Uint16(data[i : i+2]))
	i += 2
	for range count {
		if len(data)-i < 12 {
			return 1
		}
		entry := data[i : i+12]
		i += 12
		if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			if orientation := int(order.Uint16(entry[8:10])); orientation >= 1 && orientation <= 8 {
				return orientation
			}
		}
	}
	return 1
}

// orientedImage exposes rotated or mirrored pixels without allocating another source bitmap.
type orientedImage struct {
	image.Image
	orientation int
}

func (im orientedImage) Bounds() image.Rectangle {
	w, h := im.Image.Bounds().Dx(), im.Image.Bounds().Dy()
	if im.orientation >= 5 {
		w, h = h, w
	}
	return image.Rect(0, 0, w, h)
}

func (im orientedImage) At(x, y int) color.Color {
	b := im.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch im.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return im.Image.At(b.Min.X+x, b.Min.Y+y)
}
