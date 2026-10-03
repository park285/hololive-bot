package api

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

const partialSettingsWriteTestEnv = "HOLOLIVE_TEST_PARTIAL_SETTINGS_WRITE"

// RLIMIT_FSIZE와 SIGXFSZ 처리는 자식 테스트 프로세스에만 적용하여 다른 병렬 시험의 파일 쓰기를 건드리지 않는다.
func TestSettingsAPIHandlerPartialWritePreservesFileSnapshotAndWorker(t *testing.T) {
	if os.Getenv(partialSettingsWriteTestEnv) == "1" {
		testSettingsPartialWrite(t)

		return
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	defer cancel()

	executable, err := os.Executable()

	require.NoError(t, err)

	// #nosec G204 -- os.Executable은 현재 테스트 바이너리이며 사용자 입력이나 셸을 사용하지 않는다.
	command := exec.CommandContext(ctx, executable, "-test.run=^TestSettingsAPIHandlerPartialWritePreservesFileSnapshotAndWorker$", "-test.timeout=15s")

	command.Env = append(os.Environ(), partialSettingsWriteTestEnv+"=1")

	output, err := command.CombinedOutput()

	require.NoError(t, err, "partial-write subprocess: %s", output)
}

func testSettingsPartialWrite(t *testing.T) {
	t.Helper()

	worker, targets, puts := newSettingsResponseLossWorker(t)

	dir := t.TempDir()

	path := filepath.Join(dir, "settings.json")

	service := mustNewTestSettingsService(t, path, settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())

	require.NoError(t, service.Update(settingssvc.Settings{AlarmAdvanceMinutes: 5, TargetMinutes: []int{5, 3, 1}}))

	before, err := fs.ReadFile(os.DirFS(dir), "settings.json")

	require.NoError(t, err)

	h := &SettingsHandler{Logger: newDiscardLogger(), Activity: testActivityLogger{}, Settings: service, SettingsApplier: sharedsettings.NewLocalSettingsApplier(alarm.NewClient(worker.URL, newDiscardLogger()))}

	var original syscall.Rlimit

	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original))

	limited := original

	limited.Cur = 32

	signal.Ignore(syscall.SIGXFSZ)

	defer signal.Reset(syscall.SIGXFSZ)

	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited))

	defer func() { require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original)) }()

	request, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":7}`))

	h.UpdateSettings(request)

	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original))

	assertErrorResponse(t, rec, http.StatusInternalServerError, "Failed to update settings")

	after, err := fs.ReadFile(os.DirFS(dir), "settings.json")

	require.NoError(t, err)

	assert.Equal(t, before, after)

	assert.Equal(t, 5, service.Get().AlarmAdvanceMinutes)

	assert.Equal(t, []int{5, 3, 1}, targets())

	assert.Zero(t, puts.Load())

	entries, err := os.ReadDir(dir)

	require.NoError(t, err)

	assert.Len(t, entries, 1, "failed partial write must remove its temp file")
}
