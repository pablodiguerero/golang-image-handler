package imagehandler

import "testing"

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
