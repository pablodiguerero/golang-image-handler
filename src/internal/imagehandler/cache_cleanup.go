package imagehandler

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type cacheFile struct {
	path    string
	size    int64
	modTime time.Time
}

func (h *Handler) runCleanup(ctx context.Context) {
	h.cleanup(time.Now())
	ticker := time.NewTicker(h.cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			h.cleanup(now)
		}
	}
}

// cleanup visits each two-character cache shard separately. It removes expired
// entries while streaming the directory and only collects one shard when it
// needs size eviction, avoiding a process-wide list of cached files.
func (h *Handler) cleanup(now time.Time) {
	root := filepath.Join(h.cache, cacheVersion)
	shards, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var total int64
	shardPaths := make([]string, 0, len(shards))
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		shardPath := filepath.Join(root, shard.Name())
		shardPaths = append(shardPaths, shardPath)
		total += h.cleanupExpired(shardPath, now)
	}
	if total <= h.cacheMaxBytes || len(shardPaths) == 0 {
		return
	}
	// Cache keys are SHA-256 hashes and uniformly distributed over shards. A
	// per-shard quota bounds memory use of the cleanup process and total disk
	// usage without collecting metadata for every generated image at once.
	quota := h.cacheMaxBytes / int64(len(shardPaths))
	for _, shardPath := range shardPaths {
		h.trimShard(shardPath, quota)
	}
}

func (h *Handler) cleanupExpired(root string, now time.Time) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if now.Sub(info.ModTime()) > h.cacheTTL {
			_ = os.Remove(path)
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func (h *Handler) trimShard(root string, quota int64) {
	files := make([]cacheFile, 0)
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, cacheFile{path: path, size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if total <= quota {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, file := range files {
		if total <= quota {
			return
		}
		if os.Remove(file.path) == nil {
			total -= file.size
		}
	}
}
