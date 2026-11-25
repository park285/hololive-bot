package deployenv

import (
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestQUICUDPBufferUsesLastPersistedAssignment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files map[string]string
		ok    bool
	}{
		{
			name: "later sysctl.d file lowers the buffers",
			files: map[string]string{
				"etc/sysctl.d/10-high.conf":         "net.core.rmem_max=2048\nnet.core.wmem_max=2048\n",
				"etc/sysctl.d/90-low-override.conf": "net.core.rmem_max=512\nnet.core.wmem_max=512\n",
			},
		},
		{
			name: "effective persisted value satisfies the requirement",
			files: map[string]string{
				"etc/sysctl.d/10-high.conf":         "net.core.rmem_max=2048\nnet.core.wmem_max=2048\n",
				"etc/sysctl.d/90-low-override.conf": "net.core.rmem_max=4096\nnet.core.wmem_max=4096\n",
			},
			ok: true,
		},
		{
			name: "sysctl.conf is the final assignment",
			files: map[string]string{
				"etc/sysctl.d/10-high.conf":         "net.core.rmem_max=2048\nnet.core.wmem_max=2048\n",
				"etc/sysctl.d/90-low-override.conf": "net.core.rmem_max=4096\nnet.core.wmem_max=4096\n",
				"etc/sysctl.conf":                   "net.core.rmem_max=512\nnet.core.wmem_max=512\n",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			for rel, content := range tc.files {
				harness.Write(t, filepath.Join(root, rel), content)
			}

			r := harness.Bash(t, harness.Cmd{
				Script: `bash "$SCRIPTTEST_ROOT/scripts/deploy/lib/require-quic-udp-buffer.sh" 1024 fixture-host`,
				Env:    []string{"AP_SYSCTL_ROOT=" + root},
			})

			if tc.ok {
				harness.Succeeds(t, tc.name, r)
			} else {
				harness.Fails(t, tc.name, r)
			}
		})
	}
}
