// Package format renders a parsed TraceResult into human-readable
// representations: ASCII tree, Mermaid sequence diagram, Markdown report.
package format

import (
	"fmt"
	"strings"

	"github.com/drkilla/legacy-map/internal/calltree"
)

// Formats supported by Render, in addition to the default "json" handled
// by the caller.
const (
	FormatTree     = "tree"
	FormatMermaid  = "mermaid"
	FormatMarkdown = "markdown"
)

// Render dispatches to the requested format.
func Render(r *calltree.TraceResult, format string) (string, error) {
	switch format {
	case FormatTree:
		return Tree(r), nil
	case FormatMermaid:
		return Mermaid(r), nil
	case FormatMarkdown:
		return Markdown(r), nil
	default:
		return "", fmt.Errorf("unknown format %q (available: json, tree, mermaid, markdown)", format)
	}
}

// title builds a human title for a trace: "GET /api/clients" or the file name.
func title(r *calltree.TraceResult) string {
	t := strings.TrimSpace(strings.TrimSpace(r.HTTPMethod) + " " + strings.TrimSpace(r.URI))
	if t == "" {
		t = r.TraceFile
	}
	return t
}

// --- ASCII tree ---

// Tree renders the call tree as an indented ASCII tree for terminal reading.
func Tree(r *calltree.TraceResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %.1f ms — %d nodes (from %d raw calls)\n\n",
		title(r), r.DurationMs, r.FilteredCalls, r.TotalCalls)

	if r.CallTree == nil {
		b.WriteString("(empty call tree)\n")
		return b.String()
	}

	b.WriteString(nodeLabel(r.CallTree) + "\n")
	writeTreeChildren(&b, r.CallTree, "")
	return b.String()
}

func nodeLabel(n *calltree.CallNode) string {
	label := n.FunctionName
	if n.RepeatCount > 1 {
		label += fmt.Sprintf(" ×%d (identical subtrees)", n.RepeatCount)
	} else if n.CallCount > 1 && len(n.CollapsedCalls) == 0 {
		label += fmt.Sprintf(" ×%d", n.CallCount)
	}
	if n.DurationMs > 0 {
		label += fmt.Sprintf(" (%.1f ms)", n.DurationMs)
	}
	if len(n.CollapsedCalls) > 0 {
		preview := n.CollapsedCalls
		suffix := ""
		if len(preview) > 3 {
			preview = preview[:3]
			suffix = ", …"
		}
		label += " — " + strings.Join(preview, ", ") + suffix
	}
	return label
}

func writeTreeChildren(b *strings.Builder, n *calltree.CallNode, prefix string) {
	type item struct {
		text string
		node *calltree.CallNode
	}
	items := make([]item, 0, len(n.ExternalCalls)+len(n.Children))
	for _, ext := range n.ExternalCalls {
		items = append(items, item{text: "⤷ " + ext})
	}
	for _, c := range n.Children {
		items = append(items, item{node: c})
	}

	for i, it := range items {
		connector, childPrefix := "├── ", prefix+"│   "
		if i == len(items)-1 {
			connector, childPrefix = "└── ", prefix+"    "
		}
		if it.node == nil {
			b.WriteString(prefix + connector + it.text + "\n")
			continue
		}
		b.WriteString(prefix + connector + nodeLabel(it.node) + "\n")
		writeTreeChildren(b, it.node, childPrefix)
	}
}

// --- Mermaid sequence diagram ---

// Mermaid renders the call tree as a Mermaid sequenceDiagram, ready to embed
// in Markdown documentation.
func Mermaid(r *calltree.TraceResult) string {
	g := &mermaidGen{
		actorIDs: map[string]string{},
	}
	if r.CallTree != nil {
		g.walk(r.CallTree, "")
	}

	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	for _, p := range g.participants {
		fmt.Fprintf(&b, "    participant %s\n", p)
	}
	b.WriteString(g.edges.String())
	return b.String()
}

type mermaidGen struct {
	participants []string          // declaration order
	actorIDs     map[string]string // class/function name → sanitized actor id
	edges        strings.Builder
}

