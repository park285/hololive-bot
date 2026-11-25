package apnative

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const osakaConf = `AP_NAME=osaka
AP_SSH_HOST=unreachable.invalid
AP_SSH_HOST_KEY_ALIAS=ap-osaka
AP_RUNTIME_MODE=native
AP_APPROVE_DEPLOY_VAR=I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY
AP_APPROVE_ROLLBACK_VAR=I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK
AP_BACKUP_PREFIX=osaka-active-active
`

type apHostFixture struct {
	root, key string
}

func newAPHostFixture(t *testing.T) apHostFixture {
	t.Helper()

	root := t.TempDir()

	slots, err := os.ReadFile(harness.Path(t, "scripts/deploy/lib/collector-slots.tsv"))
	if err != nil {
		t.Fatalf("read collector slots: %v", err)
	}

	harness.Write(t, filepath.Join(root, "scripts/deploy/lib/collector-slots.tsv"), string(slots))
	harness.Write(t, filepath.Join(root, "scripts/deploy/ap-hosts/osaka.conf"), osakaConf)
	harness.Write(t, filepath.Join(root, "KR.key"), "")

	return apHostFixture{root: root, key: filepath.Join(root, "KR.key")}
}

func (f apHostFixture) conf(t *testing.T, name, body string) {
	t.Helper()
	harness.Write(t, filepath.Join(f.root, "scripts/deploy/ap-hosts", name+".conf"), body)
}

func (f apHostFixture) load(t *testing.T, root, host, after string) harness.Result {
	t.Helper()

	script := `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/ap-host.sh"
SSH_KEY="$KEY" ap_host_load "$1" "$2"
` + after

	return harness.Bash(t, harness.Cmd{Script: script, Args: []string{root, host}, Env: []string{"KEY=" + f.key}})
}

func (f apHostFixture) sshOptions() string {
	return "-F /dev/null -i " + f.key + " -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10 -o SetEnv=LC_ALL=C -o SetEnv=LANG=C -o HostKeyAlias=ap-osaka"
}

func TestAPHostLoadBuildsOneBoundedTransport(t *testing.T) {
	t.Parallel()

	f := newAPHostFixture(t)
	r := f.load(t, f.root, "osaka", `printf '%s\n' "$AP_SSH_USER" "${AP_SSH[*]}" "$AP_GOAMD64" "$(ap_rsync_rsh)" "$(ap_rsync_target ./payload/)"`)
	harness.Succeeds(t, "load osaka", r)

	want := []string{"ubuntu", "ssh " + f.sshOptions() + " ubuntu@unreachable.invalid", "v1", "ssh " + f.sshOptions(), "ubuntu@unreachable.invalid:./payload/"}
	if got := strings.Split(strings.TrimSpace(r.Stdout), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("transport:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAPHostLoadValidatesGOAMD64(t *testing.T) {
	t.Parallel()

	f := newAPHostFixture(t)
	f.conf(t, "osaka", osakaConf+"AP_GOAMD64=v3\n")

	r := f.load(t, f.root, "osaka", `printf '%s' "$AP_GOAMD64"`)
	harness.Succeeds(t, "v3", r)

	if r.Stdout != "v3" {
		t.Errorf("AP_GOAMD64 = %q, want v3", r.Stdout)
	}

	f.conf(t, "osaka", osakaConf+"AP_GOAMD64=v9\n")
	harness.Fails(t, "unsupported GOAMD64", f.load(t, f.root, "osaka", ""))
}

func TestAPHostLoadRejectsInvalidConfs(t *testing.T) {
	t.Parallel()

	f := newAPHostFixture(t)
	cases := []struct{ name, conf, message string }{
		{"missing-mode", strings.Replace(osakaConf, "AP_RUNTIME_MODE=native\n", "", 1), "missing AP_RUNTIME_MODE"},
		{"compose-mode", strings.Replace(osakaConf, "AP_RUNTIME_MODE=native", "AP_RUNTIME_MODE=compose", 1), "AP_RUNTIME_MODE must be native"},
	}

	for _, tc := range cases {
		f.conf(t, tc.name, strings.Replace(tc.conf, "AP_NAME=osaka", "AP_NAME="+tc.name, 1))

		r := f.load(t, f.root, tc.name, "")
		harness.Fails(t, tc.name, r)

		if !strings.Contains(r.Stderr, tc.message) {
			t.Errorf("%s: stderr missing %q:\n%s", tc.name, tc.message, r.Stderr)
		}
	}

	r := f.load(t, f.root, "nonexistent-host", "")
	harness.Fails(t, "unknown host", r)

	if !strings.Contains(r.Stderr, "Unknown AP host: nonexistent-host") {
		t.Errorf("unknown host stderr:\n%s", r.Stderr)
	}
}

func TestAPRemoteBashFailsClosedWithBoundedSSH(t *testing.T) {
	t.Parallel()

	f := newAPHostFixture(t)
	ssh := filepath.Join(t.TempDir(), "ssh")
	argv := filepath.Join(t.TempDir(), "ssh.argv")
	harness.Executable(t, ssh, `printf '%s\n' "$*" > "$AP_HOST_TEST_ARGV"
exit 255
`)

	r := harness.Bash(t, harness.Cmd{
		Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/ap-host.sh"
SSH_KEY="$KEY" ap_host_load "$1" osaka
AP_SSH[0]="$FAKE_SSH"
ap_remote_bash </dev/null
`,
		Args: []string{f.root},
		Env:  []string{"KEY=" + f.key, "FAKE_SSH=" + ssh, "AP_HOST_TEST_ARGV=" + argv},
	})
	harness.Fails(t, "unreachable host", r)

	got := harness.Read(t, argv)

	for _, want := range []string{"-o BatchMode=yes", "-o ConnectTimeout=10"} {
		if !strings.Contains(got, want) {
			t.Errorf("ssh argv missing %q: %s", want, got)
		}
	}
}

func TestRepositoryAPHostConfsLoad(t *testing.T) {
	t.Parallel()

	f := newAPHostFixture(t)
	confs, err := filepath.Glob(harness.Path(t, "scripts/deploy/ap-hosts/*.conf"))

	if err != nil || len(confs) == 0 {
		t.Fatalf("no repository AP host confs: %v", err)
	}

	for _, conf := range confs {
		host := strings.TrimSuffix(filepath.Base(conf), ".conf")
		r := f.load(t, harness.Root(t), host, `printf '%s' "$AP_SSH_USER"`)
		harness.Succeeds(t, host, r)

		if r.Stdout == "" {
			t.Errorf("%s: ap_host_load left AP_SSH_USER empty", host)
		}
	}
}
