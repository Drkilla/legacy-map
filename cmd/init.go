package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Setup XDebug tracing for a PHP/Symfony project",
	RunE:  runInit,
}

var (
	flagInitDocker    bool
	flagInitTraceDir  string
	flagInitService   string
	flagInitSkipCheck bool
)

func init() {
	initCmd.Flags().BoolVar(&flagInitDocker, "docker", false,
		"Force Docker mode (auto-detected by default)")
	initCmd.Flags().StringVar(&flagInitTraceDir, "trace-dir", "/tmp/xdebug-traces",
		"XDebug trace output directory inside PHP environment")
	initCmd.Flags().StringVar(&flagInitService, "service", "",
		"Docker Compose service name for PHP (auto-detected by default)")
	initCmd.Flags().BoolVar(&flagInitSkipCheck, "skip-check", false,
		"Skip XDebug installation check")

	rootCmd.AddCommand(initCmd)
}

const xdebugINITemplate = `; legacy-map XDebug trace configuration
; Copy this file to your PHP conf.d directory
; Docker: COPY xdebug-trace.ini /usr/local/etc/php/conf.d/99-xdebug-trace.ini
; Local: cp xdebug-trace.ini $(php -i | grep "Scan this dir" | cut -d'>' -f2 | tr -d ' ')/99-xdebug-trace.ini

xdebug.mode=trace
xdebug.trace_format=1
xdebug.trace_output_dir=%s
xdebug.trace_output_name=trace.%%t.%%R
xdebug.start_with_request=trigger
xdebug.collect_return=1
xdebug.collect_assignments=0
; Disable compression — legacy-map reads raw .xt files, not .xt.gz
xdebug.use_compression=0
`

const claudeMDSnippet = `
## Tracing & Flow Analysis

This project has the legacy-map MCP server connected.
For any question about "what happens when...", "trace the flow of...",
or any execution flow analysis: use the legacy-map MCP tools
(trigger_trace, list_traces, get_last_trace, get_trace_by_uri)
INSTEAD OF reading source code statically.

trigger_trace makes a real HTTP request with XDebug tracing enabled
and returns the filtered application-level call tree.
`

