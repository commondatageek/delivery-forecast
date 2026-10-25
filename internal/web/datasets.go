package web

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/issues"
)

type datasetKey struct {
	path    string
	size    int64
	mod     time.Time
	walSize int64 // SQLite only: path+"-wal", zero if absent
	walMod  time.Time
}

type dataset struct {
	key    datasetKey
	issues []issues.Issue // shared read-only
	file   app.DataFile   // source.Fingerprint at load time
}

// datasetCache keeps recently loaded issue sets, keyed by path and the
// file's size and modification time (D8).
type datasetCache struct {
	mu      sync.Mutex
	max     int
	entries []*dataset // least recently used first
}

func newDatasetCache(max int) *datasetCache { return &datasetCache{max: max} }

func keyFor(absPath string) (datasetKey, error) {
	fi, err := os.Stat(absPath)
	if err != nil {
		return datasetKey{}, err
	}
	k := datasetKey{path: absPath, size: fi.Size(), mod: fi.ModTime()}
	if wi, err := os.Stat(absPath + "-wal"); err == nil {
		k.walSize, k.walMod = wi.Size(), wi.ModTime()
	}
	return k, nil
}

// get returns the cached dataset for absPath, loading it on a miss or when
// the file changed. Loads are serialized.
func (c *datasetCache) get(ctx context.Context, absPath string) (*dataset, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key, err := keyFor(absPath)
	if err != nil {
		return nil, err
	}
	for i, d := range c.entries {
		if d.key.path != absPath {
			continue
		}
		if d.key == key {
			c.entries = append(append(c.entries[:i:i], c.entries[i+1:]...), d)
			return d, nil
		}
		c.entries = append(c.entries[:i:i], c.entries[i+1:]...)
		break
	}

	all, err := source.Load(ctx, absPath, "", strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	d := &dataset{key: key, issues: all, file: source.Fingerprint(absPath)}
	c.entries = append(c.entries, d)
	if len(c.entries) > c.max {
		c.entries = c.entries[1:]
	}
	return d, nil
}

// invalidate drops absPath's entry (linear sync calls this after writing).
func (c *datasetCache) invalidate(absPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, d := range c.entries {
		if d.key.path == absPath {
			c.entries = append(c.entries[:i:i], c.entries[i+1:]...)
			return
		}
	}
}
