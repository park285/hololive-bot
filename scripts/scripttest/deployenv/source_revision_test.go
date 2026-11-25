package deployenv

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const sourceRevision = `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/source-revision.sh"
`

func newGitFixture(t *testing.T) (dir, head string) {
	t.Helper()

	dir = t.TempDir()
	harness.Write(t, filepath.Join(dir, "tracked"), "clean\n")

	r := harness.Bash(t, harness.Cmd{Dir: dir, Script: `git init -q
git add tracked
git -c user.name=fixture -c user.email=fixture@example.invalid commit -qm initial
git rev-parse HEAD`})
	harness.Succeeds(t, "git fixture", r)

	return dir, strings.TrimSpace(r.Stdout)
}

func TestDeploySourceRevision(t *testing.T) {
	t.Parallel()

	t.Run("clean checkout resolves full HEAD", func(t *testing.T) {
		t.Parallel()

		dir, head := newGitFixture(t)
		r := harness.Bash(t, harness.Cmd{Script: sourceRevision + `deploy_source_revision "$1"`, Args: []string{dir}})
		harness.Succeeds(t, "clean checkout", r)

		if got := strings.TrimSpace(r.Stdout); got != head {
			t.Errorf("deploy_source_revision = %q, want %q", got, head)
		}
	})

	for name, file := range map[string][2]string{
		"tracked change":   {"tracked", "dirty\n"},
		"untracked change": {"untracked", "untracked\n"},
	} {
		t.Run(name+" fails closed", func(t *testing.T) {
			t.Parallel()

			dir, _ := newGitFixture(t)
			harness.Write(t, filepath.Join(dir, file[0]), file[1])
			harness.Fails(t, name, harness.Bash(t, harness.Cmd{Script: sourceRevision + `deploy_source_revision "$1"`, Args: []string{dir}}))
		})
	}
}

func TestDeployVerifyObjectRevision(t *testing.T) {
	t.Parallel()

	const revision = "0123456789abcdef0123456789abcdef01234567"

	cases := []struct {
		name, label, expected string
		ok                    bool
	}{
		{name: "exact image revision", label: revision, expected: revision, ok: true},
		{name: "unknown expected revision", label: "unknown", expected: "unknown"},
		{name: "missing image revision", label: "", expected: revision},
		{name: "mismatched image revision", label: strings.Repeat("0", 40), expected: revision},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			podman := filepath.Join(t.TempDir(), "podman")
			harness.Executable(t, podman, "printf '%s\\n' \"${FAKE_REVISION_LABEL:-}\"\n")

			r := harness.Bash(t, harness.Cmd{
				Script: sourceRevision + `deploy_verify_object_revision "$1" image image:fixture "$2"`,
				Args:   []string{podman, tc.expected},
				Env:    []string{"FAKE_REVISION_LABEL=" + tc.label},
			})
			if tc.ok {
				harness.Succeeds(t, tc.name, r)
			} else {
				harness.Fails(t, tc.name, r)
			}
		})
	}
}