func runInit(cmd *cobra.Command, args []string) error {
	traceDir := flagInitTraceDir

	// --- Phase 1: Detect environment ---
	fmt.Fprintln(os.Stderr, "🔍 Detecting environment...")

	isDocker, composeFile := detectDocker(flagInitDocker)
	if isDocker && composeFile != "" {
		fmt.Fprintf(os.Stderr, "✓ Docker environment detected (%s)\n", composeFile)
	} else if isDocker {
		fmt.Fprintln(os.Stderr, "✓ Docker mode (forced via --docker)")
	} else {
		fmt.Fprintln(os.Stderr, "ℹ No Docker Compose file detected — assuming local PHP")
	}

	// --- Phase 2: Detect PHP service (Docker) ---
	dockerService := flagInitService
	if isDocker && dockerService == "" {
		dockerService = detectPHPService()
		if dockerService != "" {
			fmt.Fprintf(os.Stderr, "✓ PHP service: %s\n", dockerService)
		} else {
			fmt.Fprintln(os.Stderr, "⚠ Could not detect PHP service name (use --service=NAME)")
		}
	} else if isDocker && dockerService != "" {
		fmt.Fprintf(os.Stderr, "✓ PHP service: %s (via --service)\n", dockerService)
	}

	// --- Phase 3: Check XDebug ---
	if !flagInitSkipCheck {
		xdebugVersion, status := checkXdebug(isDocker, dockerService)
		switch status {
		case xdebugInstalled:
			if xdebugVersion != "" {
				fmt.Fprintf(os.Stderr, "✓ XDebug %s installed\n", xdebugVersion)
			} else {
				fmt.Fprintln(os.Stderr, "✓ XDebug installed")
			}
		case xdebugMissing:
			printXdebugMissing(isDocker, dockerService)
			return nil // stop here, user needs to install first
		case xdebugCheckFailed:
			fmt.Fprintln(os.Stderr, "⚠ Could not check XDebug (PHP not accessible). Use --skip-check to continue anyway.")
		}
	} else {
		fmt.Fprintln(os.Stderr, "⏭ Skipping XDebug check (--skip-check)")
	}

	// --- Phase 4: Write config ---
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "📝 Writing configuration...")

	iniContent := fmt.Sprintf(xdebugINITemplate, traceDir)
	if err := os.WriteFile("xdebug-trace.ini", []byte(iniContent), 0644); err != nil {
		return fmt.Errorf("write xdebug-trace.ini: %w", err)
	}
	fmt.Fprintln(os.Stderr, "✓ XDebug trace config written to ./xdebug-trace.ini")

	// --- Phase 5: Create trace directory ---
	if err := os.MkdirAll("xdebug-traces", 0755); err != nil {
		return fmt.Errorf("create trace directory: %w", err)
	}
	fmt.Fprintln(os.Stderr, "✓ Trace directory created: ./xdebug-traces")

	// --- Phase 6: Update .gitignore ---
	if err := appendGitignore(); err != nil {
		return err
	}

	// --- Phase 7: Next steps ---
	absTraceDir, _ := filepath.Abs("xdebug-traces")
	svc := dockerService
	if svc == "" {
		svc = "php"
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "📋 Next steps:")
	fmt.Fprintln(os.Stderr)

	if isDocker {
		fmt.Fprintln(os.Stderr, "  1. Copy XDebug config into your container:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "     # Option A: Add to Dockerfile")
		fmt.Fprintln(os.Stderr, "     COPY xdebug-trace.ini /usr/local/etc/php/conf.d/99-xdebug-trace.ini")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "     # Option B: Mount as volume in docker-compose.yml")
		fmt.Fprintln(os.Stderr, "     volumes:")
		fmt.Fprintln(os.Stderr, "       - ./xdebug-trace.ini:/usr/local/etc/php/conf.d/99-xdebug-trace.ini")
		fmt.Fprintf(os.Stderr, "       - ./xdebug-traces:%s\n", traceDir)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "  2. Restart: docker compose restart %s\n", svc)
	} else {
		fmt.Fprintln(os.Stderr, "  1. Copy XDebug config:")
		fmt.Fprintln(os.Stderr, `     sudo cp xdebug-trace.ini $(php -i | grep "Scan this dir" | cut -d'>' -f2 | tr -d ' ')/99-xdebug-trace.ini`)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  2. Restart PHP:")
		fmt.Fprintln(os.Stderr, "     sudo systemctl restart php-fpm")
		fmt.Fprintln(os.Stderr, "     # ou: symfony server:stop && symfony serve -d")
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  3. Start legacy-map:")
	fmt.Fprintf(os.Stderr, "     legacy-map serve %s\n", absTraceDir)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  4. Connect Claude Code:")
	fmt.Fprintf(os.Stderr, "     legacy-map setup-mcp %s\n", absTraceDir)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  5. Trigger traces:")
	fmt.Fprintln(os.Stderr, `     curl "http://localhost:8000/your-route?XDEBUG_TRACE=1"`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "💡 Add this to your project's CLAUDE.md for best results:")
	fmt.Fprint(os.Stderr, claudeMDSnippet)

	return nil
}

type xdebugStatus int

const (
	xdebugInstalled   xdebugStatus = iota
	xdebugMissing
	xdebugCheckFailed
)

func detectDocker(forced bool) (bool, string) {
	if forced {
		return true, ""
	}
	for _, f := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yaml", "compose.yml"} {
		if _, err := os.Stat(f); err == nil {
			return true, f
		}
	}
	return false, ""
}

