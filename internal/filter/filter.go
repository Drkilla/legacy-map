package filter

import (
	"strings"

	"github.com/drkilla/legacy-map/internal/parser"
)

// DefaultExcludedNamespaces is the list of namespaces excluded by default (layer 2).
var DefaultExcludedNamespaces = []string{
	`Symfony\`,
	`Doctrine\`,
	`Twig\`,
	`Monolog\`,
	`Psr\`,
	`Composer\`,
	`PhpParser\`,
	`PHPStan\`,
	`Webmozart\`,
	`DeepCopy\`,
	`ComposerAutoloaderInit`, // Composer generated autoloader class
}

// DefaultExcludedFunctions are exact function names to exclude.
var DefaultExcludedFunctions = map[string]bool{
	"require":      true,
	"require_once": true,
	"include":      true,
	"include_once": true,
}

// DefaultAppPrefixes is the default list of prefixes that identify application code.
var DefaultAppPrefixes = []string{`App\`}

// DefaultVendorPath is the path fragment that identifies vendor closures.
const DefaultVendorPath = "vendor/"

// Config holds the filter configuration.
type Config struct {
	// ExcludedNamespaces are namespace prefixes to exclude (layer 2).
	ExcludedNamespaces *Trie
	// ExcludedFunctions are exact function names to exclude.
	ExcludedFunctions map[string]bool
	// AppPrefixes are namespace prefixes that identify application code (for layer 3 collapse).
	AppPrefixes *Trie
	// VendorPath is the path fragment used to detect vendor closures (e.g. "vendor/").
	VendorPath string
	// KeepInternal keeps PHP internal functions if true (disables layer 1).
	KeepInternal bool
}

// NewDefaultConfig returns a Config with the default exclusion and app prefix lists.
func NewDefaultConfig() *Config {
	return &Config{
		ExcludedNamespaces: NewTrie(DefaultExcludedNamespaces),
		ExcludedFunctions:  DefaultExcludedFunctions,
		AppPrefixes:        NewTrie(DefaultAppPrefixes),
		VendorPath:         DefaultVendorPath,
	}
}

// NewConfig builds a Config from explicit namespace lists.
func NewConfig(excluded, appPrefixes []string) *Config {
	return &Config{
		ExcludedNamespaces: NewTrie(excluded),
		ExcludedFunctions:  DefaultExcludedFunctions,
		AppPrefixes:        NewTrie(appPrefixes),
		VendorPath:         DefaultVendorPath,
	}
}

// IsAppCode returns true if the function name belongs to application code.
func (c *Config) IsAppCode(funcName string) bool {
	return c.AppPrefixes.HasPrefix(funcName)
}

// IsExcluded returns true if the function should be excluded.
// Checks: namespace prefix, exact function name, vendor closures.
func (c *Config) IsExcluded(funcName string) bool {
	// Namespace prefix match
	if c.ExcludedNamespaces.HasPrefix(funcName) {
		return true
	}

	// Exact function name match (require, include, etc.)
	if c.ExcludedFunctions[funcName] {
		return true
	}

	// Vendor closures: {closure:/app/vendor/...}
	if c.VendorPath != "" && isVendorClosure(funcName, c.VendorPath) {
		return true
	}

	return false
}

// ShouldKeep returns true if a TraceEntry passes layers 1 and 2.
func (c *Config) ShouldKeep(e parser.TraceEntry) bool {
	// Only entry lines have function info — always keep exit/return for tree building
	if !e.IsEntry {
		return true
	}

	// Layer 1: exclude PHP internal functions
	if !c.KeepInternal && !e.UserDefined {
		return false
	}

	// Layer 2: exclude by namespace, function name, vendor closure
	if c.IsExcluded(e.FunctionName) {
		return false
	}

	return true
}

// FilterEntries applies layers 1 and 2 on a slice of TraceEntry.
// It returns only the entries that pass both filters.
func (c *Config) FilterEntries(entries []parser.TraceEntry) []parser.TraceEntry {
	kept := make([]parser.TraceEntry, 0, len(entries)/4)
	for _, e := range entries {
		if c.ShouldKeep(e) {
			kept = append(kept, e)
		}
	}
	return kept
}

// CountFiltered counts how many entry lines pass layers 1 and 2 (for stats).
func (c *Config) CountFiltered(entries []parser.TraceEntry) (total, filtered int) {
	for _, e := range entries {
		if !e.IsEntry {
			continue
		}
		total++
		if c.ShouldKeep(e) {
			filtered++
		}
	}
	return total, filtered
}

// isVendorClosure checks if a function name is a closure defined in vendor code.
// XDebug format: {closure:/app/vendor/composer/autoload_real.php:37-43}
func isVendorClosure(funcName, vendorPath string) bool {
	if !strings.HasPrefix(funcName, "{closure:") {
		return false
	}
	return strings.Contains(funcName, vendorPath)
}
