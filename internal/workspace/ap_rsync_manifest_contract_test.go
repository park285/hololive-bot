package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPRsyncManifestCheckFailsWithoutGoToolchain(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)
	manifest := filepath.Join(t.TempDir(), "manifest.txt")

	if err := os.WriteFile(manifest, nil, 0o600); err != nil {
		t.Fatalf("write temporary manifest: %v", err)
	}

	const apRsyncManifestCheckCommand = `exec ./scripts/deploy/check-ap-rsync-manifest.sh "$AP_RSYNC_MANIFEST"`

	cmd := exec.CommandContext(t.Context(), "bash", "-c", apRsyncManifestCheckCommand)

	cmd.Dir = root

	cmd.Env = append(
		os.Environ(),
		"AP_RSYNC_MANIFEST="+manifest,
		"GO_CMD=hololive-missing-go-command",
	)

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("manifest check succeeded without Go toolchain:\n%s", output)
	}

	if !strings.Contains(string(output), "[FAIL] required Go command not found") {
		t.Fatalf("manifest check output = %q, want fail-closed toolchain error", output)
	}

	if strings.Contains(strings.ToLower(string(output)), "skipping") {
		t.Fatalf("manifest check retained fail-open skip output: %q", output)
	}
}

func TestAPRsyncManifestCheckUsesConfiguredGoCommand(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)
	script := readRepoFile(t, root, "scripts/deploy/check-ap-rsync-manifest.sh")

	for _, required := range []string{
		`GO_CMD="${GO_CMD:-go}"`,
		`command -v "$GO_CMD"`,
		`"$GO_CMD" list -deps`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("AP rsync manifest check missing %q", required)
		}
	}
}
