package calltree

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/drkilla/legacy-map/internal/filter"
	"github.com/drkilla/legacy-map/internal/parser"
)

const testFixturePath = "../../testdata/simple.xt"

func TestBuild_Fixture(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()
	result := Build(entries, cfg, "/var/www/app/")

	if result.TotalCalls != 10 {
		t.Errorf("expected 10 total calls, got %d", result.TotalCalls)
	}

	if result.CallTree == nil {
		t.Fatal("call tree is nil")
	}

	// Root should be {main}
	if result.CallTree.FunctionName != "{main}" {
		t.Errorf("expected root {main}, got %q", result.CallTree.FunctionName)
	}

	// Duration should be > 0
	if result.DurationMs <= 0 {
		t.Errorf("expected positive duration, got %f", result.DurationMs)
	}
}

func TestBuild_Fixture_AppNodesOnly(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()
	result := Build(entries, cfg, "/var/www/app/")

	// Walk the tree and verify no Doctrine/Symfony/internal nodes exist as children
	assertNoExcluded(t, result.CallTree, cfg)
}

func assertNoExcluded(t *testing.T, node *CallNode, cfg *filter.Config) {
	t.Helper()
	if node == nil {
		return
	}
	for _, child := range node.Children {
		if cfg.IsExcluded(child.FunctionName) {
			t.Errorf("excluded function in tree: %s", child.FunctionName)
		}
		if child.FunctionName == "strlen" || child.FunctionName == "array_map" {
			t.Errorf("internal PHP function in tree: %s", child.FunctionName)
		}
		assertNoExcluded(t, child, cfg)
	}
}

func TestBuild_Fixture_ExternalCalls(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()
	result := Build(entries, cfg, "/var/www/app/")

	// Find ReservationRepository->save — it should have Doctrine external calls
	repo := findNode(result.CallTree, `App\Repository\ReservationRepository->save`)
	if repo == nil {
		t.Fatal("could not find ReservationRepository->save in tree")
	}
	if len(repo.ExternalCalls) == 0 {
		t.Error("ReservationRepository->save should have external calls (Doctrine)")
	}

	found := false
	for _, ec := range repo.ExternalCalls {
		if ec == `Doctrine\ORM\EntityManagerInterface->persist` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Doctrine persist in external calls, got %v", repo.ExternalCalls)
	}
}

func TestBuild_Fixture_Services(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()
	result := Build(entries, cfg, "/var/www/app/")

	roles := map[string]string{}
	for _, s := range result.ServicesUsed {
		roles[s.ClassName] = s.Role
	}

	expected := map[string]string{
		`App\Controller\ReservationController`:  "controller",
		`App\Service\ReservationService`:        "service",
		`App\Entity\Reservation`:                "entity",
		`App\Repository\ReservationRepository`:  "repository",
	}

	for cls, role := range expected {
		got, ok := roles[cls]
		if !ok {
			t.Errorf("missing service: %s", cls)
			continue
		}
		if got != role {
			t.Errorf("service %s: expected role %q, got %q", cls, role, got)
		}
	}
}

func TestBuild_Fixture_RelativePaths(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()
	result := Build(entries, cfg, "/var/www/app/")

	// Walk and check no absolute paths remain
	assertRelativePaths(t, result.CallTree)
}

func assertRelativePaths(t *testing.T, node *CallNode) {
	t.Helper()
	if node == nil {
		return
	}
	if len(node.File) > 0 && node.File[0] == '/' {
		t.Errorf("absolute path found: %s", node.File)
	}
	for _, child := range node.Children {
		assertRelativePaths(t, child)
	}
}

func TestBuild_Fixture_ReturnValues(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()
	result := Build(entries, cfg, "/var/www/app/")

	repo := findNode(result.CallTree, `App\Repository\ReservationRepository->save`)
	if repo == nil {
		t.Fatal("could not find ReservationRepository->save")
	}
	if repo.ReturnValue != "TRUE" {
		t.Errorf("expected return value 'TRUE', got %q", repo.ReturnValue)
	}
}

func TestSplitFunctionName(t *testing.T) {
	tests := []struct {
		input                string
		wantClass, wantMethod string
	}{
		{`App\Service\Foo->bar`, `App\Service\Foo`, "bar"},
		{`App\Service\Foo::staticMethod`, `App\Service\Foo`, "staticMethod"},
		{"strlen", "", "strlen"},
		{"{main}", "", "{main}"},
	}
	for _, tt := range tests {
		cls, method := splitFunctionName(tt.input)
		if cls != tt.wantClass || method != tt.wantMethod {
			t.Errorf("splitFunctionName(%q) = (%q, %q), want (%q, %q)",
				tt.input, cls, method, tt.wantClass, tt.wantMethod)
		}
	}
}

