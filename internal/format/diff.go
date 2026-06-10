package format

import (
	"fmt"
	"strings"

	"github.com/drkilla/legacy-map/internal/calltree"
)

// DiffText renders a trace comparison as readable terminal output.
func DiffText(d *calltree.DiffResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "A: %s\n", d.TraceA)
	fmt.Fprintf(&b, "B: %s\n\n", d.TraceB)

	if len(d.OnlyInA) == 0 && len(d.OnlyInB) == 0 && len(d.Changed) == 0 {
		fmt.Fprintf(&b, "No differences — %d functions identical in both traces.\n", d.Identical)
		return b.String()
	}

	if len(d.OnlyInA) > 0 {
		fmt.Fprintf(&b, "− Only in A (%d):\n", len(d.OnlyInA))
		for _, e := range d.OnlyInA {
			fmt.Fprintf(&b, "    %d× %s%s\n", e.CallsA, e.Function, durationSuffix(e.DurationA))
		}
		b.WriteString("\n")
	}

	if len(d.OnlyInB) > 0 {
		fmt.Fprintf(&b, "+ Only in B (%d):\n", len(d.OnlyInB))
		for _, e := range d.OnlyInB {
			fmt.Fprintf(&b, "    %d× %s%s\n", e.CallsB, e.Function, durationSuffix(e.DurationB))
		}
		b.WriteString("\n")
	}

	if len(d.Changed) > 0 {
		fmt.Fprintf(&b, "Δ Call count changed (%d):\n", len(d.Changed))
		for _, e := range d.Changed {
			fmt.Fprintf(&b, "    %s: %d× → %d×%s\n",
				e.Function, e.CallsA, e.CallsB, durationDelta(e.DurationA, e.DurationB))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "= %d functions identical\n", d.Identical)
	return b.String()
}

func durationSuffix(ms float64) string {
	if ms <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%.1f ms)", ms)
}

func durationDelta(a, b float64) string {
	if a <= 0 && b <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%.1f ms → %.1f ms)", a, b)
}
