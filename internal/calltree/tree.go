package calltree

import (
	"fmt"
	"strings"

	"github.com/drkilla/legacy-map/internal/filter"
	"github.com/drkilla/legacy-map/internal/parser"
)

// BuildOptions controls post-processing of the call tree.
type BuildOptions struct {
	ReturnsMode string // "truncate" (default), "type", "none"
	Collapse    bool   // collapse trivial leaf calls (default: true)
}

// CallNode represents a single function call in the filtered call tree.
type CallNode struct {
	FunctionName   string      `json:"function"`
	ClassName      string      `json:"class,omitempty"`
	MethodName     string      `json:"method,omitempty"`
	File           string      `json:"file"`
	Line           int         `json:"line"`
	Params         []string    `json:"params,omitempty"`
	ReturnValue    string      `json:"return,omitempty"`
	DurationMs     float64     `json:"duration_ms"`
	CallCount      int         `json:"call_count,omitempty"`      // >1 when repeated calls are merged
	CollapsedCalls []string    `json:"collapsed_calls,omitempty"` // unique method names when trivial calls are collapsed
	Children       []*CallNode `json:"children,omitempty"`
	ExternalCalls  []string    `json:"external_calls,omitempty"`
}

// TraceResult is the top-level output for a parsed and filtered trace.
type TraceResult struct {
	Scenario      string        `json:"scenario,omitempty"`
	Timestamp     string        `json:"timestamp"`
	TraceFile     string        `json:"trace_file"`
	HTTPMethod    string        `json:"http_method,omitempty"`
	URI           string        `json:"uri,omitempty"`
	TotalCalls    int           `json:"total_calls_raw"`
	FilteredCalls int           `json:"total_calls_filtered"`
	DurationMs    float64       `json:"duration_ms"`
	CallTree      *CallNode     `json:"call_tree"`
	ServicesUsed  []ServiceInfo `json:"services_involved"`
}

// ServiceInfo describes a service/class discovered in the trace.
type ServiceInfo struct {
	ClassName string `json:"class"`
	File      string `json:"file"`
	Role      string `json:"role"`
}

// buildNode is an intermediate node used during tree construction.
// It holds both entry and timing data before conversion to CallNode.
type buildNode struct {
	entry      parser.TraceEntry
	exitTime   float64
	returnVal  string
	children   []*buildNode
	parent     *buildNode
}

// Build constructs a CallTree from raw trace entries and a filter config.
// It applies layer 3 collapse (vendor subtrees) and computes durations.
func Build(entries []parser.TraceEntry, cfg *filter.Config, pathPrefix string) *TraceResult {
	return BuildWithOptions(entries, cfg, 0, pathPrefix, nil)
}

// BuildWithOptions constructs a CallTree with configurable post-processing.
func BuildWithOptions(entries []parser.TraceEntry, cfg *filter.Config, totalRaw int, pathPrefix string, opts *BuildOptions) *TraceResult {
	if opts == nil {
		opts = &BuildOptions{ReturnsMode: "truncate", Collapse: true}
	}

	if len(entries) == 0 {
		return &TraceResult{TotalCalls: totalRaw}
	}

	// Phase 1: build raw tree from entries using Level
	root, totalEntries := buildRawTree(entries)
	if totalRaw > 0 {
		totalEntries = totalRaw
	}

	// Phase 2: convert to CallNode tree with filtering + collapse
	callTree := convertNode(root, cfg, pathPrefix, opts.ReturnsMode)

	// Phase 3: merge repeated sibling calls (e.g. registerBundles called 14x)
	mergeRepeatedChildren(callTree)

	// Phase 4: collapse trivial leaf calls
	if opts.Collapse {
		collapseTrivialCalls(callTree)
	}

	// Phase 5: collect services
	services := collectServices(callTree)

	// Phase 6: count filtered entries
	filteredCount := countNodes(callTree)

	// Phase 7: compute total duration
	var durationMs float64
	if root.exitTime > 0 && root.entry.Timestamp > 0 {
		durationMs = (root.exitTime - root.entry.Timestamp) * 1000
	}

	return &TraceResult{
		TotalCalls:    totalEntries,
		FilteredCalls: filteredCount,
		DurationMs:    durationMs,
		CallTree:      callTree,
		ServicesUsed:  services,
	}
}

// BuildFromFiltered constructs a CallTree from pre-filtered trace entries.
// Entries should already have layers 1 & 2 applied (via filter.Config.ShouldKeep).
// totalRaw is the original unfiltered entry count (for stats).
func BuildFromFiltered(entries []parser.TraceEntry, cfg *filter.Config, totalRaw int, pathPrefix string, opts *BuildOptions) *TraceResult {
	return BuildWithOptions(entries, cfg, totalRaw, pathPrefix, opts)
}

