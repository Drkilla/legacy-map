package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Setup XDebug tracing for a PHP/Symfony project",
	RunE:  runInit,
}

var (
	flagInitDocker   bool
	flagInitTraceDir string
)

func init() {
	initCmd.Flags().BoolVar(&flagInitDocker, "docker", false,
		"Force Docker mode (auto-detected by default)")
	initCmd.Flags().StringVar(&flagInitTraceDir, "trace-dir", "/tmp/xdebug-traces",
		"XDebug trace output directory inside PHP environment")

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
`

func runInit(cmd *cobra.Command, args []string) error {
	isDocker := flagInitDocker
	traceDir := flagInitTraceDir

	// Detect Docker environment
	if !isDocker {
		for _, f := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yaml", "compose.yml"} {
			if _, err := os.Stat(f); err == nil {
				isDocker = true
				fmt.Fprintf(os.Stderr, "✓ Docker environment detected (%s)\n", f)
				break
			}
		}
		if !isDocker {
			fmt.Fprintln(os.Stderr, "ℹ No Docker Compose file detected — assuming local PHP")
		}
	} else {
		fmt.Fprintln(os.Stderr, "✓ Docker mode (forced via --docker)")
	}

	// Write xdebug-trace.ini
	iniContent := fmt.Sprintf(xdebugINITemplate, traceDir)
	if err := os.WriteFile("xdebug-trace.ini", []byte(iniContent), 0644); err != nil {
		return fmt.Errorf("write xdebug-trace.ini: %w", err)
	}
	fmt.Fprintln(os.Stderr, "✓ XDebug trace config written to ./xdebug-trace.ini")

	// Create local trace directory
	if err := os.MkdirAll("xdebug-traces", 0755); err != nil {
		return fmt.Errorf("create trace directory: %w", err)
	}
	fmt.Fprintln(os.Stderr, "✓ Trace directory created: ./xdebug-traces")

	// Append to .gitignore if it exists
	if err := appendGitignore(); err != nil {
		return err
	}

	// Print next steps
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Next steps:")

	if isDocker {
		fmt.Fprintln(os.Stderr, "  1. Copy xdebug config into your PHP container:")
		fmt.Fprintln(os.Stderr, "     Add to Dockerfile:  COPY xdebug-trace.ini /usr/local/etc/php/conf.d/99-xdebug-trace.ini")
		fmt.Fprintln(os.Stderr, "     Or mount:           docker compose exec php cp /app/xdebug-trace.ini /usr/local/etc/php/conf.d/")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  2. Restart PHP:")
		fmt.Fprintln(os.Stderr, "     docker compose restart php")
	} else {
		fmt.Fprintln(os.Stderr, "  1. Copy xdebug config to your PHP conf.d:")
		fmt.Fprintln(os.Stderr, `     cp xdebug-trace.ini $(php -i | grep "Scan this dir" | cut -d'>' -f2 | tr -d ' ')/99-xdebug-trace.ini`)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  2. Restart PHP:")
		fmt.Fprintln(os.Stderr, "     sudo systemctl restart php-fpm  # or: sudo service php-fpm restart")
	}

	absTraceDir, _ := filepath.Abs("xdebug-traces")

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  3. Start capturing:")
	fmt.Fprintf(os.Stderr, "     legacy-map serve %s\n", absTraceDir)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  4. Connect Claude Code:")
	fmt.Fprintf(os.Stderr, "     claude mcp add legacy-map -- legacy-map serve %s\n", absTraceDir)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  5. Trigger traces by adding ?XDEBUG_TRACE=1 to any URL")
	fmt.Fprintln(os.Stderr, "     or set xdebug.start_with_request=yes in the ini to capture everything")

	return nil
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
