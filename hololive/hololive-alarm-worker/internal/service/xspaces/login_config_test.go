package xspaces

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginConfigRequiresPrivateRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login.json")
	raw := []byte(`{"revision":1,"username":"example","password":"fixture-password"}`)
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	config, err := LoadLoginConfig(path)
	require.NoError(t, err)
	require.Equal(t, int64(1), config.Revision)
	require.Equal(t, "example", config.Username)
	// 이 테스트 소유 임시 파일의 느슨한 권한을 입력 검증이 거절하는지 확인합니다.
	require.NoError(t, os.Chmod(path, 0o644)) //nolint:gosec // 거절 경로를 재현하는 임시 파일이며 실제 인증 값은 없습니다.

	_, err = LoadLoginConfig(path)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "fixture-password")
	require.NoError(t, os.Chmod(path, 0o600))

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(path, link))

	_, err = LoadLoginConfig(link)
	require.Error(t, err)

	_, err = LoadLoginConfig("")
	require.ErrorIs(t, err, ErrDisabled)
}

func TestLoginConfigRejectsInvalidAndUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"revision":0,"username":"example","password":"secret"}`,
		`{"revision":1,"username":"../other","password":"secret"}`,
		`{"revision":1,"username":"example","password":""}`,
		`{"revision":1,"username":"example","password":"secret","extra":true}`,
		`{"revision":1,"username":"example","password":"secret\n"}`,
	} {
		path := filepath.Join(t.TempDir(), "login.json")
		require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))

		_, err := LoadLoginConfig(path)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}