// buildRawTree reconstructs the parent-child tree from flat TraceEntry lines
// using FunctionNr to match entry/exit/return.
func buildRawTree(entries []parser.TraceEntry) (*buildNode, int) {
	// Sentinel root
	root := &buildNode{
		entry: parser.TraceEntry{Level: 0, FunctionName: "<root>"},
	}

	// Index entry nodes by FunctionNr for exit/return matching
	nodeByFuncNr := map[int]*buildNode{}
	current := root
	totalEntries := 0

	for _, e := range entries {
		if e.IsEntry {
			totalEntries++
			node := &buildNode{entry: e, parent: current}
			current.children = append(current.children, node)
			nodeByFuncNr[e.FunctionNr] = node
			current = node
		} else if e.IsExit {
			if n, ok := nodeByFuncNr[e.FunctionNr]; ok {
				n.exitTime = e.Timestamp
			}
			// Walk back up: current might have been a deeper child
			if n, ok := nodeByFuncNr[e.FunctionNr]; ok && n.parent != nil {
				current = n.parent
			}
		} else if e.IsReturn {
			if n, ok := nodeByFuncNr[e.FunctionNr]; ok {
				n.returnVal = e.ReturnValue
			}
		}
	}

	// Set root exit time from last exit
	if root.exitTime == 0 && len(root.children) > 0 {
		// Use the first child (usually {main}) as the effective root
		main := root.children[0]
		root.entry.Timestamp = main.entry.Timestamp
		root.exitTime = main.exitTime
	}

	return root, totalEntries
}

// convertNode recursively converts a buildNode tree into a CallNode tree,
// applying filter layer 3 (vendor collapse).
func convertNode(bn *buildNode, cfg *filter.Config, pathPrefix string, returnsMode string) *CallNode {
	if bn == nil {
		return nil
	}

	// Skip the sentinel root — process its children directly
	if bn.entry.FunctionName == "<root>" {
		if len(bn.children) == 0 {
			return nil
		}
		// Usually there's a single root ({main}), return it
		if len(bn.children) == 1 {
			return convertNode(bn.children[0], cfg, pathPrefix, returnsMode)
		}
		// Multiple roots (shouldn't happen normally): wrap them
		node := &CallNode{FunctionName: "{root}"}
		for _, child := range bn.children {
			if cn := convertNode(child, cfg, pathPrefix, returnsMode); cn != nil {
				node.Children = append(node.Children, cn)
			}
		}
		return node
	}

	isApp := cfg.IsAppCode(bn.entry.FunctionName)
	isExcluded := cfg.IsExcluded(bn.entry.FunctionName)
	isInternal := !bn.entry.UserDefined

	// Skip excluded namespace entries and internal functions entirely,
	// but still recurse into children to find app code buried deeper
	if isExcluded || isInternal {
		return promoteAppChildren(bn, cfg, pathPrefix, returnsMode)
	}

	// This is an app or non-excluded vendor node — build it
	node := entryToCallNode(bn, pathPrefix, returnsMode)
	extSeen := map[string]bool{} // dedup external calls

	addExternal := func(name string) {
		if !extSeen[name] {
			extSeen[name] = true
			node.ExternalCalls = append(node.ExternalCalls, name)
		}
	}

	for _, child := range bn.children {
		childIsApp := cfg.IsAppCode(child.entry.FunctionName)
		childIsExcluded := cfg.IsExcluded(child.entry.FunctionName)
		childIsInternal := !child.entry.UserDefined

		if childIsApp {
			// App child: recurse normally
			if cn := convertNode(child, cfg, pathPrefix, returnsMode); cn != nil {
				node.Children = append(node.Children, cn)
			}
		} else if childIsInternal || childIsExcluded {
			// If parent is app code and child is an excluded vendor call,
			// record it as an external call (layer 3 collapse), deduplicated
			if isApp && childIsExcluded {
				addExternal(child.entry.FunctionName)
			}
			// Still promote any app grandchildren buried inside
			if promoted := promoteAppChildren(child, cfg, pathPrefix, returnsMode); promoted != nil {
				if promoted.Children != nil {
					node.Children = append(node.Children, promoted.Children...)
				}
			}
		} else {
			// Non-excluded vendor call from app code: collapse (layer 3)
			// Keep the call reference but don't descend
			if isApp {
				addExternal(child.entry.FunctionName)
			} else {
				// vendor calling vendor (not excluded) — still recurse
				if cn := convertNode(child, cfg, pathPrefix, returnsMode); cn != nil {
					node.Children = append(node.Children, cn)
				}
			}
		}
	}

	return node
}

