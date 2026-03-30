package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectDocker(t *testing.T) {
	t.Run("forced", func(t *testing.T) {
		isDocker, file := detectDocker(true)
		if !isDocker {
			t.Fatal("expected docker=true when forced")
		}
		if file != "" {
			t.Fatalf("expected empty file when forced, got %q", file)
		}
	})

	t.Run("docker-compose.yml present", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("docker-compose.yml", []byte("version: '3'\n"), 0644)

		isDocker, file := detectDocker(false)
		if !isDocker {
			t.Fatal("expected docker=true")
		}
		if file != "docker-compose.yml" {
			t.Fatalf("expected docker-compose.yml, got %q", file)
		}
	})

	t.Run("compose.yaml present", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("compose.yaml", []byte("services:\n"), 0644)

		isDocker, file := detectDocker(false)
		if !isDocker {
			t.Fatal("expected docker=true")
		}
		if file != "compose.yaml" {
			t.Fatalf("expected compose.yaml, got %q", file)
		}
	})

	t.Run("no compose file", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		isDocker, file := detectDocker(false)
		if isDocker {
			t.Fatal("expected docker=false")
		}
		if file != "" {
			t.Fatalf("expected empty file, got %q", file)
		}
	})
}

func TestAppendGitignore(t *testing.T) {
	t.Run("no gitignore", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		err := appendGitignore()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not create .gitignore
		if _, err := os.Stat(".gitignore"); err == nil {
			t.Fatal(".gitignore should not be created")
		}
	})

	t.Run("gitignore exists without entry", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile(".gitignore", []byte("vendor/\n"), 0644)

		err := appendGitignore()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(".gitignore")
		if !strings.Contains(string(data), "xdebug-traces/*.xt") {
			t.Fatal("expected entry to be appended")
		}
	})

	t.Run("gitignore already contains entry", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile(".gitignore", []byte("vendor/\nxdebug-traces/*.xt\n"), 0644)

		err := appendGitignore()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(".gitignore")
		count := strings.Count(string(data), "xdebug-traces/*.xt")
		if count != 1 {
			t.Fatalf("expected exactly 1 entry, got %d", count)
		}
	})

	t.Run("gitignore without trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile(".gitignore", []byte("vendor/"), 0644)

		err := appendGitignore()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(".gitignore")
		content := string(data)
		if !strings.Contains(content, "vendor/\n") {
			t.Fatal("expected original content preserved with newline")
		}
		if !strings.Contains(content, "xdebug-traces/*.xt") {
			t.Fatal("expected entry to be appended")
		}
	})
}

func TestPrintXdebugMissing(t *testing.T) {
	// Capture stderr by redirecting to a pipe
	t.Run("docker mode", func(t *testing.T) {
		r, w, _ := os.Pipe()
		oldStderr := os.Stderr
		os.Stderr = w
		defer func() { os.Stderr = oldStderr }()

		printXdebugMissing(true, "php-custom")
		w.Close()

		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		output := string(buf[:n])

		if !strings.Contains(output, `"php-custom"`) {
			t.Fatal("expected service name in output")
		}
		if !strings.Contains(output, "pecl install xdebug") {
			t.Fatal("expected pecl install instruction")
		}
		if !strings.Contains(output, "docker-php-ext-enable") {
			t.Fatal("expected docker-php-ext-enable instruction")
		}
		if !strings.Contains(output, "docker compose build") {
			t.Fatal("expected rebuild instruction")
		}
	})

	t.Run("local mode", func(t *testing.T) {
		r, w, _ := os.Pipe()
		oldStderr := os.Stderr
		os.Stderr = w
		defer func() { os.Stderr = oldStderr }()

		printXdebugMissing(false, "")
		w.Close()

		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		output := string(buf[:n])

		if !strings.Contains(output, "XDebug is NOT installed") {
			t.Fatal("expected missing message")
		}
		// Should contain an install command appropriate for the OS
		if runtime.GOOS == "linux" {
			if _, err := os.Stat("/etc/debian_version"); err == nil {
				if !strings.Contains(output, "apt-get") {
					t.Fatal("expected apt-get on Debian/Ubuntu")
				}
			}
		}
		if !strings.Contains(output, "legacy-map init") {
			t.Fatal("expected re-run instruction")
		}
	})
}

func TestRunInitSkipCheck(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create .gitignore so appendGitignore works
	os.WriteFile(".gitignore", []byte(""), 0644)

	flagInitDocker = false
	flagInitTraceDir = "/tmp/xdebug-traces"
	flagInitService = ""
	flagInitSkipCheck = true

	err := runInit(initCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify files were created
	if _, err := os.Stat("xdebug-trace.ini"); err != nil {
		t.Fatal("xdebug-trace.ini not created")
	}
	if _, err := os.Stat(filepath.Join("xdebug-traces")); err != nil {
		t.Fatal("xdebug-traces dir not created")
	}

	// Verify ini content
	data, _ := os.ReadFile("xdebug-trace.ini")
	content := string(data)
	if !strings.Contains(content, "xdebug.mode=trace") {
		t.Fatal("expected xdebug.mode=trace in ini")
	}
	if !strings.Contains(content, "xdebug.trace_format=1") {
		t.Fatal("expected trace_format=1 in ini")
	}
}
