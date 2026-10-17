package imagehandler

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestVipsTransforms(t *testing.T) {
	if _, err := exec.LookPath("vipsthumbnail"); err != nil {
		t.Skip("libvips tools are not installed")
	}
	if _, err := exec.LookPath("vips"); err != nil {
		t.Skip("libvips tools are not installed")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.png")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name          string
		operation     string
		format        outputFormat
		width, height int
	}{
		{"fit", "fit", formatJPEG, 10, 5},
		{"fill", "fill", formatJPEG, 10, 10},
		{"fitstrict", "fitstrict", formatJPEG, 10, 10},
		{"webp", "fit", formatWebP, 10, 5},
		{"avif", "fit", formatAVIF, 10, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(root, test.name+"."+string(test.format))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := transformImage(ctx, source, output, requestSpec{operation: test.operation, width: 10, height: 10, quality: 80}, test.format)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if test.format == formatWebP && (len(content) < 12 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WEBP") {
				t.Fatal("invalid WebP signature")
			}
			if test.format == formatAVIF && (len(content) < 12 || !bytes.Equal(content[4:8], []byte("ftyp"))) {
				t.Fatal("invalid AVIF signature")
			}
			if test.format == formatJPEG {
				decoded, err := jpeg.Decode(bytes.NewReader(content))
				if err != nil {
					t.Fatal(err)
				}
				bounds := decoded.Bounds()
				if bounds.Dx() != test.width || bounds.Dy() != test.height {
					t.Fatalf("got dimensions %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), test.width, test.height)
				}
				if test.operation == "fitstrict" {
					r, g, b, _ := decoded.At(0, 0).RGBA()
					if r < 60000 || g < 60000 || b < 60000 {
						t.Fatalf("expected white padding, got %d %d %d", r, g, b)
					}
				}
			}
		})
	}
}
