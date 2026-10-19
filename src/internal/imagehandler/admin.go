package imagehandler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

const maxAdminRequestBytes = 1 << 20

type adminRequest struct {
	Paths   []string `json:"paths"`
	Formats []string `json:"formats"`
}

type adminVariant struct {
	Format string `json:"format"`
	Key    string `json:"key"`
	Cached bool   `json:"cached"`
	Error  string `json:"error,omitempty"`
}

type adminResult struct {
	Path     string         `json:"path"`
	Source   string         `json:"source,omitempty"`
	Error    string         `json:"error,omitempty"`
	Variants []adminVariant `json:"variants,omitempty"`
}

type adminPlan struct {
	result *adminResult
	spec   requestSpec
	source string
}

func (h *Handler) AdminEnabled() bool { return h.adminToken != "" }

func (h *Handler) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !h.AdminEnabled() || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return false
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == r.Header.Get("Authorization") {
		token = r.Header.Get("X-Image-Handler-Token")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.adminToken)) != 1 {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (h *Handler) decodeAdminRequest(w http.ResponseWriter, r *http.Request) (adminRequest, []outputFormat, bool) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAdminRequestBytes))
	decoder.DisallowUnknownFields()
	var request adminRequest
	if err := decoder.Decode(&request); err != nil || len(request.Paths) == 0 || len(request.Paths) > h.adminMaxPaths {
		http.Error(w, "invalid warm request", http.StatusBadRequest)
		return adminRequest{}, nil, false
	}
	formats, err := h.adminFormats(request.Formats)
	if err != nil {
		http.Error(w, "invalid output formats", http.StatusBadRequest)
		return adminRequest{}, nil, false
	}
	return request, formats, true
}

func (h *Handler) adminFormats(requested []string) ([]outputFormat, error) {
	if len(requested) == 0 {
		requested = []string{"jpg", "webp", "avif"}
	}
	formats := make([]outputFormat, 0, len(requested))
	seen := make(map[outputFormat]bool)
	for _, value := range requested {
		value = strings.ToLower(value)
		if value == "jpeg" {
			value = "jpg"
		}
		format := outputFormat(value)
		if format != formatJPEG && format != formatWebP && format != formatAVIF {
			return nil, errors.New("unsupported format")
		}
		if (format == formatWebP && !h.allowWebP) || (format == formatAVIF && !h.allowAVIF) {
			continue
		}
		if !seen[format] {
			formats = append(formats, format)
			seen[format] = true
		}
	}
	if len(formats) == 0 {
		return nil, errors.New("no enabled formats")
	}
	return formats, nil
}

func (h *Handler) planAdminRequest(request adminRequest, formats []outputFormat) []*adminPlan {
	plans := make([]*adminPlan, 0, len(request.Paths))
	for _, requestPath := range request.Paths {
		result := &adminResult{Path: requestPath}
		spec, err := h.parseRequest(requestPath)
		if err != nil {
			result.Error = "invalid image path"
			plans = append(plans, &adminPlan{result: result})
			continue
		}
		source, info, err := h.source(spec.sourceRelative)
		if err != nil {
			result.Error = "source image unavailable"
			plans = append(plans, &adminPlan{result: result})
			continue
		}
		if info.Size() > h.maxSourceSize {
			result.Error = "source image exceeds limit"
			plans = append(plans, &adminPlan{result: result})
			continue
		}
		result.Source = spec.sourceRelative
		for _, format := range formats {
			key := cacheKey(spec, format, info, h.maxSourcePixels, h.maxOutputBytes)
			cachePath := filepath.Join(h.cache, cacheVersion, key[:2], key+"."+string(format))
			result.Variants = append(result.Variants, adminVariant{
				Format: string(format), Key: key, Cached: validCacheFile(cachePath),
			})
		}
		plans = append(plans, &adminPlan{result: result, spec: spec, source: source})
	}
	return plans
}

func writeAdminResponse(w http.ResponseWriter, results []*adminPlan) {
	w.Header().Set("Content-Type", "application/json")
	response := make([]adminResult, 0, len(results))
	for _, plan := range results {
		response = append(response, *plan.result)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Results []adminResult `json:"results"`
	}{Results: response})
}

// AdminCacheKeys returns variant cache keys and whether they already exist.
func (h *Handler) AdminCacheKeys(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	request, formats, ok := h.decodeAdminRequest(w, r)
	if !ok {
		return
	}
	writeAdminResponse(w, h.planAdminRequest(request, formats))
}

// AdminWarmCache materializes requested variants. The transform semaphore keeps
// this work within the same resource budget as browser traffic.
func (h *Handler) AdminWarmCache(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	request, formats, ok := h.decodeAdminRequest(w, r)
	if !ok {
		return
	}
	plans := h.planAdminRequest(request, formats)
	type task struct {
		plan    *adminPlan
		variant int
	}
	tasks := make(chan task)
	var workers sync.WaitGroup
	workerCount := h.adminWorkers
	if workerCount < 1 {
		workerCount = cap(h.semaphore)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(plans)*len(formats) {
		workerCount = len(plans) * len(formats)
	}
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range tasks {
				variant := &task.plan.result.Variants[task.variant]
				if variant.Cached {
					continue
				}
				cachePath := filepath.Join(h.cache, cacheVersion, variant.Key[:2], variant.Key+"."+variant.Format)
				if err := h.ensureCached(r.Context(), variant.Key, cachePath, task.plan.source, task.plan.spec, outputFormat(variant.Format)); err != nil {
					variant.Error = "warm failed"
					continue
				}
				variant.Cached = true
			}
		}()
	}
	for _, plan := range plans {
		if plan.result.Error != "" {
			continue
		}
		for index := range plan.result.Variants {
			tasks <- task{plan: plan, variant: index}
		}
	}
	close(tasks)
	workers.Wait()
	writeAdminResponse(w, plans)
}