// actor returns the Mermaid actor id for a function name, registering the
// participant on first use.
func (g *mermaidGen) actor(funcName string) string {
	class, method := splitClassMethod(funcName)
	display := class
	if display == "" {
		display = method
	}
	display = shortName(display)
	if display == "{main}" || display == "" {
		display = "Main"
	}

	if id, ok := g.actorIDs[display]; ok {
		return id
	}
	id := sanitizeActorID(display)
	// Avoid collisions between distinct names sanitizing identically
	for _, existing := range g.actorIDs {
		if existing == id {
			id = fmt.Sprintf("%s_%d", id, len(g.actorIDs))
			break
		}
	}
	g.actorIDs[display] = id
	g.participants = append(g.participants, id)
	return id
}

func (g *mermaidGen) walk(n *calltree.CallNode, parentActor string) {
	// Collapsed summary nodes become a note instead of a call edge
	if strings.HasPrefix(n.FunctionName, "[") {
		if parentActor != "" {
			fmt.Fprintf(&g.edges, "    Note over %s: %s\n", parentActor, n.FunctionName)
		}
		return
	}

	self := g.actor(n.FunctionName)
	if parentActor != "" {
		_, method := splitClassMethod(n.FunctionName)
		label := method
		if n.RepeatCount > 1 {
			label += fmt.Sprintf(" ×%d", n.RepeatCount)
		} else if n.CallCount > 1 {
			label += fmt.Sprintf(" ×%d", n.CallCount)
		}
		fmt.Fprintf(&g.edges, "    %s->>%s: %s\n", parentActor, self, label)
	}

	for _, ext := range n.ExternalCalls {
		target := g.actor(ext)
		_, method := splitClassMethod(ext)
		fmt.Fprintf(&g.edges, "    %s-->>%s: %s\n", self, target, method)
	}

	for _, child := range n.Children {
		g.walk(child, self)
	}
}

// --- Markdown report ---

// Markdown renders a full report: stats, services, sequence diagram,
// external dependencies.
func Markdown(r *calltree.TraceResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", title(r))
	if r.Scenario != "" {
		fmt.Fprintf(&b, "_%s_\n\n", r.Scenario)
	}

	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Duration | %.1f ms |\n", r.DurationMs)
	fmt.Fprintf(&b, "| Raw calls | %d |\n", r.TotalCalls)
	fmt.Fprintf(&b, "| Filtered nodes | %d |\n", r.FilteredCalls)
	fmt.Fprintf(&b, "| Trace file | `%s` |\n\n", r.TraceFile)

	if len(r.ServicesUsed) > 0 {
		b.WriteString("## Services involved\n\n")
		b.WriteString("| Class | Role | File |\n|---|---|---|\n")
		for _, s := range r.ServicesUsed {
			fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", s.ClassName, s.Role, s.File)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Sequence diagram\n\n```mermaid\n")
	b.WriteString(Mermaid(r))
	b.WriteString("```\n")

	if deps := collectExternalCalls(r.CallTree); len(deps) > 0 {
		b.WriteString("\n## External dependencies\n\n")
		for _, d := range deps {
			fmt.Fprintf(&b, "- `%s`\n", d)
		}
	}

	return b.String()
}

func collectExternalCalls(n *calltree.CallNode) []string {
	if n == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(*calltree.CallNode)
	walk = func(node *calltree.CallNode) {
		for _, ext := range node.ExternalCalls {
			if !seen[ext] {
				seen[ext] = true
				out = append(out, ext)
			}
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// --- helpers ---

// splitClassMethod splits "Class->method" / "Class::method" into class and method.
func splitClassMethod(name string) (class, method string) {
	if idx := strings.Index(name, "->"); idx != -1 {
		return name[:idx], name[idx+2:]
	}
	if idx := strings.Index(name, "::"); idx != -1 {
		return name[:idx], name[idx+2:]
	}
	return "", name
}

// shortName returns the last namespace segment of a FQCN.
func shortName(fqcn string) string {
	if idx := strings.LastIndex(fqcn, `\`); idx != -1 {
		return fqcn[idx+1:]
	}
	return fqcn
}

// sanitizeActorID makes a string safe to use as a Mermaid participant id.
func sanitizeActorID(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "Unknown"
	}
	return b.String()
}
