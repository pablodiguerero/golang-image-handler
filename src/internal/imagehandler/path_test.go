package imagehandler

import (
	"strings"
	"testing"
)

func TestParsePathLegacyAndModern(t *testing.T) {
	tests := []struct {
		path      string
		source    string
		operation string
		quality   int
	}{
		{"/images/files/realty-fill-100x100.jpg", "files/realty.jpg", "fill", 100},
		{"/images/files/realty.fill-100x100.jpg", "files/realty.jpg", "fill", 100},
		{"/images/files/realty-fit-800x600-q60.png", "files/realty.png", "fit", 60},
		{"/images/files/realty-fitstrict-200x200.webp", "files/realty.webp", "fitstrict", 100},
		{"/images/portrait-fit-100x100.avif", "portrait.avif", "fit", 100},
		{"/images/public+2026/backup@work/photo-fit-100x100.jpg", "public+2026/backup@work/photo.jpg", "fit", 100},
		{"/images/media/homepage_images_body/Сумка_Wilson_RF_Tournament_9PK_Black______.fitstrict-150x150.jpg", "media/homepage_images_body/Сумка_Wilson_RF_Tournament_9PK_Black______.jpg", "fitstrict", 100},
		{"/images/media/homepage_images_body/Сумка_Wilson_RF_Tournament_9PK_Black______.original.jpg", "media/homepage_images_body/Сумка_Wilson_RF_Tournament_9PK_Black______.jpg", "original", 100},
		{"/images/media/homepage_images/%25D0%25A2%25D0%25B5%25D0%25BD%25D0%25BD%25D0%25B8%25D1%2581%25D0%25BD%25D1%258B%25D0%25B5_%25D1%2580%25D0%25B0%25D0%25BA%25D0%25B5%25D1%2582%25D0%25BA%25D0%25B8_Wilson_%25D0%25BA%25D1%2583%25D0%25BF%25D0%25B8%25D1%2582%25D1%258C_.fill-900x700.jpg", "media/homepage_images/Теннисные_ракетки_Wilson_купить_.jpg", "fill", 100},
	}
	for _, test := range tests {
		spec, err := parsePath(test.path, 8192, 40_000_000)
		if err != nil {
			t.Fatalf("%s: %v", test.path, err)
		}
		if spec.sourceRelative != test.source || spec.operation != test.operation || spec.quality != test.quality {
			t.Errorf("%s: unexpected spec %+v", test.path, spec)
		}
	}
}

func TestParsePathRejectsUnsafeOrUnboundedRequests(t *testing.T) {
	paths := []string{
		"/images/../secret-fill-100x100.jpg",
		"/images/files/../secret-fill-100x100.jpg",
		"/images/files//secret-fill-100x100.jpg",
		"/images/files/secret-fill-0x100.jpg",
		"/images/files/secret-fill-99999x100.jpg",
		"/images/files/secret-fill-8000x8000.jpg",
		"/images/files/secret-fill-100x100-q0.jpg",
		"/images/files/secret-fill-100x100-q101.jpg",
		"/images/files/secret-fill-100x100.svg",
		"/images/files/.original.jpg",
		"/images/%252e%252e/secret-fill-100x100.jpg",
	}
	for _, path := range paths {
		if _, err := parsePath(path, 8192, 40_000_000); err == nil {
			t.Errorf("accepted unsafe path %q", path)
		}
	}
}

func TestNegotiateFormat(t *testing.T) {
	tests := []struct {
		accept     string
		avif, webp bool
		want       outputFormat
	}{
		{"image/avif,image/webp,image/*;q=0.8", true, true, formatAVIF},
		{"image/avif;q=0,image/webp", true, true, formatWebP},
		{"image/avif,image/webp", false, true, formatWebP},
		{"image/avif;q=0.5,image/webp;q=1", true, true, formatWebP},
		{"image/*,*/*", true, true, formatJPEG},
		{"image/webp", true, false, formatJPEG},
	}
	for _, test := range tests {
		if got := negotiateFormat(test.accept, test.avif, test.webp); got != test.want {
			t.Errorf("%q: got %s, want %s", test.accept, got, test.want)
		}
	}
}

