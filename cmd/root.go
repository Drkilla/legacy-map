package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drkilla/legacy-map/internal/calltree"
	"github.com/drkilla/legacy-map/internal/filter"
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
	Use:   "parse <file.xt>",
	Short: "Parse an XDebug trace file and output the filtered call tree as JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runParse,
}

var watchCmd = &cobra.Command{
	Use:   "watch <directory>",
	Short: "Watch a directory for new .xt files and parse them automatically",
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
	}

	parseCmd.Flags().StringVar(&flagScenario, "scenario", "",
		"Optional scenario label (e.g. 'Login flow')")
	parseCmd.Flags().BoolVar(&flagPretty, "pretty", false,
		"Pretty-print JSON output (default: compact)")

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

func runParse(cmd *cobra.Command, args []string) error {
	path := args[0]
	cfg := buildFilterConfig()

	start := time.Now()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var totalRaw int
	var kept []parser.TraceEntry
	keptFunctions := make(map[int]bool)

	err = parser.ParseStream(f, func(e parser.TraceEntry) error {
		if e.IsEntry {
			totalRaw++
			if cfg.ShouldKeep(e) {
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
		return fmt.Errorf("parse: %w", err)
	}
	parseDur := time.Since(start)

	opts := &calltree.BuildOptions{
		ReturnsMode: flagReturns,
		Collapse:    !flagNoCollapse,
	}
	result := calltree.BuildFromFiltered(kept, cfg, totalRaw, flagPathPrefix, opts)
	result.TraceFile = path
	result.Timestamp = time.Now().Format(time.RFC3339)
	result.Scenario = flagScenario
	result.HTTPMethod, result.URI = watcher.DetectURIFromFilename(path)

	fmt.Fprintf(os.Stderr, "✓ Parsed %s in %s\n", path, parseDur.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "  Raw calls:      %d\n", result.TotalCalls)
	fmt.Fprintf(os.Stderr, "  Tree nodes:     %d\n", result.FilteredCalls)
	fmt.Fprintf(os.Stderr, "  Duration:       %.1f ms\n", result.DurationMs)
	fmt.Fprintf(os.Stderr, "  Services:       %d\n", len(result.ServicesUsed))

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

func runWatch(cmd *cobra.Command, args []string) error {
	dir := args[0]
	cfg := buildFilterConfig()

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
	cfg := buildFilterConfig()

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

	// Start watcher in background
	watchErr := make(chan error, 1)
	go func() {
		watchErr <- w.Run()
	}()

	// Create and start MCP server on stdio
	mcpSrv := mcpserver.NewServer(w.Store(), flagHTTPTimeout)

	log.SetOutput(os.Stderr) // Keep log output on stderr, stdio is for MCP

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

func buildFilterConfig() *filter.Config {
	excluded := filter.DefaultExcludedNamespaces
	if len(flagExcludeNS) > 0 {
		excluded = append(excluded, flagExcludeNS...)
	}

	appPrefixes := filter.DefaultAppPrefixes
	if len(flagAppNS) > 0 {
		appPrefixes = flagAppNS
	}

	return filter.NewConfig(excluded, appPrefixes)
}
