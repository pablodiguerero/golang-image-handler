package imagehandler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "media")
	cache := filepath.Join(base, "cache")
	if err := os.MkdirAll(filepath.Join(root, "files"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "files", "realty.jpg")
	if err := os.WriteFile(source, []byte("first source"), 0644); err != nil {
		t.Fatal(err)
	}
	return &Handler{
		root: root, cache: cache, allowAVIF: true, allowWebP: true,
		maxDimension: 8192, maxPixels: 40_000_000, maxSourceSize: 50 << 20, maxSourcePixels: 40_000_000, maxOutputBytes: 50 << 20,
		timeout: time.Minute, semaphore: make(chan struct{}, 2), inflight: make(map[string]*call),
		sourcePixels: func(context.Context, string) (int64, error) { return 1, nil },
	}, source
}

func TestAdminCacheKeysAndWarm(t *testing.T) {
	h, _ := testHandler(t)
	h.adminToken = "test-token"
	h.adminMaxPaths = 5
	h.adminWorkers = 2
	var transforms atomic.Int32
	h.transform = func(_ context.Context, _, output string, _ requestSpec, format outputFormat) error {
		transforms.Add(1)
		return os.WriteFile(output, []byte("encoded "+string(format)), 0644)
	}
	body := []byte(`{"paths":["/images/files/realty-fit-100x100.jpg"],"formats":["jpg","webp"]}`)

	unauthorized := httptest.NewRecorder()
	h.AdminCacheKeys(unauthorized, httptest.NewRequest(http.MethodPost, "/admin/cache/keys", bytes.NewReader(body)))
	if unauthorized.Code != http.StatusNotFound {
		t.Fatalf("unauthorized keys status=%d", unauthorized.Code)
	}

	warmRequest := httptest.NewRequest(http.MethodPost, "/admin/cache/warm", bytes.NewReader(body))
	warmRequest.Header.Set("Authorization", "Bearer test-token")
	warm := httptest.NewRecorder()
	h.AdminWarmCache(warm, warmRequest)
	if warm.Code != http.StatusOK || transforms.Load() != 2 {
		t.Fatalf("warm status=%d transforms=%d body=%q", warm.Code, transforms.Load(), warm.Body.String())
	}
	var response struct {
		Results []adminResult `json:"results"`
	}
	if err := json.Unmarshal(warm.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || len(response.Results[0].Variants) != 2 {
		t.Fatalf("unexpected warm response: %+v", response)
	}
	for _, variant := range response.Results[0].Variants {
		if !variant.Cached || variant.Key == "" {
			t.Fatalf("variant was not cached: %+v", variant)
		}
	}
}

func TestHandlerRejectsOversizedOriginal(t *testing.T) {
	h, _ := testHandler(t)
	h.maxSourcePixels = 10
	h.sourcePixels = func(context.Context, string) (int64, error) { return 11, nil }
	h.transform = func(context.Context, string, string, requestSpec, outputFormat) error {
		t.Fatal("oversized original reached the transformer")
		return nil
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images/files/realty.original.jpg", nil))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestHandlerRejectsOversizedOutput(t *testing.T) {
	h, _ := testHandler(t)
	h.maxOutputBytes = 3
	h.transform = func(_ context.Context, _, output string, _ requestSpec, _ outputFormat) error {
		return os.WriteFile(output, []byte("four"), 0644)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images/files/realty-fit-100x100.jpg", nil))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestCopySourceHonoursCancelledContext(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.jpg")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copySource(ctx, root, base, source, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("copySource error = %v, want context cancellation", err)
	}
}

func TestCopySourceRejectsSymlinkOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.jpg")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "source.jpg")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := copySource(context.Background(), root, base, link, 1024); err == nil {
		t.Fatal("copySource accepted a symlink outside image root")
	}
}

