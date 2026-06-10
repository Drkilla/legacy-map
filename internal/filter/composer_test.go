package filter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeComposer(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDetectComposerAppNamespaces_PSR4(t *testing.T) {
	dir := writeComposer(t, `{
		"autoload": {
			"psr-4": {
				"Acme\\Billing\\": "src/Billing/",
				"Acme\\": "src/"
			}
		}
	}`)

	got := DetectComposerAppNamespaces(dir)
	want := []string{`Acme\`, `Acme\Billing\`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDetectComposerAppNamespaces_PSR0AndTrailingBackslash(t *testing.T) {
	dir := writeComposer(t, `{
		"autoload": {
			"psr-4": {"App\\": "src/"},
			"psr-0": {"Legacy_Stuff\\\\": "lib/"}
		}
	}`)

	got := DetectComposerAppNamespaces(dir)
	want := []string{`App\`, `Legacy_Stuff\`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDetectComposerAppNamespaces_Missing(t *testing.T) {
	if got := DetectComposerAppNamespaces(t.TempDir()); got != nil {
		t.Errorf("expected nil for missing composer.json, got %v", got)
	}
}

func TestDetectComposerAppNamespaces_Invalid(t *testing.T) {
	dir := writeComposer(t, `{not json`)
	if got := DetectComposerAppNamespaces(dir); got != nil {
		t.Errorf("expected nil for invalid composer.json, got %v", got)
	}
}

func TestDetectComposerAppNamespaces_NoAutoload(t *testing.T) {
	dir := writeComposer(t, `{"require": {"php": ">=8.1"}}`)
	if got := DetectComposerAppNamespaces(dir); got != nil {
		t.Errorf("expected nil when no autoload section, got %v", got)
	}
}

func TestPresetExcludes(t *testing.T) {
	if extra, err := PresetExcludes(""); err != nil || extra != nil {
		t.Errorf("empty preset: got (%v, %v), want (nil, nil)", extra, err)
	}

	if extra, err := PresetExcludes("symfony"); err != nil || len(extra) != 0 {
		t.Errorf("symfony preset: got (%v, %v)", extra, err)
	}

	extra, err := PresetExcludes("Laravel")
	if err != nil {
		t.Fatalf("laravel preset: %v", err)
	}
	found := false
	for _, ns := range extra {
		if ns == `Illuminate\` {
			found = true
		}
	}
	if !found {
		t.Errorf("laravel preset should exclude Illuminate\\, got %v", extra)
	}

	if _, err := PresetExcludes("wordpress"); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func TestPresetLaravel_FiltersIlluminate(t *testing.T) {
	extra, _ := PresetExcludes("laravel")
	excluded := append(append([]string{}, DefaultExcludedNamespaces...), extra...)
	cfg := NewConfig(excluded, []string{`App\`})

	if !cfg.IsExcluded(`Illuminate\Routing\Router->dispatch`) {
		t.Error("Illuminate should be excluded with laravel preset")
	}
	if cfg.IsExcluded(`App\Http\Controllers\HomeController->index`) {
		t.Error("App code should not be excluded")
	}
}
