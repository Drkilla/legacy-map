package filter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// composerJSON is the subset of composer.json needed for namespace detection.
type composerJSON struct {
	Autoload struct {
		PSR4 map[string]json.RawMessage `json:"psr-4"`
		PSR0 map[string]json.RawMessage `json:"psr-0"`
	} `json:"autoload"`
}

// DetectComposerAppNamespaces reads composer.json in dir and returns the
// PSR-4/PSR-0 autoload namespace prefixes, normalized to end with a single
// backslash (the format expected by the app-namespace trie).
// Returns nil if composer.json is missing or unreadable.
func DetectComposerAppNamespaces(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return nil
	}

	var c composerJSON
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}

	seen := map[string]bool{}
	var namespaces []string
	add := func(prefix string) {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return
		}
		prefix = strings.TrimRight(prefix, `\`) + `\`
		if !seen[prefix] {
			seen[prefix] = true
			namespaces = append(namespaces, prefix)
		}
	}

	for prefix := range c.Autoload.PSR4 {
		add(prefix)
	}
	for prefix := range c.Autoload.PSR0 {
		add(prefix)
	}

	sort.Strings(namespaces)
	return namespaces
}
