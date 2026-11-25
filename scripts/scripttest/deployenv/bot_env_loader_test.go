package deployenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

type botFixture struct {
	script, bin, poison string
}

func newBotFixture(t *testing.T) botFixture {
	t.Helper()

	tmp := t.TempDir()
	module := filepath.Join(tmp, "repo", "hololive", "hololive-kakao-bot-go")
	f := botFixture{
		script: filepath.Join(module, "scripts", "bot.sh"),
		bin:    filepath.Join(tmp, "fakebin"),
		poison: filepath.Join(tmp, "env-command-executed"),
	}

	harness.Executable(t, f.script, strings.TrimPrefix(harness.Read(t, harness.Path(t, "hololive/hololive-api/scripts/bot.sh")), "#!/usr/bin/env bash\n"))
	harness.Write(t, filepath.Join(tmp, "repo", "scripts", "deploy", "lib", "compose-env.sh"), harness.Read(t, harness.Path(t, "scripts/deploy/lib/compose-env.sh")))
	harness.Write(t, filepath.Join(module, "bin", "bot"), "")
	harness.Write(t, filepath.Join(module, ".env"), "IRIS_BASE_URL=http://127.0.0.1:3000\nHOLODEX_API_KEY=test-key\nCACHE_HOST=127.0.0.1\nPOISON=$(touch "+f.poison+")\n")
	harness.Executable(t, filepath.Join(f.bin, "podman"), "case \"${1:-}\" in ps) exit 0 ;; *) exit 1 ;; esac\n")

	return f
}

func (f botFixture) run(t *testing.T, args ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{Script: `exec "$@"`, Args: append([]string{f.script}, args...), Bin: f.bin})
}

func TestBotEnvLoaderTreatsEnvAsLiteralData(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	r := f.run(t, "start")
	harness.Fails(t, "bot start with command substitution in .env", r)

	if _, err := os.Lstat(f.poison); err == nil {
		t.Error(".env command substitution was executed")
	}

	if !strings.Contains(r.Stderr, "command substitution") {
		t.Errorf("bot start did not report the command substitution: %s", r)
	}
}

func TestBotRejectsUnknownArgumentsBeforeLoadingEnv(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"start", "restart"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			r := newBotFixture(t).run(t, command, "--unknown-option")
			harness.Fails(t, command+" --unknown-option", r)

			if !strings.Contains(r.Stdout, "Unknown argument: --unknown-option") {
				t.Errorf("%s did not reject the unknown argument: %s", command, r)
			}

			if strings.Contains(r.Stderr, "command substitution") {
				t.Errorf("%s loaded .env before rejecting the argument: %s", command, r)
			}
		})
	}
}
