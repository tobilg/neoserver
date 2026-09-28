package pathpolicy

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Lease is a validated datasource path. For an HTTPS source it pins the cached
// download, so the cache quota never evicts a file that a live datasource may
// reopen by path. Release unpins it; it is idempotent and safe on nil.
type Lease struct {
	Path string
	// Cached reports that Path is a download inside the shared remote cache.
	Cached bool
	once   sync.Once
}

// Release returns the lease. A cached download becomes eligible for eviction
// once no other lease pins it.
func (l *Lease) Release() {
	if l == nil || !l.Cached {
		return
	}
	l.once.Do(func() { unpin(l.Path) })
}

var remoteCache = struct {
	sync.Mutex
	pins map[string]int
}{pins: make(map[string]int)}

func pin(path string) {
	remoteCache.Lock()
	remoteCache.pins[path]++
	remoteCache.Unlock()
}

func unpin(path string) {
	remoteCache.Lock()
	if remoteCache.pins[path] <= 1 {
		delete(remoteCache.pins, path)
	} else {
		remoteCache.pins[path]--
	}
	remoteCache.Unlock()
}

type cachedDownload struct {
	path     string
	size     int64
	lastUsed time.Time
}

// enforceRemoteCacheQuota evicts unpinned downloads: first those unused for
// longer than RemoteCacheMaxAge, then least recently used ones until the cache
// fits RemoteCacheMaxBytes. A download's modification time records its last
// use. Zero disables either limit.
func enforceRemoteCacheQuota(opts Options, now time.Time) {
	if opts.RemoteCacheMaxBytes <= 0 && opts.RemoteCacheMaxAge <= 0 {
		return
	}
	root, err := filepath.Abs(opts.RemoteCachePath)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	remoteCache.Lock()
	defer remoteCache.Unlock()
	var total int64
	var candidates []cachedDownload
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(root, name)
		total += info.Size()
		if remoteCache.pins[path] == 0 {
			candidates = append(candidates, cachedDownload{path: path, size: info.Size(), lastUsed: info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].lastUsed.Before(candidates[j].lastUsed) })
	for _, candidate := range candidates {
		expired := opts.RemoteCacheMaxAge > 0 && now.Sub(candidate.lastUsed) > opts.RemoteCacheMaxAge
		overQuota := opts.RemoteCacheMaxBytes > 0 && total > opts.RemoteCacheMaxBytes
		if !expired && !overQuota {
			continue
		}
		if err := removeDownload(candidate.path); err != nil {
			slog.Warn("evict remote datasource cache entry", "path", candidate.path, "error", err)
			continue
		}
		total -= candidate.size
	}
	if opts.RemoteCacheMaxBytes > 0 && total > opts.RemoteCacheMaxBytes {
		slog.Warn("remote datasource cache exceeds its quota; the remaining downloads are in use",
			"bytes", total, "max_bytes", opts.RemoteCacheMaxBytes)
	}
}

func removeDownload(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Remove(path + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
