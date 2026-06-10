package calltree

import (
	"sort"
	"strings"
)

// FunctionStat aggregates the occurrences of a function across a call tree.
type FunctionStat struct {
	Calls      int     `json:"calls"`
	DurationMs float64 `json:"duration_ms"`
}

// DiffEntry describes one function whose presence or call count differs
// between two traces.
type DiffEntry struct {
	Function  string  `json:"function"`
	CallsA    int     `json:"calls_a"`
	CallsB    int     `json:"calls_b"`
	DurationA float64 `json:"duration_ms_a"`
	DurationB float64 `json:"duration_ms_b"`
}

// DiffResult is the comparison of two traces at the function level.
type DiffResult struct {
	TraceA    string      `json:"trace_a"`
	TraceB    string      `json:"trace_b"`
	OnlyInA   []DiffEntry `json:"only_in_a"`
	OnlyInB   []DiffEntry `json:"only_in_b"`
	Changed   []DiffEntry `json:"call_count_changed"`
	Identical int         `json:"identical_functions"`
}

// FlattenStats walks a call tree and aggregates per-function call counts and
// durations. Collapsed summary nodes ("[N trivial calls collapsed]") are skipped.
func FlattenStats(root *CallNode) map[string]FunctionStat {
	stats := map[string]FunctionStat{}
	var walk func(n *CallNode, repeatFactor int)
	walk = func(n *CallNode, repeatFactor int) {
		if n == nil {
			return
		}
		factor := repeatFactor
		if n.RepeatCount > 1 {
			factor *= n.RepeatCount
		}
		if !strings.HasPrefix(n.FunctionName, "[") {
			calls := n.CallCount
			if calls == 0 {
				calls = 1
			}
			s := stats[n.FunctionName]
			s.Calls += calls * factor
			s.DurationMs += n.DurationMs
			stats[n.FunctionName] = s
		}
		for _, c := range n.Children {
			walk(c, factor)
		}
	}
	walk(root, 1)
	return stats
}

// Diff compares two traces function by function.
func Diff(a, b *TraceResult) *DiffResult {
	statsA := FlattenStats(a.CallTree)
	statsB := FlattenStats(b.CallTree)

	result := &DiffResult{
		TraceA: a.TraceFile,
		TraceB: b.TraceFile,
	}

	for fn, sa := range statsA {
		sb, inB := statsB[fn]
		switch {
		case !inB:
			result.OnlyInA = append(result.OnlyInA, DiffEntry{
				Function: fn, CallsA: sa.Calls, DurationA: sa.DurationMs,
			})
		case sa.Calls != sb.Calls:
			result.Changed = append(result.Changed, DiffEntry{
				Function: fn,
				CallsA:   sa.Calls, CallsB: sb.Calls,
				DurationA: sa.DurationMs, DurationB: sb.DurationMs,
			})
		default:
			result.Identical++
		}
	}
	for fn, sb := range statsB {
		if _, inA := statsA[fn]; !inA {
			result.OnlyInB = append(result.OnlyInB, DiffEntry{
				Function: fn, CallsB: sb.Calls, DurationB: sb.DurationMs,
			})
		}
	}

	byName := func(entries []DiffEntry) func(i, j int) bool {
		return func(i, j int) bool { return entries[i].Function < entries[j].Function }
	}
	sort.Slice(result.OnlyInA, byName(result.OnlyInA))
	sort.Slice(result.OnlyInB, byName(result.OnlyInB))
	sort.Slice(result.Changed, byName(result.Changed))

	return result
}