func TestParsePathShrinkAndNewSources(t *testing.T) {
	tests := []struct {
		path                  string
		source, operation     string
		width, height         int
		zeroWidth, zeroHeight bool
	}{
		{"/images/a/p-shrink-500x700.jpg", "a/p.jpg", "shrink", 500, 700, false, false},
		{"/images/a/p.shrink-500x700-q70.png", "a/p.png", "shrink", 500, 700, false, false},
		{"/images/a/p-shrink-1920x0.jpg", "a/p.jpg", "shrink", 1920, 8192, false, true},
		{"/images/a/p-shrink-0x300.jpg", "a/p.jpg", "shrink", 8192, 300, true, false},
		{"/images/a/anim-fit-100x100.GIF", "a/anim.GIF", "fit", 100, 100, false, false},
		{"/images/a/scan.fill-100x100.tif", "a/scan.tif", "fill", 100, 100, false, false},
		{"/images/a/scan-fit-100x100.TIFF", "a/scan.TIFF", "fit", 100, 100, false, false},
		{"/images/a/anim.original.gif", "a/anim.gif", "original", 0, 0, false, false},
		{"/images/a/scan.original.tiff", "a/scan.tiff", "original", 0, 0, false, false},
	}
	for _, test := range tests {
		spec, err := parsePath(test.path, 8192, 40_000_000)
		if err != nil {
			t.Fatalf("%s: %v", test.path, err)
		}
		if spec.sourceRelative != test.source || spec.operation != test.operation || spec.width != test.width || spec.height != test.height ||
			spec.zeroWidth != test.zeroWidth || spec.zeroHeight != test.zeroHeight {
			t.Errorf("%s: unexpected spec %+v", test.path, spec)
		}
	}
	for _, path := range []string{
		"/images/a/p-shrink-0x0.jpg",
		"/images/a/p-shrink-99999x0.jpg",
		"/images/a/p-fill-0x100.jpg",
		"/images/a/p-fit-100x0.jpg",
		"/images/a/p-fitstrict-0x0.jpg",
		"/images/a/p-shrink-8192x8192.jpg", // above the pixel limit
	} {
		if _, err := parsePath(path, 8192, 40_000_000); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	// The pixel limit applies to the substituted dimension of an unbounded side.
	if _, err := parsePath("/images/a/p-shrink-8192x0.jpg", 8192, 40_000_000); err == nil {
		t.Error("shrink 8192x0 exceeds the pixel limit but was accepted")
	}
	if _, err := parsePath("/images/a/p-shrink-1000x0.jpg", 8192, 40_000_000); err != nil {
		t.Errorf("shrink 1000x0 rejected: %v", err)
	}
}

func TestSpecToken(t *testing.T) {
	for path, want := range map[string]string{
		"/images/a/p-fill-260x260-q50.jpg":  "fill-260x260",
		"/images/a/p.fill-260x260.jpg":      "fill-260x260",
		"/images/a/p.shrink-1920x0.jpg":     "shrink-1920x0",
		"/images/a/p-shrink-0x300.jpg":      "shrink-0x300",
		"/images/a/p.original.jpg":          "original",
		"/images/a/p-fitstrict-250x250.jpg": "fitstrict-250x250",
	} {
		spec, err := parsePath(path, 8192, 40_000_000)
		if err != nil {
			t.Fatal(err)
		}
		if got := spec.token(); got != want {
			t.Errorf("%s: token %q, want %q", path, got, want)
		}
	}
}

func TestParseAllowedSizes(t *testing.T) {
	if set, err := parseAllowedSizes(""); err != nil || set != nil {
		t.Fatalf("empty: %v %v", set, err)
	}
	set, err := parseAllowedSizes(" fill-260x260, SHRINK-500x700,shrink-1920x0,shrink-0x0300,original ")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"fill-260x260", "shrink-500x700", "shrink-1920x0", "shrink-0x300", "original"} {
		if !set[token] {
			t.Errorf("missing %q in %v", token, set)
		}
	}
	for _, bad := range []string{"fill", "fill-0x100", "shrink-0x0", "zoom-1x1", "fill-260x260-q80", "fill-260x260,", "original-1x1", "fill-260", "fill-260x260.jpg"} {
		if _, err := parseAllowedSizes(bad); err == nil {
			t.Errorf("accepted %q", bad)
		} else if !strings.Contains(err.Error(), "APP_ALLOWED_SIZES") {
			t.Errorf("%q: unclear error %v", bad, err)
		}
	}
}

func TestParseDefaultQuality(t *testing.T) {
	for value, want := range map[string]int{"": 100, "85": 85, "1": 1, "100": 100} {
		if got, err := parseDefaultQuality(value); err != nil || got != want {
			t.Errorf("%q: got %d, %v", value, got, err)
		}
	}
	for _, bad := range []string{"0", "101", "-5", "abc", "85.5"} {
		if _, err := parseDefaultQuality(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
