package imagehandler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
type sourcePixelsFunc func(context.Context, string) (int64, error)

var (
	errSourcePixels = errors.New("source image exceeds pixel limit")
	errSourceBytes  = errors.New("source image exceeds size limit")
	errOutputBytes  = errors.New("generated image exceeds size limit")
)

type call struct {
	done chan struct{}
	err  error
}

type Handler struct {
	root            string
	cache           string
	accelPrefix     string
	allowAVIF       bool
	allowWebP       bool
	maxDimension    int
	maxPixels       int
	maxSourceSize   int64
	maxSourcePixels int64
	maxOutputBytes  int64
	timeout         time.Duration
	browserTTL      time.Duration
	cacheTTL        time.Duration
	cacheMaxBytes   int64
	cleanupEvery    time.Duration
	semaphore       chan struct{}
	transform       transformFunc
	sourcePixels    sourcePixelsFunc
	mu              sync.Mutex
	inflight        map[string]*call
	stopCleanup     context.CancelFunc
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
	maxSourcePixels := int64(positiveEnvInt("APP_MAX_SOURCE_PIXELS", maxPixels))
	maxOutputBytes := int64(positiveEnvInt("APP_MAX_OUTPUT_BYTES", 50*1024*1024))
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
		root:            root,
		cache:           cache,
		accelPrefix:     accelPrefix,
		allowAVIF:       os.Getenv("APP_ALLOW_AVIF") != "0",
		allowWebP:       os.Getenv("APP_ALLOW_WEBP") != "0",
		maxDimension:    maxDimension,
		maxPixels:       maxPixels,
		maxSourceSize:   maxSourceSize,
		maxSourcePixels: maxSourcePixels,
		maxOutputBytes:  maxOutputBytes,
		timeout:         time.Duration(seconds) * time.Second,
		browserTTL:      browserTTL,
		cacheTTL:        cacheTTL,
		cacheMaxBytes:   cacheMaxBytes,
		cleanupEvery:    cleanupEvery,
		semaphore:       make(chan struct{}, concurrency),
		transform:       transformImage,
		sourcePixels:    vipsImagePixels,
		inflight:        make(map[string]*call),
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
	key := cacheKey(spec, format, info, h.maxSourcePixels, h.maxOutputBytes)
	cachePath := filepath.Join(h.cache, cacheVersion, key[:2], key+"."+string(format))
	if !validCacheFile(cachePath) {
		if err := h.ensureCached(r.Context(), key, cachePath, source, spec, format); err != nil {
			log.Printf("transform %s: %v", spec.sourceRelative, err)
			if errors.Is(err, errSourcePixels) || errors.Is(err, errSourceBytes) || errors.Is(err, errOutputBytes) {
				http.Error(w, "source image exceeds limit", http.StatusRequestEntityTooLarge)
				return
			}
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

func cacheKey(spec requestSpec, format outputFormat, info os.FileInfo, maxSourcePixels, maxOutputBytes int64) string {
	pixelLimit := int64(0)
	if spec.operation == "original" {
		// Changing this limit must not keep serving a variant accepted under a
		// previous, larger value.
		pixelLimit = maxSourcePixels
	}
	// A lower output limit must not keep serving a variant created under a
	// previous, larger value.
	data := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%d\x00%d", cacheVersion, spec, format, info.Size(), info.ModTime().UnixNano(), sourceIdentity(info), pixelLimit, maxOutputBytes)
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
	transformSource := source
	if spec.operation == "original" {
		// Both vipsheader and vips copy must inspect the same bytes. A private
		// copy prevents a media update from replacing the pathname between those
		// processes and bypassing the pixel limit.
		transformSource, err = copySource(transformCtx, h.root, workDir, source, h.maxSourceSize)
		if err != nil {
			return err
		}
		pixels, err := h.sourcePixels(transformCtx, transformSource)
		if err != nil {
			return fmt.Errorf("read source dimensions: %w", err)
		}
		if pixels > h.maxSourcePixels {
			return errSourcePixels
		}
	}
	if err := h.transform(transformCtx, transformSource, output, spec, format); err != nil {
		return err
	}
	info, err := os.Stat(output)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("transformer produced an empty or missing image")
	}
	if info.Size() > h.maxOutputBytes {
		return errOutputBytes
	}
	return os.Rename(output, target)
}

func copySource(ctx context.Context, root, workDir, source string, maxBytes int64) (string, error) {
	input, err := openSourceWithinRoot(root, source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return "", errSourceBytes
	}
	target := filepath.Join(workDir, "source")
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer output.Close()
	buffer := make([]byte, 32*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		count, readErr := input.Read(buffer)
		if count > 0 {
			written += int64(count)
			if written > maxBytes {
				return "", errSourceBytes
			}
			if _, err := output.Write(buffer[:count]); err != nil {
				return "", err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return target, nil
}

func vipsImagePixels(ctx context.Context, source string) (int64, error) {
	value := func(field string) (int64, error) {
		output, err := exec.CommandContext(ctx, "vipsheader", "-f", field, source).Output()
		if err != nil {
			return 0, fmt.Errorf("vipsheader %s: %w", field, err)
		}
		result, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if err != nil || result < 1 {
			return 0, errors.New("invalid source dimensions")
		}
		return result, nil
	}
	width, err := value("width")
	if err != nil {
		return 0, err
	}
	height, err := value("height")
	if err != nil {
		return 0, err
	}
	if width > (1<<63-1)/height {
		return 0, errSourcePixels
	}
	return width * height, nil
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
