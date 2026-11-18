package adminruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchedulePosterAPIKeyDisabled(t *testing.T) {
	t.Parallel()

	key, err := loadSchedulePosterAPIKey("")
	require.NoError(t, err)
	require.Empty(t, key, "경로가 없으면 자동 검토를 끈다")
}

func TestLoadSchedulePosterAPIKeyError(t *testing.T) {
	t.Parallel()

	const secret = "synthetic-secret-0123456789abcdef"

	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte("GEMINI_API_KEY="+secret), 0o600))

	key, err := loadSchedulePosterAPIKey(path)
	require.ErrorContains(t, err, "load schedule poster gemini api key:")
	require.Empty(t, key)
	require.NotContains(t, err.Error(), secret, "오류에 키를 싣지 않는다")
}
