package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drkilla/legacy-map/internal/calltree"
	"github.com/drkilla/legacy-map/internal/filter"
	"github.com/drkilla/legacy-map/internal/parser"
	"github.com/fsnotify/fsnotify"
)

// Config holds watcher configuration.
type Config struct {
	Dir        string
	BufferSize int
	Filter     *filter.Config
	PathPrefix string
}

// Watcher watches a directory for new .xt files and processes them.
type Watcher struct {
	cfg          Config
	store        *Store
	done         chan struct{}
	gzWarnedOnce bool
}

// New creates a new Watcher.
func New(cfg Config) *Watcher {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 20
	}
	if cfg.Filter == nil {
		cfg.Filter = filter.NewDefaultConfig()
	}
	return &Watcher{
		cfg:   cfg,
		store: NewStore(cfg.BufferSize),
		done:  make(chan struct{}),
	}
}

// Store returns the underlying trace store (for MCP server to read from).
func (w *Watcher) Store() *Store {
	return w.store
}

// Run starts watching the directory. It blocks until Stop is called or an error occurs.
func (w *Watcher) Run() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer fsw.Close()

	if err := fsw.Add(w.cfg.Dir); err != nil {
		return fmt.Errorf("watch dir %s: %w", w.cfg.Dir, err)
	}

	log.Printf("👀 Watching %s for .xt files (buffer: %d)", w.cfg.Dir, w.cfg.BufferSize)

	// Process any existing .xt files on startup
	w.processExisting()

	for {
		select {
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Create) {
				if strings.HasSuffix(event.Name, ".xt.gz") {
					if !w.gzWarnedOnce {
						log.Printf("⚠ Compressed trace detected: %s — Set xdebug.use_compression=0 in your PHP config and restart", filepath.Base(event.Name))
						w.gzWarnedOnce = true
					}
				} else if strings.HasSuffix(event.Name, ".xt") {
					w.handleNewFile(event.Name)
				}
			}
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			log.Printf("⚠ Watch error: %v", err)
		case <-w.done:
			return nil
		}
	}
}

// Stop signals the watcher to stop.
func (w *Watcher) Stop() {
	close(w.done)
}

// processExisting processes .xt files already present in the directory.
func (w *Watcher) processExisting() {
	matches, err := filepath.Glob(filepath.Join(w.cfg.Dir, "*.xt"))
	if err != nil {
		return
	}
	for _, path := range matches {
		w.processFile(path)
	}
}

// handleNewFile processes a newly created file in a goroutine.
func (w *Watcher) handleNewFile(path string) {
	go w.handleFile(path)
}

// handleFile waits for the file to be complete, then processes it.
func (w *Watcher) handleFile(path string) {
	if !w.waitForComplete(path, 10*time.Second) {
		log.Printf("⚠ Timeout waiting for %s to complete", filepath.Base(path))
		return
	}
	w.processFile(path)
}

// waitForComplete waits until the file contains "TRACE END" or its size
// stabilizes for 500ms, up to a maximum timeout.
func (w *Watcher) waitForComplete(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	var lastSize int64 = -1
	stableAt := time.Time{}

	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		currentSize := info.Size()

		// Check for TRACE END in the last bytes
		if currentSize > 50 {
			f, err := os.Open(path)
			if err == nil {
				buf := make([]byte, 50)
				n, _ := f.ReadAt(buf, currentSize-50)
				f.Close()
				if n > 0 && strings.Contains(string(buf[:n]), "TRACE END") {
					return true
				}
			}
		}

		// Check size stability
		if currentSize == lastSize {
			if stableAt.IsZero() {
				stableAt = time.Now()
			} else if time.Since(stableAt) > 500*time.Millisecond {
				return true
			}
		} else {
			lastSize = currentSize
			stableAt = time.Time{}
		}

		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// processFile parses a trace file and adds the result to the store.
func (w *Watcher) processFile(path string) {
	start := time.Now()

	entries, err := parser.ParseFile(path)
	if err != nil {
		log.Printf("✗ Parse error %s: %v", filepath.Base(path), err)
		return
	}

	result := calltree.Build(entries, w.cfg.Filter, w.cfg.PathPrefix)
	result.TraceFile = path
	result.Timestamp = time.Now().Format(time.RFC3339)
	result.HTTPMethod, result.URI = DetectURIFromFilename(path)

	w.store.Add(result)

	dur := time.Since(start)
	method := result.HTTPMethod
	if method == "" {
		method = "???"
	}
	uri := result.URI
	if uri == "" {
		uri = filepath.Base(path)
	}

	log.Printf("✓ Trace parsed: %s %s — %d app calls (filtered from %d) in %s",
		method, uri, result.FilteredCalls, result.TotalCalls, dur.Round(time.Millisecond))
}

// DetectURIFromFilename extracts URI from XDebug trace filename.
func DetectURIFromFilename(path string) (method, uri string) {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".xt")

	parts := strings.SplitN(base, ".", 3)
	if len(parts) < 3 {
		return "", ""
	}

	uriPart := parts[2]
	if idx := strings.Index(uriPart, "_XDEBUG_TRACE"); idx != -1 {
		uriPart = uriPart[:idx]
	}
	uri = strings.ReplaceAll(uriPart, "_", "/")
	return "", uri
}
