package testaccountcli

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/session"
)

const (
	issueCommand          = "issue"
	statusCommand         = "status"
	credentialsFileOption = "--credentials-file"
)

func configureCommand(t *testing.T) {
	t.Helper()

	for _, key := range []string{"ADMIN_PASS_HASH_FILE", "SESSION_SECRET_FILE", "VALKEY_URL_FILE", "HOLO_BOT_API_KEY_FILE", "ADMIN_PASS_BCRYPT", "ADMIN_SECRET_KEY"} {
		t.Setenv(key, "")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-administrator-password"), 10)
	require.NoError(t, err)
	t.Setenv("ENV", "test")
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASS_HASH", string(hash))
	t.Setenv("SESSION_SECRET", "synthetic-session-secret-with-at-least-32-bytes")
	t.Setenv("VALKEY_URL", miniredis.RunT(t).Addr())
	t.Setenv("ALLOWED_ORIGINS", "https://admin.test")
}

func readCredentials(t *testing.T, file string) session.TestCredentials {
	t.Helper()

	data := readCredentialBytes(t, file)

	var credentials session.TestCredentials

	require.NoError(t, jsonv2.Unmarshal(data, &credentials))

	return credentials
}

func readCredentialBytes(t *testing.T, file string) []byte {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(file))
	require.NoError(t, err)

	defer func() { require.NoError(t, root.Close()) }()

	data, err := root.ReadFile(filepath.Base(file))
	require.NoError(t, err)

	return data
}

func privateDirectory(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "private")
	require.NoError(t, os.Mkdir(dir, 0o700))

	return dir
}

func TestCommandIssuesReportsAndRevokesWithoutPrintingCredentials(t *testing.T) {
	configureCommand(t)

	file := filepath.Join(privateDirectory(t), "credentials.json")

	var output bytes.Buffer

	require.NoError(t, Run(t.Context(), []string{issueCommand, "--ttl", "1m", credentialsFileOption, file}, &output))

	credentials := readCredentials(t, file)
	require.NotEmpty(t, credentials.Password)
	require.NotContains(t, output.String(), credentials.Password)
	require.NotContains(t, output.String(), "password")
	require.Contains(t, output.String(), `"read_only":true`)

	info, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	output.Reset()
	require.NoError(t, Run(t.Context(), []string{statusCommand}, &output))
	require.Contains(t, output.String(), `"status":"active"`)
	require.NotContains(t, output.String(), "password")

	duplicate := filepath.Join(filepath.Dir(file), "second.json")
	require.ErrorIs(t, Run(t.Context(), []string{issueCommand, credentialsFileOption, duplicate}, io.Discard), session.ErrTestAccountExists)
	output.Reset()
	require.NoError(t, Run(t.Context(), []string{"revoke", "--username", credentials.Username}, &output))
	require.Contains(t, output.String(), `"status":"revoked"`)
	output.Reset()
	require.NoError(t, Run(t.Context(), []string{statusCommand}, &output))
	require.JSONEq(t, `{"status":"absent","expires_at_unix":0,"read_only":true}`, output.String())
}

func TestCredentialFileRejectsExistingFilesSymlinksAndPublicDirectories(t *testing.T) {
	dir := privateDirectory(t)
	file := filepath.Join(dir, "credentials.json")
	credentials := session.TestCredentials{Username: "test-synthetic", Password: "synthetic-password", ExpiresAtUnix: 2000000000}
	require.NoError(t, writeCredentials(file, credentials))

	before := readCredentialBytes(t, file)
	require.Error(t, writeCredentials(file, credentials))

	after := readCredentialBytes(t, file)
	require.Equal(t, before, after)

	link := filepath.Join(dir, "link.json")
	require.NoError(t, os.Symlink(file, link))
	require.Error(t, writeCredentials(link, credentials))

	publicDir := t.TempDir()
	// G302는 디렉터리도 일반 파일로 판정합니다. 거부 검사용 임시 디렉터리에만 그룹 권한을 줍니다.
	require.NoError(t, os.Chmod(publicDir, 0o750)) //nolint:gosec // 조회 계정 파일의 비공개 디렉터리 검사를 위한 입력입니다.
	require.Error(t, writeCredentials(filepath.Join(publicDir, "credentials.json"), credentials))
}

type failedReceiptWriter struct{}

func (failedReceiptWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic closed output")
}

func TestUnreportedIssuanceKeepsTheExactCredentialsForReconciliation(t *testing.T) {
	configureCommand(t)

	file := filepath.Join(privateDirectory(t), "credentials.json")
	require.Error(t, Run(t.Context(), []string{issueCommand, credentialsFileOption, file}, failedReceiptWriter{}))

	credentials := readCredentials(t, file)

	var output bytes.Buffer

	require.NoError(t, Run(t.Context(), []string{statusCommand}, &output))
	require.Contains(t, output.String(), credentials.Username)
	require.NotContains(t, output.String(), credentials.Password)
	require.NoError(t, Run(t.Context(), []string{"revoke", "--username", credentials.Username}, io.Discard))
}

func TestCommandRejectsUnboundedOrAmbiguousArgumentsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{nil, {issueCommand}, {issueCommand, credentialsFileOption, "relative"}, {issueCommand, credentialsFileOption, "/tmp/a", "--ttl", "61m"}, {issueCommand, credentialsFileOption, "/tmp/a", "--ttl", "59s"}, {statusCommand, "extra"}, {"revoke"}, {"unsupported"}, {statusCommand, "--password", "synthetic"}} {
		_, err := parseOptions(args)
		require.Error(t, err)
	}

	options, err := parseOptions([]string{issueCommand, credentialsFileOption, "/tmp/private/credentials.json"})
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, options.ttl)
}
