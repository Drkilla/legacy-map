package calltree

import "testing"

func TestFlattenStats(t *testing.T) {
	root := &CallNode{
		FunctionName: "{main}",
		Children: []*CallNode{
			{
				FunctionName: `App\Controller->index`,
				DurationMs:   10,
				Children: []*CallNode{
					{FunctionName: `App\Repo->find`, CallCount: 3, DurationMs: 6},
					{FunctionName: `App\Mapper->map`, RepeatCount: 5, DurationMs: 2,
						Children: []*CallNode{{FunctionName: `App\Entity->__construct`}}},
					{FunctionName: "[8 trivial calls collapsed]", CallCount: 8},
				},
			},
		},
	}

	stats := FlattenStats(root)

	if stats[`App\Repo->find`].Calls != 3 {
		t.Errorf("merged CallCount should count as 3, got %d", stats[`App\Repo->find`].Calls)
	}
	if stats[`App\Mapper->map`].Calls != 5 {
		t.Errorf("RepeatCount should count as 5, got %d", stats[`App\Mapper->map`].Calls)
	}
	// Children of a repeated subtree inherit the repeat factor
	if stats[`App\Entity->__construct`].Calls != 5 {
		t.Errorf("child of repeated subtree should count 5, got %d", stats[`App\Entity->__construct`].Calls)
	}
	if _, ok := stats["[8 trivial calls collapsed]"]; ok {
		t.Error("collapsed summary nodes should be skipped")
	}
	if stats[`App\Controller->index`].DurationMs != 10 {
		t.Errorf("duration should aggregate, got %f", stats[`App\Controller->index`].DurationMs)
	}
}

func TestDiff(t *testing.T) {
	a := &TraceResult{
		TraceFile: "a.xt",
		CallTree: &CallNode{
			FunctionName: "{main}",
			Children: []*CallNode{
				{FunctionName: `App\Shared->same`},
				{FunctionName: `App\Old->gone`, CallCount: 2},
				{FunctionName: `App\Repo->find`},
			},
		},
	}
	b := &TraceResult{
		TraceFile: "b.xt",
		CallTree: &CallNode{
			FunctionName: "{main}",
			Children: []*CallNode{
				{FunctionName: `App\Shared->same`},
				{FunctionName: `App\New->added`},
				{FunctionName: `App\Repo->find`, CallCount: 4},
			},
		},
	}

	d := Diff(a, b)

	if len(d.OnlyInA) != 1 || d.OnlyInA[0].Function != `App\Old->gone` || d.OnlyInA[0].CallsA != 2 {
		t.Errorf("unexpected OnlyInA: %+v", d.OnlyInA)
	}
	if len(d.OnlyInB) != 1 || d.OnlyInB[0].Function != `App\New->added` {
		t.Errorf("unexpected OnlyInB: %+v", d.OnlyInB)
	}
	if len(d.Changed) != 1 || d.Changed[0].Function != `App\Repo->find` ||
		d.Changed[0].CallsA != 1 || d.Changed[0].CallsB != 4 {
		t.Errorf("unexpected Changed: %+v", d.Changed)
	}
	// {main} and App\Shared->same are identical
	if d.Identical != 2 {
		t.Errorf("expected 2 identical functions, got %d", d.Identical)
	}
}

func TestDiff_Identical(t *testing.T) {
	tree := &CallNode{
		FunctionName: "{main}",
		Children:     []*CallNode{{FunctionName: `App\Foo->bar`}},
	}
	d := Diff(&TraceResult{CallTree: tree}, &TraceResult{CallTree: tree})
	if len(d.OnlyInA)+len(d.OnlyInB)+len(d.Changed) != 0 {
		t.Errorf("expected no differences: %+v", d)
	}
	if d.Identical != 2 {
		t.Errorf("expected 2 identical, got %d", d.Identical)
	}
}
