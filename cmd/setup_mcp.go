package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var setupMCPCmd = &cobra.Command{
	Use:   "setup-mcp [trace-dir]",
	Short: "Configure Claude Code to use legacy-map as an MCP server",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSetupMCP,
}

func init() {
	rootCmd.AddCommand(setupMCPCmd)
}

func runSetupMCP(cmd *cobra.Command, args []string) error {
	traceDir := "./xdebug-traces"
	if len(args) > 0 {
		traceDir = args[0]
	}

	absTraceDir, err := filepath.Abs(traceDir)
	if err != nil {
		return fmt.Errorf("resolve trace dir: %w", err)
	}

	// Resolve the binary path
	absBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	absBinary, err = filepath.EvalSymlinks(absBinary)
	if err != nil {
		return fmt.Errorf("resolve binary symlink: %w", err)
	}

	// Check if claude CLI is available
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		// Claude not found — print manual config
		fmt.Fprintln(os.Stderr, "⚠ claude CLI not found in PATH")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Add this to your Claude Code MCP configuration:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stdout, `{
  "mcpServers": {
    "legacy-map": {
      "command": %q,
      "args": ["serve", %q]
    }
  }
}
`, absBinary, absTraceDir)
		return nil
	}

	// Run claude mcp add
	addCmd := exec.Command(claudePath, "mcp", "add", "legacy-map", "--", absBinary, "serve", absTraceDir)
	addCmd.Stdout = os.Stdout
	addCmd.Stderr = os.Stderr

	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("claude mcp add: %w", err)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "✓ legacy-map MCP server configured in Claude Code")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Test it:")
	fmt.Fprintln(os.Stderr, "  1. Start Claude Code: claude")
	fmt.Fprintln(os.Stderr, `  2. Ask: "Retrace moi ce qui se passe sur GET /api/your-endpoint"`)
	fmt.Fprintln(os.Stderr, "  3. Claude triggers the request, captures the trace, and explains the flow")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "💡 Add this to your project's CLAUDE.md for best results:")
	fmt.Fprint(os.Stderr, claudeMDSnippet)

	return nil
}
