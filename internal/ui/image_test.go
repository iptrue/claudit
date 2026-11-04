package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"io"
	"strings"
	"testing"
)

func imageTestPNGHeader(width, height uint32) []byte {
	data := make([]byte, 33)
	copy(data, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	data[24], data[25] = 8, 6 // 8-bit RGBA; no IDAT or bitmap allocation.
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestImageRejectsExcessivePixelsBeforeDecode(t *testing.T) {
	for _, dims := range [][2]uint32{{100000, 100000}, {8193, 8192}} {
		data := imageTestPNGHeader(dims[0], dims[1])
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != int(dims[0]) || cfg.Height != int(dims[1]) {
			t.Fatalf("fixture config: %+v, %v", cfg, err)
		}
		got, err := imageFromData(data)
		if err == nil || !strings.Contains(err.Error(), "pixel limit") || len(got.Data) != 0 {
			t.Fatalf("%dx%d image: err=%v, data=%d bytes", dims[0], dims[1], err, len(got.Data))
		}
	}
}

func TestImagePixelLimitAllowsBoundary(t *testing.T) {
	// The missing IDAT proves the accepted header reaches Decode without allocating pixels.
	_, err := imageFromData(imageTestPNGHeader(8192, 8192))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("pixel limit boundary: %v", err)
	}
}

func TestImageRejectsZeroJPEGDimensions(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	sof := bytes.Index(b.Bytes(), []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("fixture has no JPEG frame header")
	}
	for _, offset := range []int{5, 7} {
		data := bytes.Clone(b.Bytes())
		data[sof+offset], data[sof+offset+1] = 0, 0
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			t.Fatalf("zero dimensions must reach application validation: %v", err)
		}
		if got, err := imageFromData(data); err == nil || len(got.Data) != 0 {
			t.Fatalf("zero dimension: err=%v, data=%d bytes", err, len(got.Data))
		}
	}
}
