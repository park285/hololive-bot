package deployenv

import (
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestYouTubeJSNodeVersionSupport(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"v26.12.1": true, "v26.13.0": true, "v26.99.1": true,
		"v22.22.2": false, "v23.0.0": false, "v24.22.0": false, "v25.9.0": false,
		"v26.11.9": false, "v26.12.0": false, "v27.1.0": false, "latest": false, "": false,
	}
	for version, supported := range cases {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			r := harness.Bash(t, harness.Cmd{
				Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/youtubejs-node-version.sh"; node_version_supported "$1"`,
				Args:   []string{version},
			})
			if got := r.ExitCode == 0; got != supported {
				t.Errorf("node_version_supported(%q) = %t, want %t", version, got, supported)
			}
		})
	}
}
