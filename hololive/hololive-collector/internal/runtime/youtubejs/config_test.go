package youtubejs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanonicalHelperFileAcceptsOnlyCleanAbsoluteRegularFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "server.mjs")

	if err := os.WriteFile(file, []byte("export {};\n"), 0o600); err != nil {
		t.Fatalf("write helper file: %v", err)
	}

	link := filepath.Join(dir, "server-link.mjs")
	if err := os.Symlink(file, link); err != nil {
		t.Fatalf("create helper symlink: %v", err)
	}

	resolvedFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatalf("resolve helper file: %v", err)
	}

	for _, accepted := range []string{file, link} {
		got, err := canonicalHelperFile(accepted, "helper script")
		if err != nil {
			t.Fatalf("canonicalHelperFile(%q) error: %v", accepted, err)
		}

		if got != resolvedFile {
			t.Fatalf("canonicalHelperFile(%q) = %q, want resolved regular file %q", accepted, got, resolvedFile)
		}
	}

	for name, rejected := range map[string]string{
		"relative":  "server.mjs",
		"unclean":   dir + "/../" + filepath.Base(dir) + "/server.mjs",
		"directory": dir,
		"missing":   filepath.Join(dir, "missing.mjs"),
	} {
		if got, err := canonicalHelperFile(rejected, "helper script"); err == nil {
			t.Fatalf("%s path %q must be rejected, got %q", name, rejected, got)
		}
	}
}

func TestResolveConfigRejectsMissingTimeoutsAndBodyLimits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	node := filepath.Join(dir, "node")
	script := filepath.Join(dir, "server.mjs")

	for _, path := range []string{node, script} {
		if err := os.WriteFile(path, []byte("export {};\n"), 0o600); err != nil {
			t.Fatalf("write helper file: %v", err)
		}
	}

	valid := Config{
		NodePath:          node,
		ScriptPath:        script,
		RuntimeBaseDir:    dir,
		StartupTimeout:    time.Second,
		RequestTimeout:    time.Second,
		HealthTimeout:     time.Second,
		ShutdownTimeout:   time.Second,
		RequestBodyLimit:  DefaultRequestBodyLimit,
		ResponseBodyLimit: DefaultResponseBodyLimit,
		MaxInflight:       1,
	}
	if _, err := resolveConfig(&valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	for name, mutate := range map[string]func(*Config){
		"startup timeout":     func(cfg *Config) { cfg.StartupTimeout = 0 },
		"request timeout":     func(cfg *Config) { cfg.RequestTimeout = 0 },
		"sub-ms request":      func(cfg *Config) { cfg.RequestTimeout = time.Microsecond },
		"health timeout":      func(cfg *Config) { cfg.HealthTimeout = 0 },
		"shutdown timeout":    func(cfg *Config) { cfg.ShutdownTimeout = 0 },
		"request body limit":  func(cfg *Config) { cfg.RequestBodyLimit = 0 },
		"response body limit": func(cfg *Config) { cfg.ResponseBodyLimit = 0 },
	} {
		cfg := valid
		mutate(&cfg)

		if _, err := resolveConfig(&cfg); err == nil {
			t.Fatalf("%s: missing value was replaced by a default", name)
		}
	}
}
