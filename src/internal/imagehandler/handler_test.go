package imagehandler

import (
	"context"
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
		maxDimension: 8192, maxPixels: 40_000_000, maxSourceSize: 50 << 20,
		timeout: time.Minute, semaphore: make(chan struct{}, 2), inflight: make(map[string]*call),
	}, source
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
	if cacheKey(spec, formatJPEG, first) == cacheKey(spec, formatJPEG, second) {
		t.Fatal("same-size source update with restored mtime did not change cache key")
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
