package format

import (
	"strings"
	"testing"

	"github.com/drkilla/legacy-map/internal/calltree"
	"github.com/drkilla/legacy-map/internal/filter"
	"github.com/drkilla/legacy-map/internal/parser"
)

func fixtureResult(t *testing.T) *calltree.TraceResult {
	t.Helper()
	entries, err := parser.ParseFile("../../testdata/simple.xt")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	cfg := filter.NewDefaultConfig()
	result := calltree.Build(entries, cfg, "/var/www/app/")
	result.HTTPMethod = "POST"
	result.URI = "/reservations"
	result.TraceFile = "testdata/simple.xt"
	return result
}

func TestTree(t *testing.T) {
	out := Tree(fixtureResult(t))

	for _, want := range []string{
		"POST /reservations",
		"{main}",
		`App\Controller\ReservationController->create`,
		`App\Repository\ReservationRepository->save`,
		`⤷ Doctrine\ORM\EntityManagerInterface->persist`,
		"└── ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output missing %q:\n%s", want, out)
		}
	}
}

func TestTree_EmptyResult(t *testing.T) {
	out := Tree(&calltree.TraceResult{TraceFile: "empty.xt"})
	if !strings.Contains(out, "(empty call tree)") {
		t.Errorf("expected empty tree marker, got:\n%s", out)
	}
}

func TestMermaid(t *testing.T) {
	out := Mermaid(fixtureResult(t))

	if !strings.HasPrefix(out, "sequenceDiagram\n") {
		t.Fatalf("mermaid output should start with sequenceDiagram:\n%s", out)
	}
	for _, want := range []string{
		"participant Main",
		"participant ReservationController",
		"Main->>ReservationController: create",
		"ReservationController->>ReservationService: create",
		"ReservationService->>ReservationRepository: save",
		"ReservationRepository-->>EntityManagerInterface: persist",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mermaid output missing %q:\n%s", want, out)
		}
	}
}

func TestMermaid_CollapsedNodeBecomesNote(t *testing.T) {
	r := &calltree.TraceResult{
		CallTree: &calltree.CallNode{
			FunctionName: `App\Service\Foo->run`,
			Children: []*calltree.CallNode{
				{FunctionName: "[12 trivial calls collapsed]", CallCount: 12},
			},
		},
	}
	out := Mermaid(r)
	if !strings.Contains(out, "Note over Foo: [12 trivial calls collapsed]") {
		t.Errorf("expected collapsed node as note:\n%s", out)
	}
}

func TestMarkdown(t *testing.T) {
	out := Markdown(fixtureResult(t))

	for _, want := range []string{
		"# POST /reservations",
		"| Duration |",
		"## Services involved",
		"`App\\Controller\\ReservationController` | controller",
		"## Sequence diagram",
		"```mermaid\nsequenceDiagram",
		"## External dependencies",
		"`Doctrine\\ORM\\EntityManagerInterface->persist`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q:\n%s", want, out)
		}
	}
}

func TestRender_UnknownFormat(t *testing.T) {
	if _, err := Render(&calltree.TraceResult{}, "yaml"); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestDiffText(t *testing.T) {
	d := &calltree.DiffResult{
		TraceA:    "a.xt",
		TraceB:    "b.xt",
		OnlyInA:   []calltree.DiffEntry{{Function: `App\Old->gone`, CallsA: 3, DurationA: 1.5}},
		OnlyInB:   []calltree.DiffEntry{{Function: `App\New->added`, CallsB: 1}},
		Changed:   []calltree.DiffEntry{{Function: `App\Repo->find`, CallsA: 1, CallsB: 4, DurationA: 2, DurationB: 9}},
		Identical: 26,
	}
	out := DiffText(d)

	for _, want := range []string{
		"− Only in A (1):",
		`3× App\Old->gone (1.5 ms)`,
		"+ Only in B (1):",
		`1× App\New->added`,
		"Δ Call count changed (1):",
		`App\Repo->find: 1× → 4× (2.0 ms → 9.0 ms)`,
		"= 26 functions identical",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output missing %q:\n%s", want, out)
		}
	}
}

func TestDiffText_NoDifferences(t *testing.T) {
	out := DiffText(&calltree.DiffResult{TraceA: "a", TraceB: "b", Identical: 10})
	if !strings.Contains(out, "No differences") {
		t.Errorf("expected no-differences message:\n%s", out)
	}
}