func detectPHPService() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "--services")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	services := strings.Split(strings.TrimSpace(string(output)), "\n")
	candidates := []string{"php", "app", "php-fpm", "fpm", "web", "api"}

	for _, candidate := range candidates {
		for _, svc := range services {
			svc = strings.TrimSpace(svc)
			if strings.Contains(strings.ToLower(svc), candidate) {
				return svc
			}
		}
	}
	return ""
}

func checkXdebug(isDocker bool, dockerService string) (version string, status xdebugStatus) {
	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if isDocker {
		if dockerService == "" {
			return "", xdebugCheckFailed
		}
		cmd = exec.CommandContext(ctx, "docker", "compose", "exec", "-T", dockerService, "php", "-m")
	} else {
		cmd = exec.CommandContext(ctx, "php", "-m")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", xdebugCheckFailed
	}

	outStr := string(output)
	if !strings.Contains(strings.ToLower(outStr), "xdebug") {
		return "", xdebugMissing
	}

	// Try to get version
	ver := extractXdebugVersion(isDocker, dockerService)
	return ver, xdebugInstalled
}

func extractXdebugVersion(isDocker bool, dockerService string) string {
	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if isDocker {
		cmd = exec.CommandContext(ctx, "docker", "compose", "exec", "-T", dockerService, "php", "-r", "echo phpversion('xdebug');")
	} else {
		cmd = exec.CommandContext(ctx, "php", "-r", "echo phpversion('xdebug');")
	}

	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	ver := strings.TrimSpace(string(output))
	if matched, _ := regexp.MatchString(`^\d+\.\d+`, ver); matched {
		return ver
	}
	return ""
}

func printXdebugMissing(isDocker bool, dockerService string) {
	svc := dockerService
	if svc == "" {
		svc = "php"
	}

	fmt.Fprintln(os.Stderr)

	if isDocker {
		fmt.Fprintf(os.Stderr, "✗ XDebug is NOT installed in container %q\n", svc)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "📦 Install XDebug first:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  Add to your Dockerfile:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "    RUN pecl install xdebug && docker-php-ext-enable xdebug")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  If using Alpine:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "    RUN apk add --no-cache $PHPIZE_DEPS \\")
		fmt.Fprintln(os.Stderr, "        && pecl install xdebug \\")
		fmt.Fprintln(os.Stderr, "        && docker-php-ext-enable xdebug")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  Then rebuild:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "    docker compose build && docker compose up -d")
	} else {
		fmt.Fprintln(os.Stderr, "✗ XDebug is NOT installed")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "📦 Install XDebug first:")
		fmt.Fprintln(os.Stderr)

		switch runtime.GOOS {
		case "linux":
			if _, err := os.Stat("/etc/debian_version"); err == nil {
				fmt.Fprintln(os.Stderr, "    sudo apt-get install php-xdebug")
			} else if _, err := os.Stat("/etc/redhat-release"); err == nil {
				fmt.Fprintln(os.Stderr, "    sudo yum install php-xdebug")
			} else {
				fmt.Fprintln(os.Stderr, "    pecl install xdebug")
			}
		case "darwin":
			fmt.Fprintln(os.Stderr, "    pecl install xdebug")
		default:
			fmt.Fprintln(os.Stderr, "    pecl install xdebug")
		}
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Then re-run:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    legacy-map init")
}

func appendGitignore() error {
	const entry = "xdebug-traces/*.xt"

	data, err := os.ReadFile(".gitignore")
	if os.IsNotExist(err) {
		return nil // no .gitignore, skip
	}
	if err != nil {
		return fmt.Errorf("read .gitignore: %w", err)
	}

	// Check if already present
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			fmt.Fprintln(os.Stderr, "✓ .gitignore already contains "+entry)
			return nil
		}
	}

	f, err := os.OpenFile(".gitignore", os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	defer f.Close()

	// Ensure we start on a new line
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Fprintln(f)
	}
	fmt.Fprintln(f, entry)
	fmt.Fprintln(os.Stderr, "✓ Added "+entry+" to .gitignore")

	return nil
}
