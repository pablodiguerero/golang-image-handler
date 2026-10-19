package imagehandler

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
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

func vipsAvailable(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"vipsthumbnail", "vips"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("libvips tools are not installed")
		}
	}
}

func writePNG(t *testing.T, path string, width, height int, c color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func runTransform(t *testing.T, source, output string, spec requestSpec, format outputFormat) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := transformImage(ctx, source, output, spec, format); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestVipsJPEGFlattensTransparencyOntoWhite(t *testing.T) {
	vipsAvailable(t)
	root := t.TempDir()
	source := filepath.Join(root, "alpha.png")
	writePNG(t, source, 40, 20, color.NRGBA{A: 0})
	for _, operation := range []string{"fit", "fill", "fitstrict", "shrink", "original"} {
		t.Run(operation, func(t *testing.T) {
			spec := requestSpec{operation: operation, width: 30, height: 30, quality: 90}
			content := runTransform(t, source, filepath.Join(root, operation+".jpg"), spec, formatJPEG)
			decoded, err := jpeg.Decode(bytes.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			r, g, b, _ := decoded.At(0, 0).RGBA()
			if r < 65000 || g < 65000 || b < 65000 {
				t.Fatalf("corner is not white: %d %d %d", r>>8, g>>8, b>>8)
			}
		})
	}
	t.Run("webp keeps alpha", func(t *testing.T) {
		spec := requestSpec{operation: "fit", width: 30, height: 30, quality: 90}
		output := filepath.Join(root, "alpha.webp")
		runTransform(t, source, output, spec, formatWebP)
		out, err := exec.Command("vipsheader", "-f", "bands", output).Output()
		if err != nil || string(bytes.TrimSpace(out)) != "4" {
			t.Fatalf("webp bands=%q err=%v, want 4 (alpha kept)", out, err)
		}
	})
}

func TestVipsShrinkNeverUpscales(t *testing.T) {
	vipsAvailable(t)
	root := t.TempDir()
	source := filepath.Join(root, "small.png")
	writePNG(t, source, 200, 150, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	for _, test := range []struct {
		name          string
		spec          requestSpec
		width, height int
	}{
		{"larger box keeps size", requestSpec{operation: "shrink", width: 500, height: 700}, 200, 150},
		{"unbounded height keeps size", requestSpec{operation: "shrink", width: 1920, height: 8192, zeroHeight: true}, 200, 150},
		{"unbounded width keeps size", requestSpec{operation: "shrink", width: 8192, height: 900, zeroWidth: true}, 200, 150},
		{"smaller box shrinks", requestSpec{operation: "shrink", width: 100, height: 100}, 100, 75},
		{"unbounded height shrinks", requestSpec{operation: "shrink", width: 50, height: 8192, zeroHeight: true}, 50, 38},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.spec.quality = 80
			content := runTransform(t, source, filepath.Join(root, "out.jpg"), test.spec, formatJPEG)
			decoded, err := jpeg.Decode(bytes.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			if b := decoded.Bounds(); b.Dx() != test.width || b.Dy() != test.height {
				t.Fatalf("got %dx%d, want %dx%d", b.Dx(), b.Dy(), test.width, test.height)
			}
		})
	}
}

func TestVipsLoadsGIFAndTIFF(t *testing.T) {
	vipsAvailable(t)
	root := t.TempDir()
	pngSource := filepath.Join(root, "base.png")
	writePNG(t, pngSource, 40, 30, color.NRGBA{R: 200, A: 255})
	tiff := filepath.Join(root, "scan.tif")
	if out, err := exec.Command("vips", "copy", pngSource, tiff).CombinedOutput(); err != nil {
		t.Fatalf("make tiff: %v %s", err, out)
	}
	gifSource := filepath.Join(root, "anim.gif")
	var buffer bytes.Buffer
	palette := []color.Color{color.Black, color.White}
	frames := &gif.GIF{}
	for i := 0; i < 2; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, 40, 30), palette)
		frames.Image = append(frames.Image, frame)
		frames.Delay = append(frames.Delay, 5)
	}
	if err := gif.EncodeAll(&buffer, frames); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gifSource, buffer.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{tiff, gifSource} {
		for _, format := range []outputFormat{formatJPEG, formatWebP} {
			for _, operation := range []string{"fit", "original"} {
				spec := requestSpec{operation: operation, width: 20, height: 20, quality: 80}
				output := filepath.Join(root, filepath.Base(source)+"-"+operation+"."+string(format))
				if content := runTransform(t, source, output, spec, format); len(content) == 0 {
					t.Errorf("%s %s %s: empty output", filepath.Base(source), operation, format)
				}
			}
		}
	}
}
