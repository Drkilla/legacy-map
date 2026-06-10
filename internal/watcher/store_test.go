package watcher

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/drkilla/legacy-map/internal/calltree"
)

func mkTrace(uri string) *calltree.TraceResult {
	return &calltree.TraceResult{URI: uri}
}

func TestStore_AddAndLast(t *testing.T) {
	s := NewStore(3)

	if got := s.Last(1); got != nil {
		t.Fatalf("expected nil from empty store, got %v", got)
	}

	s.Add(mkTrace("/a"))
	s.Add(mkTrace("/b"))

	last := s.Last(1)
	if len(last) != 1 || last[0].URI != "/b" {
		t.Fatalf("expected newest /b, got %v", last)
	}

	both := s.Last(2)
	if len(both) != 2 || both[0].URI != "/b" || both[1].URI != "/a" {
		t.Fatalf("expected [/b /a], got %v", both)
	}

	// Asking for more than available returns only what exists
	if got := s.Last(10); len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
}

func TestStore_RingWrapAround(t *testing.T) {
	s := NewStore(3)
	for i := 1; i <= 5; i++ {
		s.Add(mkTrace(fmt.Sprintf("/t%d", i)))
	}

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 traces after wrap, got %d", len(all))
	}
	// Newest first: /t5, /t4, /t3
	want := []string{"/t5", "/t4", "/t3"}
	for i, w := range want {
		if all[i].URI != w {
			t.Errorf("all[%d] = %s, want %s", i, all[i].URI, w)
		}
	}

	if s.Count() != 5 {
		t.Errorf("expected total count 5, got %d", s.Count())
	}
}

func TestStore_WaitForNew(t *testing.T) {
	s := NewStore(3)
	s.Add(mkTrace("/old"))
	before := s.Count()

	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Add(mkTrace("/new"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	trace, ok := s.WaitForNew(ctx, before)
	if !ok {
		t.Fatal("expected new trace to be detected")
	}
	if trace.URI != "/new" {
		t.Errorf("expected /new, got %s", trace.URI)
	}
}

func TestStore_WaitForNew_Timeout(t *testing.T) {
	s := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, ok := s.WaitForNew(ctx, s.Count())
	if ok {
		t.Fatal("expected timeout, got a trace")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore(10)
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Add(mkTrace(fmt.Sprintf("/w%d-%d", n, j)))
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Last(5)
				s.All()
				s.Count()
			}
		}()
	}
	wg.Wait()

	if s.Count() != 800 {
		t.Errorf("expected 800 writes, got %d", s.Count())
	}
	if len(s.All()) != 10 {
		t.Errorf("expected buffer full at 10, got %d", len(s.All()))
	}
}
