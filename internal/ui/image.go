package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"slices"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/pixelbender/claudit/internal/protocol"
)

// Bound source allocations before decoding pixels (256 MiB for 8-bit RGBA).
const imageMaxPixels = 64 * 1024 * 1024

func imageFromData(data []byte) (protocol.Image, error) {
	media := http.DetectContentType(data)
	if !slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, media) {
		return protocol.Image{}, fmt.Errorf("not a picture claude reads")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return protocol.Image{}, err
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		return protocol.Image{}, fmt.Errorf("invalid image dimensions: %dx%d", cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > imageMaxPixels {
		return protocol.Image{}, fmt.Errorf("image exceeds pixel limit of %d: %dx%d", imageMaxPixels, cfg.Width, cfg.Height)
	}
	side := max(cfg.Width, cfg.Height)
	if len(data) <= imageBytes && side <= imageSide {
		return protocol.Image{Media: media, Data: data}, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return protocol.Image{}, err
	}
	if media == "image/jpeg" {
		if orientation := jpegOrientation(data); orientation != 1 {
			src = orientedImage{src, orientation}
			cfg.Width, cfg.Height = src.Bounds().Dx(), src.Bounds().Dy()
		}
	}
	for target := min(side, imageSide); ; target = max(1, target*3/4) {
		w, h := max(1, cfg.Width*target/side), max(1, cfg.Height*target/side)
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		// JPEG has no alpha; composite transparent pixels onto white.
		draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		for _, quality := range []int{95, 85, 75, 60, 40} {
			var b bytes.Buffer
			if err := jpeg.Encode(&b, dst, &jpeg.Options{Quality: quality}); err != nil {
				return protocol.Image{}, err
			}
			if b.Len() <= imageBytes {
				return protocol.Image{Media: "image/jpeg", Data: b.Bytes()}, nil
			}
		}
	}
}
