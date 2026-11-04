package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"testing"
)

func imageTestJPEGOrientation(data []byte, orientation uint16, order binary.ByteOrder) []byte {
	tiff := make([]byte, 26)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:4], 42)
	order.PutUint32(tiff[4:8], 8)
	order.PutUint16(tiff[8:10], 1)
	order.PutUint16(tiff[10:12], 0x112)
	order.PutUint16(tiff[12:14], 3)
	order.PutUint32(tiff[14:18], 1)
	order.PutUint16(tiff[18:20], orientation)
	segment := []byte{0xff, 0xe1, 0, byte(len(tiff) + 8)}
	segment = append(segment, []byte("Exif\x00\x00")...)
	segment = append(segment, tiff...)
	return append(append(append([]byte{}, data[:2]...), segment...), data[2:]...)
}

func TestImageResizeAppliesJPEGOrientation(t *testing.T) {
	red := color.RGBA{240, 20, 20, 255}
	green := color.RGBA{20, 240, 20, 255}
	blue := color.RGBA{20, 20, 240, 255}
	yellow := color.RGBA{240, 240, 20, 255}
	src := image.NewRGBA(image.Rect(0, 0, 2500, 500))
	for i, c := range []color.RGBA{red, green, blue, yellow} {
		r := image.Rect((i%2)*1250, (i/2)*250, (i%2+1)*1250, (i/2+1)*250)
		draw.Draw(src, r, image.NewUniform(c), image.Point{}, draw.Src)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name        string
		orientation uint16
		order       binary.ByteOrder
		width       int
		height      int
		corners     [4]color.RGBA
	}{
		{"normal", 1, binary.LittleEndian, 2000, 400, [4]color.RGBA{red, green, blue, yellow}},
		{"mirror", 2, binary.LittleEndian, 2000, 400, [4]color.RGBA{green, red, yellow, blue}},
		{"rotate180", 3, binary.LittleEndian, 2000, 400, [4]color.RGBA{yellow, blue, green, red}},
		{"flip", 4, binary.LittleEndian, 2000, 400, [4]color.RGBA{blue, yellow, red, green}},
		{"transpose", 5, binary.LittleEndian, 400, 2000, [4]color.RGBA{red, blue, green, yellow}},
		{"rotate90", 6, binary.LittleEndian, 400, 2000, [4]color.RGBA{blue, red, yellow, green}},
		{"transverse", 7, binary.BigEndian, 400, 2000, [4]color.RGBA{yellow, green, blue, red}},
		{"rotate270", 8, binary.BigEndian, 400, 2000, [4]color.RGBA{green, yellow, red, blue}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := imageFromData(imageTestJPEGOrientation(b.Bytes(), tt.orientation, tt.order))
			if err != nil {
				t.Fatal(err)
			}
			im, _, err := image.Decode(bytes.NewReader(got.Data))
			if err != nil {
				t.Fatal(err)
			}
			if im.Bounds().Dx() != tt.width || im.Bounds().Dy() != tt.height {
				t.Fatalf("oriented JPEG: %v, bounds=%v, want %dx%d", err, im.Bounds(), tt.width, tt.height)
			}
			for i, want := range tt.corners {
				x, y := 10, 10
				if i%2 != 0 {
					x = tt.width - 11
				}
				if i/2 != 0 {
					y = tt.height - 11
				}
				c := color.RGBAModel.Convert(im.At(x, y)).(color.RGBA)
				if math.Abs(float64(c.R)-float64(want.R)) > 8 || math.Abs(float64(c.G)-float64(want.G)) > 8 || math.Abs(float64(c.B)-float64(want.B)) > 8 {
					t.Errorf("corner %d = %v, want %v", i, c, want)
				}
			}
		})
	}
}

func TestJPEGOrientationIgnoresMalformedMetadata(t *testing.T) {
	valid := imageTestJPEGOrientation([]byte{0xff, 0xd8, 0xff, 0xd9}, 6, binary.LittleEndian)
	for n := 0; n < len(valid)-2; n++ {
		if got := jpegOrientation(valid[:n]); got != 1 {
			t.Errorf("truncated at %d: orientation=%d", n, got)
		}
	}
	for _, tt := range []struct {
		name   string
		offset int
		value  []byte
	}{
		{"segment length", 4, []byte{0xff, 0xff}},
		{"byte order", 12, []byte("XX")},
		{"TIFF magic", 14, []byte{0, 0}},
		{"IFD offset", 16, []byte{0xff, 0xff, 0xff, 0xff}},
		{"entry type", 24, []byte{4, 0}},
		{"entry count", 26, []byte{0xff, 0xff, 0xff, 0xff}},
		{"orientation range", 30, []byte{9, 0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := bytes.Clone(valid)
			copy(data[tt.offset:], tt.value)
			if got := jpegOrientation(data); got != 1 {
				t.Errorf("malformed metadata: orientation=%d", got)
			}
		})
	}
}

func TestImageSmallJPEGPreservesOrientationMetadata(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	data := imageTestJPEGOrientation(b.Bytes(), 6, binary.LittleEndian)
	got, err := imageFromData(data)
	if err != nil || !bytes.Equal(got.Data, data) {
		t.Fatalf("small JPEG was re-encoded: %v", err)
	}
}
