package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/drkilla/legacy-map/internal/calltree"
	"github.com/drkilla/legacy-map/internal/filter"
	"github.com/drkilla/legacy-map/internal/format"
	mcpserver "github.com/drkilla/legacy-map/internal/mcp"
	"github.com/drkilla/legacy-map/internal/parser"
	"github.com/drkilla/legacy-map/internal/watcher"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "legacy-map",
	Short: "XDebug trace analyzer for PHP/Symfony applications",
}

var parseCmd = &cobra.Command{
	Use:   "parse <file.xt|file.xt.gz>",
	Short: "Parse an XDebug trace file (raw or gzipped) and output the filtered call tree as JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runParse,
}

var watchCmd = &cobra.Command{
	Use:   "watch <directory>",
	Short: "Watch a directory for new .xt/.xt.gz files and parse them automatically",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatch,
}

var serveCmd = &cobra.Command{
	Use:   "serve <directory>",
	Short: "Watch a directory and expose traces via MCP server (stdio)",
	Args:  cobra.ExactArgs(1),
	RunE:  runServe,
}

var (
	flagExcludeNS   []string
	flagAppNS       []string
	flagPathPrefix  string
	flagScenario    string
	flagBufferSize  int
	flagHTTPTimeout int
	flagPretty      bool
	flagReturns     string
	flagNoCollapse  bool
	flagPreset      string
	flagFormat      string
)

func init() {
	// Shared flags for all commands that use filtering
	for _, cmd := range []*cobra.Command{parseCmd, watchCmd, serveCmd} {
		cmd.Flags().StringSliceVar(&flagExcludeNS, "exclude-ns", nil,
			"Additional namespaces to exclude (comma-separated, e.g. Sentry\\,Jean85\\)")
		cmd.Flags().StringSliceVar(&flagAppNS, "app-ns", nil,
			"Application namespace prefixes (default: App\\)")
		cmd.Flags().StringVar(&flagPathPrefix, "path-prefix", "/app/",
			"Path prefix to strip from filenames to make them relative")
		cmd.Flags().StringVar(&flagReturns, "returns", "truncate",
			"Return value mode: truncate (default, 200 chars), type (type only), none (omit)")
		cmd.Flags().BoolVar(&flagNoCollapse, "no-collapse", false,
			"Disable collapsing of trivial leaf calls (getters, setters, hydrations)")
		cmd.Flags().StringVar(&flagPreset, "preset", "",
			"Framework preset adding excluded namespaces (symfony, laravel)")
	}

	parseCmd.Flags().StringVar(&flagScenario, "scenario", "",
		"Optional scenario label (e.g. 'Login flow')")
	parseCmd.Flags().BoolVar(&flagPretty, "pretty", false,
		"Pretty-print JSON output (default: compact)")
	parseCmd.Flags().StringVar(&flagFormat, "format", "json",
		"Output format: json (default), tree, mermaid, markdown")

	for _, cmd := range []*cobra.Command{watchCmd, serveCmd} {
		cmd.Flags().IntVar(&flagBufferSize, "buffer-size", 20,
			"Number of recent traces to keep in memory")
	}

	serveCmd.Flags().IntVar(&flagHTTPTimeout, "http-timeout", 30,
		"Default HTTP timeout in seconds for trigger_trace MCP tool")

	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(serveCmd)
}