func TestDetectRole(t *testing.T) {
	tests := []struct {
		className string
		file      string
		want      string
	}{
		{`App\Controller\FooController`, "src/Controller/FooController.php", "controller"},
		{`App\Repository\BarRepository`, "src/Repository/BarRepository.php", "repository"},
		{`App\Entity\Baz`, "src/Entity/Baz.php", "entity"},
		{`App\EventListener\FooListener`, "", "event_listener"},
		{`App\EventSubscriber\BarSubscriber`, "", "event_listener"},
		{`App\Command\DoStuffCommand`, "", "command_handler"},
		{`App\Query\GetStuffQuery`, "", "query_handler"},
		{`App\Service\Whatever`, "", "service"},
	}
	for _, tt := range tests {
		got := detectRole(tt.className, tt.file)
		if got != tt.want {
			t.Errorf("detectRole(%q, %q) = %q, want %q", tt.className, tt.file, got, tt.want)
		}
	}
}

func findNode(node *CallNode, funcName string) *CallNode {
	if node == nil {
		return nil
	}
	if node.FunctionName == funcName {
		return node
	}
	for _, child := range node.Children {
		if found := findNode(child, funcName); found != nil {
			return found
		}
	}
	return nil
}

// --- Real trace integration test ---

const realTraceDir = "/home/drkilla/projects/ezyformalite/xdebug-traces"

func TestBuild_RealTrace(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(realTraceDir, "*.xt"))
	if err != nil || len(files) == 0 {
		t.Skipf("no .xt files in %s", realTraceDir)
	}

	cfg := filter.NewDefaultConfig()

	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			entries, err := parser.ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}

			result := Build(entries, cfg, "/app/")

			fmt.Printf("  %s\n", name)
			fmt.Printf("    Raw calls:      %d\n", result.TotalCalls)
			fmt.Printf("    Tree nodes:     %d\n", result.FilteredCalls)
			fmt.Printf("    Duration:       %.1f ms\n", result.DurationMs)
			fmt.Printf("    Services:       %d\n", len(result.ServicesUsed))

			for _, s := range result.ServicesUsed {
				fmt.Printf("      [%s] %s\n", s.Role, s.ClassName)
			}

			if result.CallTree == nil {
				t.Error("call tree is nil")
				return
			}

			// Dump first 2 levels of tree as JSON for visibility
			preview := shallowCopy(result.CallTree, 2)
			b, _ := json.MarshalIndent(preview, "    ", "  ")
			fmt.Printf("    Tree preview:\n    %s\n", string(b))
		})
	}
}

// shallowCopy returns a copy of the tree truncated at maxDepth.
func shallowCopy(node *CallNode, maxDepth int) *CallNode {
	if node == nil || maxDepth < 0 {
		return nil
	}
	cp := *node
	cp.Children = nil
	cp.ExternalCalls = node.ExternalCalls
	if maxDepth > 0 {
		for _, child := range node.Children {
			if sc := shallowCopy(child, maxDepth-1); sc != nil {
				cp.Children = append(cp.Children, sc)
			}
		}
	} else if len(node.Children) > 0 {
		cp.Children = []*CallNode{{FunctionName: fmt.Sprintf("... %d more children", len(node.Children))}}
	}
	return &cp
}

