package filter

import (
	"fmt"
"path/filepath"
	"sort"
	"testing"

	"github.com/drkilla/legacy-map/internal/parser"
)

const realTraceDir = "/home/drkilla/projects/ezyformalite/xdebug-traces"

func TestFilterRealTraces(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(realTraceDir, "*.xt"))
	if err != nil || len(files) == 0 {
		t.Skipf("no .xt files found in %s", realTraceDir)
	}

	cfg := NewDefaultConfig()

	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			entries, err := parser.ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}

			total, filtered := cfg.CountFiltered(entries)
			pct := float64(total-filtered) / float64(total) * 100

			fmt.Printf("  %s\n", name)
			fmt.Printf("    Total entry calls:    %d\n", total)
			fmt.Printf("    After filter (L1+L2): %d\n", filtered)
			fmt.Printf("    Removed:              %d (%.1f%%)\n", total-filtered, pct)

			// Show top 10 app-code functions by frequency
			freq := map[string]int{}
			for _, e := range entries {
				if e.IsEntry && cfg.ShouldKeep(e) {
					freq[e.FunctionName]++
				}
			}
			type kv struct {
				Name  string
				Count int
			}
			var sorted []kv
			for k, v := range freq {
				sorted = append(sorted, kv{k, v})
			}
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })
			fmt.Printf("    Top app functions:\n")
			for i, s := range sorted {
				if i >= 10 {
					break
				}
				fmt.Printf("      %4d× %s\n", s.Count, s.Name)
			}

			if filtered == 0 {
				t.Error("expected at least some entries after filtering")
			}
		})
	}
}
