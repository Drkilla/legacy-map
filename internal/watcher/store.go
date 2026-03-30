package watcher

import (
	"context"
	"sync"
	"time"

	"github.com/drkilla/legacy-map/internal/calltree"
)

// Store is a thread-safe ring buffer of TraceResult.
type Store struct {
	mu    sync.RWMutex
	buf   []*calltree.TraceResult
	size  int
	pos   int // next write position
	count int // total items written (for knowing if buf is full)
}

// NewStore creates a ring buffer store with the given capacity.
func NewStore(size int) *Store {
	return &Store{
		buf:  make([]*calltree.TraceResult, size),
		size: size,
	}
}

// Add inserts a trace result into the ring buffer.
func (s *Store) Add(r *calltree.TraceResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.pos] = r
	s.pos = (s.pos + 1) % s.size
	s.count++
}

// Last returns the N most recent trace results (newest first).
func (s *Store) Last(n int) []*calltree.TraceResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastN(n)
}

// lastN is the lock-free internal implementation of Last.
// Caller must hold at least RLock.
func (s *Store) lastN(n int) []*calltree.TraceResult {
	available := s.count
	if available > s.size {
		available = s.size
	}
	if n > available {
		n = available
	}
	if n <= 0 {
		return nil
	}

	results := make([]*calltree.TraceResult, n)
	for i := 0; i < n; i++ {
		idx := (s.pos - 1 - i + s.size) % s.size
		results[i] = s.buf[idx]
	}
	return results
}

// Count returns the total number of traces ever written.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// WaitForNew blocks until a new trace is added after countBefore, or ctx is cancelled.
func (s *Store) WaitForNew(ctx context.Context, countBefore int) (*calltree.TraceResult, bool) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-ticker.C:
			s.mu.RLock()
			current := s.count
			s.mu.RUnlock()
			if current > countBefore {
				traces := s.Last(1)
				if len(traces) > 0 {
					return traces[0], true
				}
			}
		}
	}
}

// All returns all stored trace results (newest first).
func (s *Store) All() []*calltree.TraceResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	available := s.count
	if available > s.size {
		available = s.size
	}
	return s.lastN(available)
}
