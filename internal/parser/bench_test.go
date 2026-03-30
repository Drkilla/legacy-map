package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const realTraceDir = "/home/drkilla/projects/ezyformalite/xdebug-traces"

func TestParseRealTraces(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(realTraceDir, "*.xt"))
	if err != nil || len(files) == 0 {
		t.Skipf("no .xt files found in %s", realTraceDir)
	}

	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}

			var entryCount, exitCount, returnCount int
			err = ParseStream(mustOpen(t, path), func(e TraceEntry) error {
				switch {
				case e.IsEntry:
					entryCount++
				case e.IsExit:
					exitCount++
				case e.IsReturn:
					returnCount++
				}
				return nil
			})
			if err != nil {
				t.Fatalf("ParseStream failed: %v", err)
			}

			total := entryCount + exitCount + returnCount
			fmt.Printf("  %s (%.1f MB)\n", name, float64(info.Size())/(1024*1024))
			fmt.Printf("    Entry: %d  Exit: %d  Return: %d  Total: %d\n", entryCount, exitCount, returnCount, total)

			if entryCount == 0 {
				t.Error("expected at least some entry lines")
			}
			if entryCount != exitCount {
				t.Logf("warning: entry count (%d) != exit count (%d) — trace may be truncated", entryCount, exitCount)
			}
		})
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