// promoteAppChildren finds app-code descendants inside a non-app subtree
// and returns them as a virtual wrapper (or nil if none found).
func promoteAppChildren(bn *buildNode, cfg *filter.Config, pathPrefix string, returnsMode string) *CallNode {
	var promoted []*CallNode
	for _, child := range bn.children {
		if cfg.IsAppCode(child.entry.FunctionName) {
			if cn := convertNode(child, cfg, pathPrefix, returnsMode); cn != nil {
				promoted = append(promoted, cn)
			}
		} else {
			// Recurse deeper
			if wrapper := promoteAppChildren(child, cfg, pathPrefix, returnsMode); wrapper != nil {
				promoted = append(promoted, wrapper.Children...)
			}
		}
	}
	if len(promoted) == 0 {
		return nil
	}
	return &CallNode{Children: promoted}
}

// entryToCallNode creates a CallNode from a buildNode.
func entryToCallNode(bn *buildNode, pathPrefix string, returnsMode string) *CallNode {
	className, methodName := splitFunctionName(bn.entry.FunctionName)

	var durationMs float64
	if bn.exitTime > 0 && bn.entry.Timestamp > 0 {
		durationMs = (bn.exitTime - bn.entry.Timestamp) * 1000
	}

	file := bn.entry.Filename
	if pathPrefix != "" {
		file = strings.TrimPrefix(file, pathPrefix)
	}

	var retVal string
	switch returnsMode {
	case "none":
		// omit
	case "type":
		retVal = ExtractType(bn.returnVal)
	default: // "truncate"
		retVal = truncate(bn.returnVal, maxValueLen)
	}

	return &CallNode{
		FunctionName: bn.entry.FunctionName,
		ClassName:    className,
		MethodName:   methodName,
		File:         file,
		Line:         bn.entry.LineNumber,
		Params:       truncateStrings(bn.entry.Params, maxValueLen),
		ReturnValue:  retVal,
		DurationMs:   durationMs,
	}
}

// ExtractType extracts the type from an XDebug return value string.
func ExtractType(val string) string {
	if val == "" {
		return ""
	}
	if val == "TRUE" || val == "FALSE" {
		return "bool"
	}
	if val == "NULL" {
		return "null"
	}
	if strings.HasPrefix(val, "'") {
		return "string"
	}
	if strings.HasPrefix(val, "array(") {
		if idx := strings.Index(val, ")"); idx != -1 {
			return val[:idx+1]
		}
		return "array"
	}
	if strings.HasPrefix(val, "class ") {
		parts := strings.Fields(val)
		if len(parts) >= 2 {
			fqcn := parts[1]
			if idx := strings.LastIndex(fqcn, "\\"); idx != -1 {
				return fqcn[idx+1:]
			}
			return fqcn
		}
	}
	if len(val) > 0 && (val[0] >= '0' && val[0] <= '9' || val[0] == '-') {
		if strings.Contains(val, ".") {
			return "float"
		}
		return "int"
	}
	return truncate(val, 50)
}

const maxValueLen = 200

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func truncateStrings(ss []string, max int) []string {
	if len(ss) == 0 {
		return ss
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = truncate(s, max)
	}
	return out
}

// splitFunctionName extracts class and method from "Class->method" or "Class::method".
func splitFunctionName(name string) (className, methodName string) {
	if idx := strings.Index(name, "->"); idx != -1 {
		return name[:idx], name[idx+2:]
	}
	if idx := strings.Index(name, "::"); idx != -1 {
		return name[:idx], name[idx+2:]
	}
	return "", name
}

// detectRole determines the role of a class based on naming conventions.
func detectRole(className, file string) string {
	combined := className + "|" + file
	switch {
	case strings.Contains(combined, "Controller"):
		return "controller"
	case strings.Contains(combined, "Repository"):
		return "repository"
	case strings.Contains(combined, "Entity") || strings.Contains(combined, "Model"):
		return "entity"
	case strings.Contains(combined, "EventListener") || strings.Contains(combined, "EventSubscriber"):
		return "event_listener"
	case strings.Contains(combined, "Command"):
		return "command_handler"
	case strings.Contains(combined, "Query"):
		return "query_handler"
	default:
		return "service"
	}
}

// collectServices walks the call tree and extracts unique ServiceInfo entries.
func collectServices(node *CallNode) []ServiceInfo {
	if node == nil {
		return nil
	}
	seen := map[string]bool{}
	var services []ServiceInfo
	walkServices(node, seen, &services)
	return services
}

func walkServices(node *CallNode, seen map[string]bool, services *[]ServiceInfo) {
	if node.ClassName != "" && !seen[node.ClassName] {
		seen[node.ClassName] = true
		*services = append(*services, ServiceInfo{
			ClassName: node.ClassName,
			File:      node.File,
			Role:      detectRole(node.ClassName, node.File),
		})
	}
	for _, child := range node.Children {
		walkServices(child, seen, services)
	}
}

