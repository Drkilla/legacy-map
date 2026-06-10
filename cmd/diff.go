package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/drkilla/legacy-map/internal/calltree"
	"github.com/drkilla/legacy-map/internal/format"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <a.xt> <b.xt>",
	Short: "Compare two traces and show which functions appeared, disappeared, or changed call count",
	Long: `Compare two XDebug traces at the function level.

Useful to verify that a refactor did not change the execution flow,
or to spot N+1 regressions between two versions of an endpoint.`,
	Args: cobra.ExactArgs(2),
	RunE: runDiff,
}

var flagDiffFormat string

func init() {
	diffCmd.Flags().StringSliceVar(&flagExcludeNS, "exclude-ns", nil,
		"Additional namespaces to exclude (comma-separated, e.g. Sentry\\,Jean85\\)")
	diffCmd.Flags().StringSliceVar(&flagAppNS, "app-ns", nil,
		"Application namespace prefixes (default: App\\ or composer.json autoload)")
	diffCmd.Flags().StringVar(&flagPathPrefix, "path-prefix", "/app/",
		"Path prefix to strip from filenames to make them relative")
	diffCmd.Flags().StringVar(&flagPreset, "preset", "",
		"Framework preset adding excluded namespaces (symfony, laravel)")
	diffCmd.Flags().StringVar(&flagDiffFormat, "format", "text",
		"Output format: text (default), json")

	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	cfg, err := buildFilterConfig()
	if err != nil {
		return err
	}

	// Collapse heuristics and return values would make the comparison
	// unstable — diff always works on the raw filtered tree
	opts := &calltree.BuildOptions{ReturnsMode: "none", Collapse: false}

	a, err := parseTraceFile(args[0], cfg, opts)
	if err != nil {
		return err
	}
	b, err := parseTraceFile(args[1], cfg, opts)
	if err != nil {
		return err
	}

	d := calltree.Diff(a, b)

	switch flagDiffFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	case "text":
		fmt.Print(format.DiffText(d))
		return nil
	default:
		return fmt.Errorf("unknown format %q (available: text, json)", flagDiffFormat)
	}
}