func TestHandlerCachesNegotiatedVariantsAndInvalidatesSource(t *testing.T) {
	h, source := testHandler(t)
	var transforms atomic.Int32
	h.transform = func(_ context.Context, _, output string, _ requestSpec, format outputFormat) error {
		transforms.Add(1)
		return os.WriteFile(output, []byte("encoded "+format), 0644)
	}
	request := func(accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/images/files/realty-fit-100x100.jpg", nil)
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	first := request("image/webp")
	if first.Code != 200 || first.Header().Get("Content-Type") != "image/webp" || first.Header().Get("Vary") != "Accept" {
		t.Fatalf("first response: status=%d headers=%v", first.Code, first.Header())
	}
	if got := request("image/webp"); got.Code != 200 || got.Header().Get("ETag") != first.Header().Get("ETag") {
		t.Fatalf("cache hit failed: status=%d headers=%v", got.Code, got.Header())
	}
	if transforms.Load() != 1 {
		t.Fatalf("expected one transform, got %d", transforms.Load())
	}
	if got := request("image/avif,image/webp"); got.Code != 200 || got.Header().Get("Content-Type") != "image/avif" {
		t.Fatalf("AVIF negotiation failed: status=%d headers=%v", got.Code, got.Header())
	}
	if transforms.Load() != 2 {
		t.Fatalf("expected two format variants, got %d", transforms.Load())
	}
	if err := os.WriteFile(source, []byte("second source is larger"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := request("image/webp"); got.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatal("source update did not invalidate cache key")
	}
	if transforms.Load() != 3 {
		t.Fatalf("expected source regeneration, got %d", transforms.Load())
	}
}

func TestHandlerSupportsUnicodeAndOriginalURLs(t *testing.T) {
	h, _ := testHandler(t)
	filename := "Сумка_Wilson_RF_Tournament_9PK_Black______.jpg"
	if err := os.WriteFile(filepath.Join(h.root, "files", filename), []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	var specs []requestSpec
	h.transform = func(_ context.Context, _, output string, spec requestSpec, _ outputFormat) error {
		specs = append(specs, spec)
		return os.WriteFile(output, []byte("encoded"), 0644)
	}
	urls := []string{
		"/images/files/Сумка_Wilson_RF_Tournament_9PK_Black______.fitstrict-150x150.jpg",
		"/images/files/Сумка_Wilson_RF_Tournament_9PK_Black______.original.jpg",
	}
	for _, url := range urls {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%q", url, w.Code, w.Body.String())
		}
	}
	if len(specs) != 2 || specs[0].operation != "fitstrict" || specs[1].operation != "original" {
		t.Fatalf("unexpected transform specs: %+v", specs)
	}
}

func TestCacheKeyChangesForSameSizeAndRestoredMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.jpg")
	if err := os.WriteFile(path, []byte("first source"), 0644); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, first.ModTime(), first.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	spec := requestSpec{sourceRelative: "source.jpg", operation: "fit", width: 100, height: 100, quality: 80}
	if cacheKey(spec, formatJPEG, first, 80_000_000, 50<<20) == cacheKey(spec, formatJPEG, second, 80_000_000, 50<<20) {
		t.Fatal("same-size source update with restored mtime did not change cache key")
	}
}

func TestOriginalCacheKeyIncludesPixelLimit(t *testing.T) {
	info := testFileInfo{sys: testStat{Dev: 1, Ino: 2, Ctim: 3}}
	spec := requestSpec{sourceRelative: "source.jpg", operation: "original", quality: 100}
	if cacheKey(spec, formatJPEG, info, 10, 50<<20) == cacheKey(spec, formatJPEG, info, 20, 50<<20) {
		t.Fatal("original cache key ignores source pixel limit")
	}
}

func TestCacheKeyIncludesOutputLimit(t *testing.T) {
	info := testFileInfo{sys: testStat{Dev: 1, Ino: 2, Ctim: 3}}
	spec := requestSpec{sourceRelative: "source.jpg", operation: "fit", width: 100, height: 100, quality: 100}
	if cacheKey(spec, formatJPEG, info, 40_000_000, 10) == cacheKey(spec, formatJPEG, info, 40_000_000, 20) {
		t.Fatal("cache key ignores output byte limit")
	}
}

func TestSourceIdentityExcludesAccessTime(t *testing.T) {
	first := testFileInfo{sys: testStat{Dev: 1, Ino: 2, Ctim: 3, Atim: 4}}
	accessed := testFileInfo{sys: testStat{Dev: 1, Ino: 2, Ctim: 3, Atim: 99}}
	changed := testFileInfo{sys: testStat{Dev: 1, Ino: 2, Ctim: 5, Atim: 99}}
	if sourceIdentity(first) != sourceIdentity(accessed) {
		t.Fatal("access time changed source identity")
	}
	if sourceIdentity(first) == sourceIdentity(changed) {
		t.Fatal("change time did not change source identity")
	}
}

type testStat struct {
	Dev  int
	Ino  int
	Ctim int
	Atim int
}

type testFileInfo struct {
	sys testStat
}

func (testFileInfo) Name() string       { return "source.jpg" }
func (testFileInfo) Size() int64        { return 0 }
func (testFileInfo) Mode() os.FileMode  { return 0 }
func (testFileInfo) ModTime() time.Time { return time.Time{} }
func (testFileInfo) IsDir() bool        { return false }
func (f testFileInfo) Sys() any         { return f.sys }

func TestHandlerSingleFlightAndConditionalResponse(t *testing.T) {
	h, _ := testHandler(t)
	var transforms atomic.Int32
	h.transform = func(_ context.Context, _, output string, _ requestSpec, _ outputFormat) error {
		transforms.Add(1)
		time.Sleep(20 * time.Millisecond)
		return os.WriteFile(output, []byte("jpeg result"), 0644)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images/files/realty-fill-100x100.jpg", nil))
			if w.Code != 200 {
				t.Errorf("concurrent request: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if transforms.Load() != 1 {
		t.Fatalf("same key transformed %d times", transforms.Load())
	}
	r := httptest.NewRequest(http.MethodGet, "/images/files/realty-fill-100x100.jpg", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	etag := w.Header().Get("ETag")
	r = httptest.NewRequest(http.MethodGet, "/images/files/realty-fill-100x100.jpg", nil)
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("conditional response: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestAccelRedirectAndSymlinkEscape(t *testing.T) {
	h, _ := testHandler(t)
	h.accelPrefix = "/__image_cache"
	h.transform = func(_ context.Context, _, output string, _ requestSpec, _ outputFormat) error {
		return os.WriteFile(output, []byte("cached"), 0644)
	}
	r := httptest.NewRequest(http.MethodGet, "/images/files/realty-fitstrict-100x100.jpg", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("X-Accel-Redirect") == "" {
		t.Fatalf("accel response: status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	outside := filepath.Join(t.TempDir(), "secret.jpg")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(h.root, "files", "secret.jpg")); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images/files/secret-fit-100x100.jpg", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("symlink escape returned %d", w.Code)
	}
}

func TestHealthCheck(t *testing.T) {
	h, _ := testHandler(t)
	w := httptest.NewRecorder()
	h.HealthCheck(w, httptest.NewRequest(http.MethodGet, "/health-check/", nil))
	if w.Code != 200 {
		t.Fatalf("health check returned %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "{\"status\":\"OK\",\"healthy\":true}\n" {
		t.Fatalf("health check body: %q", body)
	}
}

func TestCleanupRemovesExpiredAndOldestFiles(t *testing.T) {
	h, _ := testHandler(t)
	h.cacheTTL = 7 * 24 * time.Hour
	h.cacheMaxBytes = 10
	cacheRoot := filepath.Join(h.cache, cacheVersion, "ab")
	if err := os.MkdirAll(cacheRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	files := []struct {
		name string
		age  time.Duration
		size int
	}{
		{"expired.jpg", 8 * 24 * time.Hour, 4},
		{"old.jpg", 2 * time.Hour, 8},
		{"new.jpg", time.Hour, 8},
	}
	for _, file := range files {
		path := filepath.Join(cacheRoot, file.name)
		if err := os.WriteFile(path, make([]byte, file.size), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-file.age), now.Add(-file.age)); err != nil {
			t.Fatal(err)
		}
	}
	h.cleanup(now)
	for _, name := range []string{"expired.jpg", "old.jpg"} {
		if _, err := os.Stat(filepath.Join(cacheRoot, name)); !os.IsNotExist(err) {
			t.Errorf("%s was not removed", name)
		}
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "new.jpg")); err != nil {
		t.Fatalf("new cache entry was removed: %v", err)
	}
}

func TestCacheKeysKeepWebPAndAVIFAndChangeJPEG(t *testing.T) {
	_, source := testHandler(t)
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	// The formula as it was before the white JPEG background.
	legacy := func(spec requestSpec, format outputFormat) string {
		pixelLimit := int64(0)
		if spec.operation == "original" {
			pixelLimit = 40_000_000
		}
		data := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%d\x00%d", cacheVersion, spec, format, info.Size(), info.ModTime().UnixNano(), sourceIdentity(info), pixelLimit, int64(50<<20))
		sum := sha256.Sum256([]byte(data))
		return hex.EncodeToString(sum[:])
	}
	for _, spec := range []requestSpec{
		{sourceRelative: "files/realty.jpg", operation: "fill", width: 260, height: 260, quality: 100},
		{sourceRelative: "files/realty.jpg", operation: "fit", width: 800, height: 600, quality: 60},
		{sourceRelative: "files/realty.jpg", operation: "fitstrict", width: 250, height: 250, quality: 100},
		{sourceRelative: "files/realty.jpg", operation: "original", quality: 100},
	} {
		for _, format := range []outputFormat{formatWebP, formatAVIF} {
			if got, want := cacheKey(spec, format, info, 40_000_000, 50<<20), legacy(spec, format); got != want {
				t.Errorf("%s %s: key changed: %s != %s", spec, format, got, want)
			}
		}
		if got := cacheKey(spec, formatJPEG, info, 40_000_000, 50<<20); got == legacy(spec, formatJPEG) {
			t.Errorf("%s jpg: key did not change", spec)
		}
	}
}

func TestAllowedSizesHandlerAndAdmin(t *testing.T) {
	h, _ := testHandler(t)
	var err error
	h.allowedSizes, err = parseAllowedSizes("fit-100x100,shrink-500x0,original")
	if err != nil {
		t.Fatal(err)
	}
	h.adminToken = "test-token"
	h.adminMaxPaths = 10
	h.adminWorkers = 1
	var transforms atomic.Int32
	h.transform = func(_ context.Context, _, output string, _ requestSpec, _ outputFormat) error {
		transforms.Add(1)
		return os.WriteFile(output, []byte("encoded"), 0644)
	}
	for path, want := range map[string]int{
		"/images/files/realty-fit-100x100.jpg":      http.StatusOK,
		"/images/files/realty.fit-100x100-q40.jpg":  http.StatusOK, // dot separator and quality
		"/images/files/realty-shrink-500x0.jpg":     http.StatusOK,
		"/images/files/realty.original.jpg":         http.StatusOK,
		"/images/files/realty-fit-101x100.jpg":      http.StatusNotFound,
		"/images/files/realty.fill-100x100.jpg":     http.StatusNotFound,
		"/images/files/realty-shrink-500x600.jpg":   http.StatusNotFound,
		"/images/files/missing-fit-999x999.jpg":     http.StatusNotFound,
		"/images/files/realty-fit-100x100.svg":      http.StatusBadRequest,
		"/images/files/realty-shrink-0x0.jpg":       http.StatusBadRequest,
		"/images/files/../realty-fit-100x100.jpg":   http.StatusBadRequest,
		"/images/files/realty-fit-100x100-q101.jpg": http.StatusBadRequest,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("%s: status=%d want %d", path, w.Code, want)
		}
	}
	before := transforms.Load()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/images/files/realty-fit-101x100.jpg", nil))
	if w.Code != http.StatusNotFound || transforms.Load() != before {
		t.Fatalf("unlisted variant did work: status=%d", w.Code)
	}

	body := []byte(`{"paths":["/images/files/realty-fit-100x100.jpg","/images/files/realty-fit-101x100.jpg"],"formats":["webp"]}`)
	for _, endpoint := range []func(http.ResponseWriter, *http.Request){h.AdminCacheKeys, h.AdminWarmCache} {
		request := httptest.NewRequest(http.MethodPost, "/admin/cache/x", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		endpoint(rec, request)
		var response struct {
			Results []adminResult `json:"results"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Results) != 2 {
			t.Fatalf("bad admin response %q: %v", rec.Body.String(), err)
		}
		if response.Results[0].Error != "" || len(response.Results[0].Variants) != 1 {
			t.Errorf("listed path rejected: %+v", response.Results[0])
		}
		if response.Results[1].Error != "invalid image path" || len(response.Results[1].Variants) != 0 {
			t.Errorf("unlisted path accepted: %+v", response.Results[1])
		}
	}
}

func TestDefaultQualityAppliesWithoutExplicitQ(t *testing.T) {
	h, _ := testHandler(t)
	h.defaultQuality = 85
	for path, want := range map[string]int{
		"/images/files/realty-fit-100x100.jpg":     85,
		"/images/files/realty-fit-100x100-q40.jpg": 40,
		"/images/files/realty.original.jpg":        85,
	} {
		spec, err := h.parseRequest(path)
		if err != nil || spec.quality != want {
			t.Errorf("%s: quality=%d err=%v, want %d", path, spec.quality, err, want)
		}
	}
	h.defaultQuality = 0 // unset in tests: historical 100
	if spec, _ := h.parseRequest("/images/files/realty-fit-100x100.jpg"); spec.quality != 100 {
		t.Errorf("default changed: %d", spec.quality)
	}
	h.defaultQuality = 85
	info, _ := os.Stat(filepath.Join(h.root, "files", "realty.jpg"))
	a, _ := h.parseRequest("/images/files/realty-fit-100x100.jpg")
	b := a
	b.quality = 100
	if cacheKey(a, formatWebP, info, 1, 1) == cacheKey(b, formatWebP, info, 1, 1) {
		t.Error("quality is not part of the cache key")
	}
}
