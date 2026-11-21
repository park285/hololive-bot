package eventgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenHelperBootstrapsShippedServer(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err)

	script, err := filepath.Abs("../../../eventgrade/server.mjs")
	require.NoError(t, err)

	secret := filepath.Join(t.TempDir(), "secret.json")
	require.NoError(t, os.WriteFile(secret, []byte(`{"app_version":"fixture","api_secret":"fixture-secret","credentials":{"jp":"j","us":"u","as":"a"}}`), 0o400))

	helper, err := OpenHelper(t.Context(), node, script, secret)
	require.NoError(t, err)

	_, err = helper.border(t.Context(), borderRequest{Region: "jp", EventID: "ev1", ChapterID: "ch1"})
	require.Error(t, err)
	require.NoError(t, helper.Close())

	select {
	case <-helper.Done():
	default:
		t.Fatal("helper process still running after Close")
	}
}
