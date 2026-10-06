package youtubejs

import (
	"os"
	"path/filepath"
	"testing"
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
