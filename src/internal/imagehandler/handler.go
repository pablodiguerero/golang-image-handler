package imagehandler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cacheVersion = "v2"

type transformFunc func(context.Context, string, string, requestSpec, outputFormat) error

type call struct {
	done chan struct{}
	err  error
}

type Handler struct {
	root          string
	cache         string
	accelPrefix   string
	allowAVIF     bool
	allowWebP     bool
	maxDimension  int
	maxPixels     int
	maxSourceSize int64
	timeout       time.Duration
	browserTTL    time.Duration
	cacheTTL      time.Duration
	cacheMaxBytes int64
	cleanupEvery  time.Duration
	semaphore     chan struct{}
	transform     transformFunc
	mu            sync.Mutex
	inflight      map[string]*call
	stopCleanup   context.CancelFunc
}

func NewFromEnv() (*Handler, error) {
	root := os.Getenv("APP_IMAGE_ROOT")
	if root == "" {
		return nil, errors.New("APP_IMAGE_ROOT is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("image root: %w", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, errors.New("APP_IMAGE_ROOT must be a directory")
	}
	cache := os.Getenv("APP_IMAGE_CACHE")
	if cache == "" {
		cache = filepath.Join(os.TempDir(), "image-handler-cache")
	}
	cache, err = filepath.Abs(cache)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cache, 0755); err != nil {
		return nil, fmt.Errorf("image cache: %w", err)
	}
	cache, err = filepath.EvalSymlinks(cache)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("vipsthumbnail"); err != nil {
		return nil, fmt.Errorf("vipsthumbnail is required: %w", err)
	}
	if _, err := exec.LookPath("vips"); err != nil {
		return nil, fmt.Errorf("vips is required: %w", err)
	}
	maxDimension := positiveEnvInt("APP_MAX_DIMENSION", 8192)
	maxPixels := positiveEnvInt("APP_MAX_OUTPUT_PIXELS", 40_000_000)
	maxSourceSize := int64(positiveEnvInt("APP_MAX_SOURCE_BYTES", 50*1024*1024))
	concurrency := positiveEnvInt("APP_MAX_CONCURRENT_TRANSFORMS", 2)
	seconds := positiveEnvInt("APP_TRANSFORM_TIMEOUT_SECONDS", 60)
	browserTTL := positiveEnvDuration("APP_BROWSER_CACHE_TTL_SECONDS", time.Hour)
	cacheTTL := positiveEnvDuration("APP_CACHE_TTL_SECONDS", 7*24*time.Hour)
	cleanupEvery := positiveEnvDuration("APP_CACHE_CLEANUP_INTERVAL_SECONDS", 24*time.Hour)
	cacheMaxBytes := int64(positiveEnvInt("APP_CACHE_MAX_BYTES", 10*1024*1024*1024))
	accelPrefix := strings.TrimSuffix(os.Getenv("APP_ACCEL_REDIRECT_PREFIX"), "/")
	if accelPrefix != "" && (!strings.HasPrefix(accelPrefix, "/") || strings.Contains(accelPrefix, "..")) {
		return nil, errors.New("APP_ACCEL_REDIRECT_PREFIX must be an absolute URL path")
	}
	h := &Handler{
		root:          root,
		cache:         cache,
		accelPrefix:   accelPrefix,
		allowAVIF:     os.Getenv("APP_ALLOW_AVIF") != "0",
		allowWebP:     os.Getenv("APP_ALLOW_WEBP") != "0",
		maxDimension:  maxDimension,
		maxPixels:     maxPixels,
		maxSourceSize: maxSourceSize,
		timeout:       time.Duration(seconds) * time.Second,
		browserTTL:    browserTTL,
		cacheTTL:      cacheTTL,
		cacheMaxBytes: cacheMaxBytes,
		cleanupEvery:  cleanupEvery,
		semaphore:     make(chan struct{}, concurrency),
		transform:     transformImage,
		inflight:      make(map[string]*call),
	}
	cleanupCtx, cancel := context.WithCancel(context.Background())
	h.stopCleanup = cancel
	go h.runCleanup(cleanupCtx)
	return h, nil
}

func positiveEnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func positiveEnvDuration(name string, fallback time.Duration) time.Duration {
	seconds := positiveEnvInt(name, 0)
	if seconds == 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func (h *Handler) Close() {
	if h.stopCleanup != nil {
		h.stopCleanup()
	}
}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rootInfo, rootErr := os.Stat(h.root)
	cacheInfo, cacheErr := os.Stat(h.cache)
	healthy := rootErr == nil && rootInfo.IsDir() && cacheErr == nil && cacheInfo.IsDir()
	w.Header().Set("Content-Type", "application/json")
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Status  string `json:"status"`
		Healthy bool   `json:"healthy"`
	}{Status: map[bool]string{true: "OK", false: "ERROR"}[healthy], Healthy: healthy})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	spec, err := parsePath(r.URL.Path, h.maxDimension, h.maxPixels)
	if err != nil {
		http.Error(w, "invalid image path", http.StatusBadRequest)
		return
	}
	source, info, err := h.source(spec.sourceRelative)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
		} else {
			log.Printf("source image: %v", err)
			http.Error(w, "source image unavailable", http.StatusForbidden)
		}
		return
	}
	if info.Size() > h.maxSourceSize {
		http.Error(w, "source image too large", http.StatusRequestEntityTooLarge)
		return
	}
	format := negotiateFormat(r.Header.Get("Accept"), h.allowAVIF, h.allowWebP)
	key := cacheKey(spec, format, info)
	cachePath := filepath.Join(h.cache, cacheVersion, key[:2], key+"."+string(format))
	if !validCacheFile(cachePath) {
		if err := h.ensureCached(r.Context(), key, cachePath, source, spec, format); err != nil {
			log.Printf("transform %s: %v", spec.sourceRelative, err)
			http.Error(w, "image processing failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", format.contentType())
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(h.browserTTL.Seconds())))
	w.Header().Set("Vary", "Accept")
	etag := `"` + key + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	if h.accelPrefix != "" {
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("X-Accel-Redirect", h.accelPrefix+"/"+cacheVersion+"/"+key[:2]+"/"+key+"."+string(format))
		w.WriteHeader(http.StatusOK)
		return
	}
	file, err := os.Open(cachePath)
	if err != nil {
		log.Printf("open cache: %v", err)
		http.Error(w, "cached image unavailable", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	http.ServeContent(w, r, filepath.Base(cachePath), info.ModTime(), file)
}

func (h *Handler) source(relative string) (string, os.FileInfo, error) {
	candidate := filepath.Join(h.root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(h.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, errors.New("source image escapes APP_IMAGE_ROOT")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("source image is not a regular file")
	}
	return resolved, info, nil
}

func cacheKey(spec requestSpec, format outputFormat, info os.FileInfo) string {
	data := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%s", cacheVersion, spec, format, info.Size(), info.ModTime().UnixNano(), sourceIdentity(info))
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// sourceIdentity uses only fields that reflect a source change. In particular,
// it deliberately excludes atime: libvips reads may update atime and must not
// turn cache hits into fresh transformations.
func sourceIdentity(info os.FileInfo) string {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return ""
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, name := range []string{"Dev", "Ino", "Ctim", "Ctimespec"} {
		field := value.FieldByName(name)
		if field.IsValid() && field.CanInterface() {
			parts = append(parts, name+"="+fmt.Sprint(field.Interface()))
		}
	}
	return strings.Join(parts, ",")
}

func validCacheFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (h *Handler) ensureCached(ctx context.Context, key, target, source string, spec requestSpec, format outputFormat) error {
	h.mu.Lock()
	if existing := h.inflight[key]; existing != nil {
		h.mu.Unlock()
		select {
		case <-existing.done:
			return existing.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	h.inflight[key] = c
	h.mu.Unlock()

	// Cache population belongs to the service, rather than to the first client.
	// A disconnected leader must not abort requests already waiting on this key.
	c.err = h.buildCached(context.Background(), target, source, spec, format)
	h.mu.Lock()
	delete(h.inflight, key)
	close(c.done)
	h.mu.Unlock()
	return c.err
}

func (h *Handler) buildCached(ctx context.Context, target, source string, spec requestSpec, format outputFormat) error {
	select {
	case h.semaphore <- struct{}{}:
		defer func() { <-h.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if validCacheFile(target) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp(filepath.Dir(target), ".image-work-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	output := filepath.Join(workDir, "image."+string(format))
	transformCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	if err := h.transform(transformCtx, source, output, spec, format); err != nil {
		return err
	}
	if !validCacheFile(output) {
		return errors.New("transformer produced an empty or missing image")
	}
	return os.Rename(output, target)
}

func matchesETag(header, etag string) bool {
	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "*" || item == etag || strings.TrimPrefix(item, "W/") == etag {
			return true
		}
	}
	return false
}
