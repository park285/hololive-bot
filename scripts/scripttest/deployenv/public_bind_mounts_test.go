package deployenv

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const ingressTemplate = "events {}\nhttp { server { listen @BIND_IP@:30192; allow @BIND_IP@; } }\n"

type ingressFixture struct {
	root, template, config string
}

func newIngressFixture(t *testing.T) ingressFixture {
	t.Helper()

	root := t.TempDir()
	f := ingressFixture{
		root:     root,
		template: filepath.Join(root, "deploy", "nginx", "admin-dashboard-ingress.conf.template"),
		config:   filepath.Join(root, "rendered", "admin-dashboard-ingress.conf"),
	}
	harness.Write(t, f.template, ingressTemplate)

	return f
}

func (f ingressFixture) prepare(t *testing.T, bindIP string) harness.Result {
	t.Helper()

	script := `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/public-bind-mounts.sh"; prepare_admin_dashboard_ingress_bind_mount "$1"`
	env := []string{"HOLOLIVE_INGRESS_CONF=" + f.config}

	if bindIP == "" {
		script = "unset HOLOLIVE_BOT_PORT_BIND_IP; " + script
	} else {
		env = append(env, "HOLOLIVE_BOT_PORT_BIND_IP="+bindIP)
	}

	return harness.Bash(t, harness.Cmd{Script: script, Args: []string{f.root}, Env: env})
}

func TestIngressConfigRendersBindIP(t *testing.T) {
	t.Parallel()

	f := newIngressFixture(t)
	harness.Succeeds(t, "render", f.prepare(t, "100.100.1.9"))

	if mode := harness.Mode(t, f.config).Perm(); mode != 0o644 {
		t.Errorf("rendered config mode = %o, want 644", mode)
	}

	got := harness.Read(t, f.config)

	for _, want := range []string{"listen 100.100.1.9:30192;", "allow 100.100.1.9;"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "@BIND_IP@") {
		t.Errorf("rendered config kept the placeholder:\n%s", got)
	}
}

func TestIngressConfigReplacesStaleRender(t *testing.T) {
	t.Parallel()

	f := newIngressFixture(t)
	harness.Write(t, f.config, "stale\n")
	harness.Succeeds(t, "re-render", f.prepare(t, "100.100.1.7"))

	got := harness.Read(t, f.config)
	if !strings.Contains(got, "listen 100.100.1.7:30192;") || strings.Contains(got, "stale") {
		t.Errorf("re-render did not replace the stale config:\n%s", got)
	}
}

func TestIngressConfigRejectsSymlinkedConfig(t *testing.T) {
	t.Parallel()

	f := newIngressFixture(t)
	target := filepath.Join(f.root, "target")
	harness.Write(t, target, "unchanged\n")
	harness.Write(t, f.config, "")
	harness.Remove(t, f.config)
	harness.Symlink(t, target, f.config)

	harness.Fails(t, "symlinked config", f.prepare(t, "100.100.1.9"))

	if got := harness.Read(t, target); got != "unchanged\n" {
		t.Errorf("symlink target changed to %q", got)
	}
}

func TestIngressConfigRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()

	t.Run("symlinked template", func(t *testing.T) {
		t.Parallel()

		f := newIngressFixture(t)
		target := filepath.Join(f.root, "target")
		harness.Write(t, target, ingressTemplate)
		harness.Remove(t, f.template)
		harness.Symlink(t, target, f.template)
		harness.Fails(t, "symlinked template", f.prepare(t, "100.100.1.9"))
	})

	t.Run("missing bind IP", func(t *testing.T) {
		t.Parallel()

		harness.Fails(t, "missing bind IP", newIngressFixture(t).prepare(t, ""))
	})

	t.Run("non-literal bind IP", func(t *testing.T) {
		t.Parallel()

		harness.Fails(t, "non-literal bind IP", newIngressFixture(t).prepare(t, "127.0.0.1; include /tmp/injected.conf; #"))
	})
}