func TestExtractType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"TRUE", "bool"},
		{"FALSE", "bool"},
		{"NULL", "null"},
		{"'hello world'", "string"},
		{"42", "int"},
		{"-7", "int"},
		{"3.14", "float"},
		{"-0.5", "float"},
		{"array(3) { [0]=> ... }", "array(3)"},
		{"array(0) {}", "array(0)"},
		{`class App\Domain\Customer\CustomerDetailReadModel { ... }`, "CustomerDetailReadModel"},
		{`class stdClass { ... }`, "stdClass"},
		{"???", "???"},
	}
	for _, tt := range tests {
		got := ExtractType(tt.input)
		if got != tt.want {
			t.Errorf("ExtractType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCollapseTrivialCalls(t *testing.T) {
	// Build a parent with 10 trivial getters and 2 significant children
	parent := &CallNode{
		FunctionName: `App\Repository\CaseRepository->findAll`,
		ClassName:    `App\Repository\CaseRepository`,
		MethodName:   "findAll",
	}

	for i := 0; i < 10; i++ {
		parent.Children = append(parent.Children, &CallNode{
			FunctionName: fmt.Sprintf(`App\Entity\Case->getId`),
			ClassName:    `App\Entity\Case`,
			MethodName:   "getId",
			DurationMs:   0.01,
		})
	}
	parent.Children = append(parent.Children, &CallNode{
		FunctionName: `App\Service\CaseService->process`,
		ClassName:    `App\Service\CaseService`,
		MethodName:   "process",
		DurationMs:   50.0,
	})

	collapseTrivialCalls(parent)

	// Should have 2 children: the significant one + 1 collapsed summary
	if len(parent.Children) != 2 {
		t.Fatalf("expected 2 children after collapse, got %d", len(parent.Children))
	}

	// Find the summary node
	var summary *CallNode
	for _, c := range parent.Children {
		if len(c.CollapsedCalls) > 0 {
			summary = c
		}
	}
	if summary == nil {
		t.Fatal("no collapsed summary node found")
	}
	if summary.CallCount != 10 {
		t.Errorf("expected CallCount=10, got %d", summary.CallCount)
	}
	if len(summary.CollapsedCalls) != 1 {
		t.Errorf("expected 1 unique collapsed call, got %d: %v", len(summary.CollapsedCalls), summary.CollapsedCalls)
	}
}

func TestCollapseTrivialCalls_FewTrivials_NoCollapse(t *testing.T) {
	parent := &CallNode{FunctionName: "parent"}
	for i := 0; i < 3; i++ {
		parent.Children = append(parent.Children, &CallNode{
			FunctionName: `App\Entity\Foo->getName`,
			MethodName:   "getName",
			DurationMs:   0.01,
		})
	}

	collapseTrivialCalls(parent)

	// 3 trivial calls should NOT be collapsed (threshold is >5)
	if len(parent.Children) != 3 {
		t.Errorf("expected 3 children (no collapse), got %d", len(parent.Children))
	}
}

func TestBuildFromFiltered_ReturnsMode(t *testing.T) {
	entries, err := parser.ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := filter.NewDefaultConfig()

	// Mode "none" should omit return values
	result := BuildWithOptions(entries, cfg, 0, "/var/www/app/", &BuildOptions{
		ReturnsMode: "none",
		Collapse:    false,
	})
	repo := findNode(result.CallTree, `App\Repository\ReservationRepository->save`)
	if repo == nil {
		t.Fatal("could not find ReservationRepository->save")
	}
	if repo.ReturnValue != "" {
		t.Errorf("returns=none: expected empty return, got %q", repo.ReturnValue)
	}

	// Mode "type" should extract type
	result2 := BuildWithOptions(entries, cfg, 0, "/var/www/app/", &BuildOptions{
		ReturnsMode: "type",
		Collapse:    false,
	})
	repo2 := findNode(result2.CallTree, `App\Repository\ReservationRepository->save`)
	if repo2 == nil {
		t.Fatal("could not find ReservationRepository->save")
	}
	if repo2.ReturnValue != "bool" {
		t.Errorf("returns=type: expected 'bool' for TRUE, got %q", repo2.ReturnValue)
	}
}

func init() {
	// Silence unused import
	_ = os.Stdout
}

func TestCollapseTrivialSubtree_Recursive(t *testing.T) {
	// 10 x (toDomain -> __construct), all <1ms → entire subtrees trivial → collapse
	parent := &CallNode{FunctionName: "parent"}
	for i := 0; i < 10; i++ {
		parent.Children = append(parent.Children, &CallNode{
			FunctionName: `App\Entity\Settings->toDomain`,
			MethodName:   "toDomain",
			DurationMs:   0.02,
			Children: []*CallNode{{
				FunctionName: `App\Domain\Settings->__construct`,
				MethodName:   "__construct",
				DurationMs:   0.01,
			}},
		})
	}

	collapseTrivialCalls(parent)

	if len(parent.Children) != 1 {
		t.Fatalf("expected 1 summary child after recursive collapse, got %d", len(parent.Children))
	}
	summary := parent.Children[0]
	if summary.CallCount != 10 {
		t.Errorf("expected CallCount=10, got %d", summary.CallCount)
	}
	// Collapsed names should cover both toDomain and __construct
	if len(summary.CollapsedCalls) < 2 {
		t.Errorf("expected collapsed names to include both methods, got %v", summary.CollapsedCalls)
	}
}

func TestIsTrivialSubtree(t *testing.T) {
	trivial := &CallNode{
		MethodName: "toDomain", DurationMs: 0.1,
		Children: []*CallNode{{MethodName: "__construct", DurationMs: 0.1}},
	}
	if !isTrivialSubtree(trivial) {
		t.Error("toDomain→__construct should be trivial")
	}

	withHTTP := &CallNode{
		MethodName: "toDomain", DurationMs: 0.1,
		Children: []*CallNode{{MethodName: "httpCall", DurationMs: 10}},
	}
	if isTrivialSubtree(withHTTP) {
		t.Error("toDomain with httpCall child should NOT be trivial")
	}

	deepHTTP := &CallNode{
		MethodName: "toDomain", DurationMs: 0.1,
		Children: []*CallNode{{
			MethodName: "__construct", DurationMs: 0.1,
			Children: []*CallNode{{MethodName: "httpCall", DurationMs: 5}},
		}},
	}
	if isTrivialSubtree(deepHTTP) {
		t.Error("trivial wrapper over deep httpCall should NOT be trivial (recursive)")
	}
}
