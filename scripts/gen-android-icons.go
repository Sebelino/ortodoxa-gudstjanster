//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var (
	bgColor    = color.RGBA{0x1a, 0x36, 0x5d, 0xff}
	crossColor = color.RGBA{0xd4, 0xaf, 0x37, 0xff}
)

func generateIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / 32.0
	fillRoundedRect(img, 0, 0, float64(size), float64(size), 4*s, bgColor)
	fillRect(img, 14*s, 3*s, 4*s, 26*s, crossColor)
	fillRect(img, 9*s, 6*s, 14*s, 3.5*s, crossColor)
	fillRect(img, 4*s, 13*s, 24*s, 4*s, crossColor)
	drawLine(img, 9*s, 23*s, 23*s, 26.5*s, 3*s, crossColor)
	return img
}

func fillRect(img *image.RGBA, x, y, w, h float64, c color.RGBA) {
	x0, y0 := int(math.Round(x)), int(math.Round(y))
	x1, y1 := int(math.Round(x+w)), int(math.Round(y+h))
	b := img.Bounds()
	for py := max(y0, b.Min.Y); py < min(y1, b.Max.Y); py++ {
		for px := max(x0, b.Min.X); px < min(x1, b.Max.X); px++ {
			img.SetRGBA(px, py, c)
		}
	}
}

func fillRoundedRect(img *image.RGBA, x, y, w, h, r float64, c color.RGBA) {
	b := img.Bounds()
	x1, y1 := x+w, y+h
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			fx, fy := float64(px)+0.5, float64(py)+0.5
			if fx < x || fx >= x1 || fy < y || fy >= y1 {
				continue
			}
			cx, cy := fx, fy
			if cx < x+r {
				cx = x + r
			} else if cx > x1-r {
				cx = x1 - r
			}
			if cy < y+r {
				cy = y + r
			} else if cy > y1-r {
				cy = y1 - r
			}
			dx, dy := fx-cx, fy-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(px, py, c)
			}
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1, width float64, c color.RGBA) {
	b := img.Bounds()
	halfW := width / 2.0
	dx, dy := x1-x0, y1-y0
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}
	nx, ny := -dy/length, dx/length
	minPx := int(math.Floor(math.Min(x0, x1) - halfW - 1))
	maxPx := int(math.Ceil(math.Max(x0, x1) + halfW + 1))
	minPy := int(math.Floor(math.Min(y0, y1) - halfW - 1))
	maxPy := int(math.Ceil(math.Max(y0, y1) + halfW + 1))
	for py := max(minPy, b.Min.Y); py < min(maxPy, b.Max.Y); py++ {
		for px := max(minPx, b.Min.X); px < min(maxPx, b.Max.X); px++ {
			fx, fy := float64(px)+0.5, float64(py)+0.5
			t := ((fx-x0)*dx + (fy-y0)*dy) / (length * length)
			capExtra := halfW / length
			if t < -capExtra || t > 1+capExtra {
				continue
			}
			dist := math.Abs((fx-x0)*nx + (fy-y0)*ny)
			if dist > halfW {
				continue
			}
			if t < 0 {
				d := math.Sqrt((fx-x0)*(fx-x0) + (fy-y0)*(fy-y0))
				if d > halfW {
					continue
				}
			} else if t > 1 {
				d := math.Sqrt((fx-x1)*(fx-x1) + (fy-y1)*(fy-y1))
				if d > halfW {
					continue
				}
			}
			img.SetRGBA(px, py, c)
		}
	}
}

func saveIcon(dir string, size int) error {
	img := generateIcon(size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	path := filepath.Join(dir, "ic_launcher.png")
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func main() {
	base := "android/app/src/main/res"
	icons := map[string]int{
		"mipmap-hdpi":    72,
		"mipmap-xhdpi":   96,
		"mipmap-xxhdpi":  144,
		"mipmap-xxxhdpi": 192,
	}
	for dir, size := range icons {
		path := filepath.Join(base, dir)
		if err := saveIcon(path, size); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s/ic_launcher.png (%dx%d)\n", path, size, size)
	}

	// Splash drawable (512px)
	splashDir := filepath.Join(base, "drawable")
	img := generateIcon(512)
	var buf bytes.Buffer
	png.Encode(&buf, img)
	splashPath := filepath.Join(splashDir, "splash.png")
	os.WriteFile(splashPath, buf.Bytes(), 0644)
	fmt.Printf("Generated %s (512x512)\n", splashPath)
}