// SetVersion sets the version string displayed by --version.
func SetVersion(v string) {
	rootCmd.Version = v
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// parseTraceFile runs the full parse+filter+build pipeline on a trace file.
func parseTraceFile(path string, cfg *filter.Config, opts *calltree.BuildOptions) (*calltree.TraceResult, error) {
	f, err := parser.OpenTraceFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var totalRaw int
	var kept []parser.TraceEntry
	keptFunctions := make(map[int]bool)
	keeper := cfg.NewStreamKeeper()

	err = parser.ParseStream(f, func(e parser.TraceEntry) error {
		if e.IsEntry {
			totalRaw++
			if keeper.Keep(e) {
				keptFunctions[e.FunctionNr] = true
				kept = append(kept, e)
			}
		} else {
			if keptFunctions[e.FunctionNr] {
				kept = append(kept, e)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	result := calltree.BuildFromFiltered(kept, cfg, totalRaw, flagPathPrefix, opts)
	result.TraceFile = path
	result.Timestamp = time.Now().Format(time.RFC3339)
	result.HTTPMethod, result.URI = watcher.DetectURIFromFilename(path)
	return result, nil
}

func runParse(cmd *cobra.Command, args []string) error {
	path := args[0]
	cfg, err := buildFilterConfig()
	if err != nil {
		return err
	}

	start := time.Now()
	result, err := parseTraceFile(path, cfg, buildOptions())
	if err != nil {
		return err
	}
	result.Scenario = flagScenario
	parseDur := time.Since(start)

	fmt.Fprintf(os.Stderr, "✓ Parsed %s in %s\n", path, parseDur.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "  Raw calls:      %d\n", result.TotalCalls)
	fmt.Fprintf(os.Stderr, "  Tree nodes:     %d\n", result.FilteredCalls)
	fmt.Fprintf(os.Stderr, "  Duration:       %.1f ms\n", result.DurationMs)
	fmt.Fprintf(os.Stderr, "  Services:       %d\n", len(result.ServicesUsed))

	if flagFormat != "json" {
		out, err := format.Render(result, flagFormat)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	if flagPretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(result)
}

func buildOptions() *calltree.BuildOptions {
	return &calltree.BuildOptions{
		ReturnsMode: flagReturns,
		Collapse:    !flagNoCollapse,
	}
}

// validateTraceDir ensures the watched directory exists before starting.
func validateTraceDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("trace directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("trace directory %s is not a directory", dir)
	}
	return nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	dir := args[0]
	if err := validateTraceDir(dir); err != nil {
		return err
	}
	cfg, err := buildFilterConfig()
	if err != nil {
		return err
	}

	w := watcher.New(watcher.Config{
		Dir:          dir,
		BufferSize:   flagBufferSize,
		Filter:       cfg,
		PathPrefix:   flagPathPrefix,
		BuildOptions: buildOptions(),
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\n🛑 Stopping watcher...\n")
		w.Stop()
	}()

	return w.Run()
}

func runServe(cmd *cobra.Command, args []string) error {
	dir := args[0]
	if err := validateTraceDir(dir); err != nil {
		return err
	}
	cfg, err := buildFilterConfig()
	if err != nil {
		return err
	}

	// MCP server defaults: returns=type for token efficiency
	serveOpts := buildOptions()
	if !cmd.Flags().Changed("returns") {
		serveOpts.ReturnsMode = "type"
	}

	w := watcher.New(watcher.Config{
		Dir:          dir,
		BufferSize:   flagBufferSize,
		Filter:       cfg,
		PathPrefix:   flagPathPrefix,
		BuildOptions: serveOpts,
	})

	log.SetOutput(os.Stderr) // Keep log output on stderr, stdio is for MCP

	// Start watcher in background — a watcher failure must be visible,
	// otherwise the MCP server keeps running without ever capturing traces
	go func() {
		if err := w.Run(); err != nil {
			log.Printf("✗ Watcher stopped: %v — traces will no longer be captured", err)
		}
	}()

	// Create and start MCP server on stdio
	mcpSrv := mcpserver.NewServer(w.Store(), flagHTTPTimeout, rootCmd.Version)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("🛑 Stopping...")
		w.Stop()
	}()

	// ServeStdio blocks until stdin is closed or error
	if err := server.ServeStdio(mcpSrv); err != nil {
		w.Stop()
		return fmt.Errorf("MCP server: %w", err)
	}

	w.Stop()
	return nil
}

func buildFilterConfig() (*filter.Config, error) {
	excluded := make([]string, 0, len(filter.DefaultExcludedNamespaces)+len(flagExcludeNS))
	excluded = append(excluded, filter.DefaultExcludedNamespaces...)

	presetExtra, err := filter.PresetExcludes(flagPreset)
	if err != nil {
		return nil, err
	}
	excluded = append(excluded, presetExtra...)
	excluded = append(excluded, flagExcludeNS...)

	appPrefixes := flagAppNS
	if len(appPrefixes) == 0 {
		if detected := filter.DetectComposerAppNamespaces("."); len(detected) > 0 {
			appPrefixes = detected
			fmt.Fprintf(os.Stderr, "✓ App namespaces from composer.json: %s\n", strings.Join(detected, ", "))
		} else {
			appPrefixes = filter.DefaultAppPrefixes
		}
	}

	return filter.NewConfig(excluded, appPrefixes), nil
}
