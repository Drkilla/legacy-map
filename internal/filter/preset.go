package filter

import (
	"fmt"
	"sort"
	"strings"
)

// presetExcludes maps a framework preset name to extra namespaces excluded
// on top of DefaultExcludedNamespaces (which already covers the Symfony
// ecosystem — Laravel reuses Symfony components, so defaults always apply).
var presetExcludes = map[string][]string{
	"symfony": nil, // defaults already cover Symfony
	"laravel": {
		`Illuminate\`,
		`Laravel\`,
		`Livewire\`,
		`Carbon\`,
		`League\`,
		`Whoops\`,
		`Dotenv\`,
		`Egulias\`,
		`Faker\`,
		`Hamcrest\`,
		`GrahamCampbell\`,
	},
}

// PresetExcludes returns the extra excluded namespaces for a framework
// preset. An empty name is valid and returns nothing.
func PresetExcludes(name string) ([]string, error) {
	if name == "" {
		return nil, nil
	}
	extra, ok := presetExcludes[strings.ToLower(name)]
	if !ok {
		names := make([]string, 0, len(presetExcludes))
		for k := range presetExcludes {
			names = append(names, k)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unknown preset %q (available: %s)", name, strings.Join(names, ", "))
	}
	return extra, nil
}
