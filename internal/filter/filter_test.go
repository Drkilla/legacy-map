package filter

import (
	"testing"

	"github.com/drkilla/legacy-map/internal/parser"
)

func TestShouldKeep_Layer1_InternalFunctions(t *testing.T) {
	cfg := NewDefaultConfig()

	internal := parser.TraceEntry{IsEntry: true, FunctionName: "strlen", UserDefined: false}
	if cfg.ShouldKeep(internal) {
		t.Error("internal PHP function should be filtered out")
	}

	userDef := parser.TraceEntry{IsEntry: true, FunctionName: `App\Service\Foo->bar`, UserDefined: true}
	if !cfg.ShouldKeep(userDef) {
		t.Error("user-defined function should be kept")
	}
}

func TestShouldKeep_Layer1_KeepInternal(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.KeepInternal = true

	internal := parser.TraceEntry{IsEntry: true, FunctionName: "strlen", UserDefined: false}
	if !cfg.ShouldKeep(internal) {
		t.Error("internal function should be kept when KeepInternal is true")
	}
}

func TestShouldKeep_Layer2_ExcludedNamespace(t *testing.T) {
	cfg := NewDefaultConfig()

	tests := []struct {
		name string
		want bool
	}{
		{`Symfony\Component\HttpKernel\Kernel->handle`, false},
		{`Doctrine\ORM\EntityManager->persist`, false},
		{`Twig\Environment->render`, false},
		{`Monolog\Logger->info`, false},
		{`App\Controller\FooController->index`, true},
		{`App\Service\BarService->execute`, true},
	}

	for _, tt := range tests {
		e := parser.TraceEntry{IsEntry: true, FunctionName: tt.name, UserDefined: true}
		got := cfg.ShouldKeep(e)
		if got != tt.want {
			t.Errorf("ShouldKeep(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestShouldKeep_ExitAndReturnAlwaysKept(t *testing.T) {
	cfg := NewDefaultConfig()

	exit := parser.TraceEntry{IsExit: true}
	if !cfg.ShouldKeep(exit) {
		t.Error("exit lines should always be kept")
	}

	ret := parser.TraceEntry{IsReturn: true, ReturnValue: "foo"}
	if !cfg.ShouldKeep(ret) {
		t.Error("return lines should always be kept")
	}
}

func TestIsAppCode(t *testing.T) {
	cfg := NewDefaultConfig()

	if !cfg.IsAppCode(`App\Service\ReservationService->create`) {
		t.Error("App namespace should be app code")
	}
	if cfg.IsAppCode(`Doctrine\ORM\EntityManager->persist`) {
		t.Error("Doctrine should not be app code")
	}
	if cfg.IsAppCode("strlen") {
		t.Error("internal function should not be app code")
	}
}

func TestFilterEntries_Fixture(t *testing.T) {
	entries, err := parser.ParseFile("../../testdata/simple.xt")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := NewDefaultConfig()
	total, filtered := cfg.CountFiltered(entries)

	// From our fixture: 10 entry lines total
	// Kept: {main}, ReservationController->create, ReservationService->create,
	//        Reservation->__construct, ReservationRepository->save = 5
	// Filtered out: Doctrine::persist, UnitOfWork::scheduleForInsert,
	//               Doctrine::flush, strlen, Symfony\JsonResponse = 5
	if total != 10 {
		t.Errorf("expected 10 total entry lines, got %d", total)
	}
	if filtered != 5 {
		t.Errorf("expected 5 filtered entry lines, got %d", filtered)
	}

	kept := cfg.FilterEntries(entries)
	// Kept entries include entry + exit + return lines for kept functions,
	// plus orphan exit/return lines (for filtered functions)
	// Just verify we have fewer entries than the original
	if len(kept) >= len(entries) {
		t.Errorf("filtering should reduce entry count: original=%d, filtered=%d", len(entries), len(kept))
	}
}

func TestNewConfig_CustomNamespaces(t *testing.T) {
	cfg := NewConfig(
		[]string{`Vendor\`},
		[]string{`MyApp\`},
	)

	e := parser.TraceEntry{IsEntry: true, FunctionName: `Vendor\Lib\Foo->bar`, UserDefined: true}
	if cfg.ShouldKeep(e) {
		t.Error("custom excluded namespace should be filtered")
	}

	if !cfg.IsAppCode(`MyApp\Service\Baz`) {
		t.Error("custom app prefix should match")
	}
	if cfg.IsAppCode(`App\Service\Baz`) {
		t.Error("default App prefix should not match with custom config")
	}
}

func TestIsCompiledContainer(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"symfony compiled container with hash", `ContainerDmKuytK\Something`, true},
		{"alphanumeric hash", `ContainerABC123\Foo`, true},
		{"no hash between Container and backslash", `Container\Foo`, false},
		{"not a prefix", `App\Container\Foo`, false},
		{"no backslash", "ContainerFactory", false},
		{"hash too short", `ContainerAB\Foo`, false},
		{"hash too long", `ContainerABCDEFGHIJKLMNOPQRSTUV\Foo`, false},
		{"non-alphanumeric char in hash", `ContainerAB-CD\Foo`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCompiledContainer(tc.in); got != tc.want {
				t.Errorf("isCompiledContainer(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsExcluded_CompiledContainer(t *testing.T) {
	cfg := NewDefaultConfig()
	if !cfg.IsExcluded(`ContainerDmKuytK\getFooService`) {
		t.Error("compiled container class should be excluded")
	}
}

func TestIsExcluded_NewDefaultNamespaces(t *testing.T) {
	cfg := NewDefaultConfig()
	for _, fn := range []string{
		`OpenApi\Annotations\Info`,
		`Nelmio\ApiDocBundle\Foo`,
		`Lexik\Bundle\JWTAuthenticationBundle\Foo`,
		`Proxies\__CG__\App\Entity\Foo`,
		`GuzzleHttp\Client->send`,
		`Elasticsearch\Client->search`,
		`Lcobucci\JWT\Token`,
		`Ramsey\Uuid\Uuid`,
		`SoapClient->__call`,
	} {
		if !cfg.IsExcluded(fn) {
			t.Errorf("%q should be excluded by default", fn)
		}
	}
}
