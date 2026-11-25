package buildscripts

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const fakePodman = `set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_PODMAN_LOG"
case "$1" in
  build)
    tag='' revision=''
    while (($#)); do
      case "$1" in
        --tag) tag="$2"; shift ;;
        --label) [[ "$2" != org.opencontainers.image.revision=* ]] || revision="${2#*=}"; shift ;;
        --build-arg) shift ;;
      esac
      shift
    done
    printf '%s\n' "${FAKE_LABEL:-$revision}" > "$FAKE_PODMAN_STATE/${tag//[\/:]/_}"
    ;;
  image)
    [[ "$2" == inspect ]] || exit 2
    ref="${*: -1}"
    cat "$FAKE_PODMAN_STATE/${ref//[\/:]/_}"
    ;;
  info) printf 'amd64\n' ;;
  *) exit 2 ;;
esac
`

type buildFixture struct {
	repo, bin, log, state, head string
}

func newBuildFixture(t *testing.T) buildFixture {
	t.Helper()

	tmp := t.TempDir()
	f := buildFixture{
		repo:  filepath.Join(tmp, "repo"),
		bin:   filepath.Join(tmp, "bin"),
		log:   filepath.Join(tmp, "podman.log"),
		state: filepath.Join(tmp, "images"),
	}

	harness.Executable(t, filepath.Join(f.repo, "build-all.sh"), strings.TrimPrefix(harness.Read(t, harness.Path(t, "build-all.sh")), "#!/usr/bin/env bash\n"))
	harness.Write(t, filepath.Join(f.repo, "scripts", "deploy", "lib", "source-revision.sh"), harness.Read(t, harness.Path(t, "scripts/deploy/lib/source-revision.sh")))
	harness.Write(t, filepath.Join(f.repo, "hololive", "hololive-api", "VERSION"), "1.2.3\n")
	harness.Write(t, filepath.Join(f.repo, "hololive", "hololive-alarm-worker", "VERSION"), "1.2.4\n")
	harness.Write(t, filepath.Join(f.state, ".keep"), "")
	harness.Executable(t, filepath.Join(f.bin, "podman"), fakePodman)

	r := harness.Bash(t, harness.Cmd{Dir: f.repo, Script: `git init -q
git add -A
git -c user.name=fixture -c user.email=fixture@example.invalid commit -qm fixture
git rev-parse HEAD`})
	harness.Succeeds(t, "git fixture", r)

	f.head = strings.TrimSpace(r.Stdout)

	return f
}

func (f buildFixture) build(t *testing.T, label string, targets ...string) (harness.Result, []string) {
	t.Helper()

	harness.Write(t, f.log, "")

	env := []string{"FAKE_PODMAN_LOG=" + f.log, "FAKE_PODMAN_STATE=" + f.state}

	if label != "" {
		env = append(env, "FAKE_LABEL="+label)
	}

	r := harness.Bash(t, harness.Cmd{
		Dir:    f.repo,
		Script: `exec ./build-all.sh --no-bump --skip-local-ci "$@"`,
		Args:   targets,
		Env:    env,
		Bin:    f.bin,
	})

	calls := strings.Split(strings.TrimRight(harness.Read(t, f.log), "\n"), "\n")
	if len(calls) == 1 && calls[0] == "" {
		calls = nil
	}

	return r, calls
}

func withPrefix(calls []string, prefix string) []string {
	var out []string

	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}

	return out
}

func TestCollectorBuildPairsIssuerAndVerifiesRevisions(t *testing.T) {
	t.Parallel()

	f := newBuildFixture(t)
	r, calls := f.build(t, "", "collector")
	harness.Succeeds(t, "collector build", r)

	builds := withPrefix(calls, "build ")
	if len(builds) != 2 {
		t.Fatalf("collector build ran %d podman builds, want 2 (collector and PO issuer): %q", len(builds), builds)
	}

	for _, image := range []string{"localhost/hololive-collector:prod", "localhost/hololive-youtube-po-sandbox:prod"} {
		var line string

		for _, b := range builds {
			if strings.Contains(b, "--tag "+image+" ") {
				line = b
			}
		}

		for _, want := range []string{"--format docker ", "--build-arg REVISION=" + f.head + " ", "--build-arg VERSION=1.2.3 "} {
			if !strings.Contains(line, want) {
				t.Errorf("build of %s lacks %q: %q", image, want, line)
			}
		}

		inspect := fmt.Sprintf("image inspect --format {{index .Config.Labels %q}} %s", "org.opencontainers.image.revision", image)
		if !strings.Contains("\n"+strings.Join(calls, "\n")+"\n", "\n"+inspect+"\n") {
			t.Errorf("revision label of %s was not verified; calls: %q", image, calls)
		}
	}
}

func TestBuildRejectsMismatchedRevisionLabel(t *testing.T) {
	t.Parallel()

	f := newBuildFixture(t)
	r, calls := f.build(t, strings.Repeat("0", 40), "hololive-alarm-worker")
	harness.Fails(t, "label mismatch", r)

	if !strings.Contains(r.Stdout+r.Stderr, "revision mismatch") {
		t.Errorf("label mismatch was not reported: %s", r)
	}

	if !strings.Contains(strings.Join(withPrefix(calls, "build "), "\n"), "--build-arg VERSION=1.2.4 ") {
		t.Errorf("alarm-worker did not build with its own VERSION: %q", calls)
	}
}

func TestDirtyWorktreeBuildsWithUnknownRevision(t *testing.T) {
	t.Parallel()

	f := newBuildFixture(t)
	harness.Write(t, filepath.Join(f.repo, "untracked"), "")

	r, calls := f.build(t, "", "hololive-api")
	harness.Succeeds(t, "dirty build", r)

	if !strings.Contains(strings.Join(withPrefix(calls, "build "), "\n"), "--build-arg REVISION=unknown ") {
		t.Errorf("dirty worktree claimed a source revision: %q", calls)
	}

	if inspects := withPrefix(calls, "image inspect"); len(inspects) != 0 {
		t.Errorf("dirty build verified an unknown revision: %q", inspects)
	}
}

func TestBuildRefusesBeforeInvokingPodman(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version *string
		targets []string
	}{
		{name: "no targets"},
		{name: "unknown target", targets: []string{"alarm-worker"}},
		{name: "empty runtime VERSION", version: new(""), targets: []string{"hololive-api"}},
		{name: "non-numeric runtime VERSION", version: new("dev"), targets: []string{"hololive-api"}},
		{name: "two-part runtime VERSION", version: new("1.2"), targets: []string{"hololive-api"}},
		{name: "missing runtime VERSION", version: new("-"), targets: []string{"hololive-api"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newBuildFixture(t)

			if tc.version != nil {
				path := filepath.Join(f.repo, "hololive", "hololive-alarm-worker", "VERSION")

				if *tc.version == "-" {
					harness.Remove(t, path)
				} else {
					harness.Write(t, path, *tc.version+"\n")
				}
			}

			r, calls := f.build(t, "", tc.targets...)
			harness.Fails(t, tc.name, r)

			if len(calls) != 0 {
				t.Errorf("%s invoked podman: %q", tc.name, calls)
			}
		})
	}
}