// countNodes counts the number of CallNodes in the tree.
func countNodes(node *CallNode) int {
	if node == nil {
		return 0
	}
	count := 1
	for _, child := range node.Children {
		count += countNodes(child)
	}
	return count
}

// mergeRepeatedChildren collapses consecutive sibling calls to the same function.
// e.g. 14 calls to registerBundles become a single node with CallCount=14,
// duration summed, children merged, external calls deduplicated.
func mergeRepeatedChildren(node *CallNode) {
	if node == nil {
		return
	}

	// First recurse into all children
	for _, child := range node.Children {
		mergeRepeatedChildren(child)
	}

	// Then merge consecutive siblings with the same FunctionName
	if len(node.Children) < 2 {
		return
	}

	merged := make([]*CallNode, 0, len(node.Children))
	i := 0
	for i < len(node.Children) {
		current := node.Children[i]
		count := 1
		totalDuration := current.DurationMs

		// Collect all external calls into a dedup set
		extSeen := map[string]bool{}
		for _, e := range current.ExternalCalls {
			extSeen[e] = true
		}

		// Look ahead for consecutive identical function names
		j := i + 1
		for j < len(node.Children) && node.Children[j].FunctionName == current.FunctionName {
			count++
			totalDuration += node.Children[j].DurationMs

			// Merge children from repeated calls into the first one
			current.Children = append(current.Children, node.Children[j].Children...)

			// Dedup external calls
			for _, e := range node.Children[j].ExternalCalls {
				if !extSeen[e] {
					extSeen[e] = true
					current.ExternalCalls = append(current.ExternalCalls, e)
				}
			}
			j++
		}

		if count > 1 {
			current.CallCount = count
			current.DurationMs = totalDuration
		}
		merged = append(merged, current)
		i = j
	}

	node.Children = merged
}

// collapseTrivialCalls replaces groups of trivial leaf children (getters, setters,
// hydration methods) with a single summary node to reduce noise.
func collapseTrivialCalls(node *CallNode) {
	if node == nil || len(node.Children) == 0 {
		return
	}

	var trivial []*CallNode
	var significant []*CallNode

	for _, child := range node.Children {
		if isTrivialSubtree(child) {
			trivial = append(trivial, child)
		} else {
			significant = append(significant, child)
			collapseTrivialCalls(child) // recurse on significant children
		}
	}

	// Only collapse if there are more than 5 trivial calls
	if len(trivial) > 5 {
		var totalDuration float64
		var totalCount int
		seen := map[string]bool{}
		var uniqueNames []string

		for _, t := range trivial {
			totalDuration += t.DurationMs
			count := t.CallCount
			if count == 0 {
				count = 1
			}
			totalCount += count

			collectTrivialNames(t, seen, &uniqueNames)
		}

		summary := &CallNode{
			FunctionName:   fmt.Sprintf("[%d trivial calls collapsed]", totalCount),
			CollapsedCalls: uniqueNames,
			CallCount:      totalCount,
			DurationMs:     totalDuration,
		}
		node.Children = append(significant, summary)
	} else {
		// Few trivial calls, keep them as-is
		node.Children = append(significant, trivial...)
	}
}

// isTrivialMethod returns true if the node itself looks trivial: a short
// accessor/hydrator/constructor under 1ms. Does not look at children.
func isTrivialMethod(node *CallNode) bool {
	if node.DurationMs > 1.0 {
		return false
	}
	method := node.MethodName
	return strings.HasPrefix(method, "get") ||
		strings.HasPrefix(method, "set") ||
		strings.HasPrefix(method, "is") ||
		strings.HasPrefix(method, "has") ||
		method == "toDomain" ||
		method == "toArray" ||
		method == "__construct" ||
		method == "__toString" ||
		method == "__destruct"
}

// isTrivialSubtree returns true if the node AND all its descendants are
// trivial. A constructor whose body only assigns properties is trivial;
// a constructor that triggers an HTTP call is not (one of its descendants
// will fail isTrivialMethod).
func isTrivialSubtree(node *CallNode) bool {
	if !isTrivialMethod(node) {
		return false
	}
	for _, child := range node.Children {
		if !isTrivialSubtree(child) {
			return false
		}
	}
	return true
}

// collectTrivialNames walks a trivial subtree and appends every unique
// method name it encounters into uniqueNames (via the seen set).
func collectTrivialNames(node *CallNode, seen map[string]bool, uniqueNames *[]string) {
	if !seen[node.FunctionName] {
		seen[node.FunctionName] = true
		*uniqueNames = append(*uniqueNames, node.FunctionName)
	}
	for _, child := range node.Children {
		collectTrivialNames(child, seen, uniqueNames)
	}
}
