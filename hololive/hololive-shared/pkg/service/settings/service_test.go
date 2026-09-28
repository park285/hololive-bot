// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package settings

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSettingsService_LoadDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.DiscardHandler)

	defaults := Settings{
		AlarmAdvanceMinutes: 5,
	}
	service := mustNewSettingsService(t, filePath, defaults, logger)
	got := service.Get()

	if got.AlarmAdvanceMinutes != 5 {
		t.Fatalf("expected default 5, got %d", got.AlarmAdvanceMinutes)
	}

	updated := Settings{AlarmAdvanceMinutes: 12}
	if err := service.Update(updated); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	reloaded := mustNewSettingsService(t, filePath, defaults, logger)

	got = reloaded.Get()

	if got.AlarmAdvanceMinutes != 12 {
		t.Fatalf("expected persisted 12, got %d", got.AlarmAdvanceMinutes)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), "settings.json")
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	// scraper proxy 토글은 퇴역했으므로 저장 파일에 다시 쓰지 않는다(DEC-20260926-hololive-legacy-env-config-retirement).
	if strings.Contains(string(raw), "scraperProxyEnabled") {
		t.Fatalf("persisted settings still carry the retired scraperProxyEnabled key: %s", raw)
	}
}

// 퇴역 전 파일에 남은 scraperProxyEnabled는 json/v2 기본 decode가 모르는 멤버로 무시하고, 나머지 값은 그대로 읽는다.
func TestSettingsService_ReadsFileWithRetiredScraperProxyKey(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(filePath, []byte(`{"alarmAdvanceMinutes":5,"scraperProxyEnabled":true,"targetMinutes":[5,1]}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got := mustNewSettingsService(t, filePath, Settings{}, slog.New(slog.DiscardHandler)).Get()
	if got.AlarmAdvanceMinutes != 5 || !slices.Equal(got.TargetMinutes, []int{5, 1}) {
		t.Fatalf("settings = %+v, want alarmAdvanceMinutes=5 targetMinutes=[5 1]", got)
	}
}

func TestSettingsService_PreservesTargetMinutesOnReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.DiscardHandler)

	defaults := Settings{
		AlarmAdvanceMinutes: 30,
		TargetMinutes:       []int{30, 15, 5, 1},
	}
	service := mustNewSettingsService(t, filePath, defaults, logger)
	current := service.Get()

	if err := service.Update(current); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	reloaded := mustNewSettingsService(t, filePath, Settings{}, logger)
	got := reloaded.Get()
	want := []int{30, 15, 5, 1}

	if len(got.TargetMinutes) != len(want) {
		t.Fatalf("expected target minutes len %d, got %d (%v)", len(want), len(got.TargetMinutes), got.TargetMinutes)
	}

	for i := range want {
		if got.TargetMinutes[i] != want[i] {
			t.Fatalf("expected target minutes %v, got %v", want, got.TargetMinutes)
		}
	}
}

func TestSettingsService_PreservesExplicitStoredTargetMinutesOnReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.DiscardHandler)

	if err := os.WriteFile(filePath, []byte(`{"alarmAdvanceMinutes":5,"targetMinutes":[5,1]}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	reloaded := mustNewSettingsService(t, filePath, Settings{}, logger)
	got := reloaded.Get()
	want := []int{5, 1}

	if len(got.TargetMinutes) != len(want) {
		t.Fatalf("expected target minutes len %d, got %d (%v)", len(want), len(got.TargetMinutes), got.TargetMinutes)
	}

	for i := range want {
		if got.TargetMinutes[i] != want[i] {
			t.Fatalf("expected target minutes %v, got %v", want, got.TargetMinutes)
		}
	}
}

func TestSettingsService_DoesNotRewriteExplicitTargetMinutesOnReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.DiscardHandler)

	original := `{"alarmAdvanceMinutes":5,"targetMinutes":[5,1]}`
	if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	_ = mustNewSettingsService(t, filePath, Settings{}, logger)

	raw, err := fs.ReadFile(os.DirFS(dir), "settings.json")
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	if string(raw) != original {
		t.Fatalf("expected explicit settings file unchanged, got %q", string(raw))
	}
}

func TestSettingsService_UpdateLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	service := mustNewSettingsService(t, path, Settings{AlarmAdvanceMinutes: 5}, slog.New(slog.DiscardHandler))

	if err := service.Update(Settings{AlarmAdvanceMinutes: 7}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	for _, entry := range entries {
		if entry.Name() != "settings.json" {
			t.Fatalf("unexpected leftover file %q after Update()", entry.Name())
		}
	}

	reloaded := mustNewSettingsService(t, path, Settings{AlarmAdvanceMinutes: 5}, slog.New(slog.DiscardHandler))
	if got := reloaded.Get(); got.AlarmAdvanceMinutes != 7 {
		t.Fatalf("reloaded settings = %+v, want AlarmAdvanceMinutes=7", got)
	}
}

func TestSettingsService_UpdateFailsWithoutClobberingExistingFileWhenDirIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	service := mustNewSettingsService(t, path, Settings{AlarmAdvanceMinutes: 5}, slog.New(slog.DiscardHandler))

	if err := service.Update(Settings{AlarmAdvanceMinutes: 9}); err != nil {
		t.Fatalf("seed Update() error = %v", err)
	}

	// 디렉터리는 탐색에 x 비트가 필요해 0600 이하로 낮출 수 없다.
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // G302: 쓰기 불가 디렉터리 재현용 모드
		t.Fatalf("Chmod() error = %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: t.TempDir() 정리를 위한 모드 복구
			t.Errorf("restore dir mode: %v", err)
		}
	})

	if err := service.Update(Settings{AlarmAdvanceMinutes: 11}); err == nil {
		t.Fatal("Update() error = nil, want failure on a read-only directory")
	}

	reloaded := mustNewSettingsService(t, path, Settings{AlarmAdvanceMinutes: 5}, slog.New(slog.DiscardHandler))
	if got := reloaded.Get().AlarmAdvanceMinutes; got != 9 {
		t.Fatalf("persisted AlarmAdvanceMinutes = %d, want the pre-failure value 9", got)
	}
}

// 저장 파일은 settings.ReadFile 하나가 해석한다. 구형 형식(targetMinutes 없음)이나 읽을 수 없는 파일을 기본값으로 대신하지
// 않고 기동 오류로 드러낸다(stack-audit 2026-09-26 T11 holo-settings-file-legacy-format-and-dual-reader).
func TestSettingsService_RejectsUnsupportedStoredFile(t *testing.T) {
	for name, content := range map[string]string{
		"legacy advance-only format": `{"alarmAdvanceMinutes":1}`,
		"missing advance minute":     `{"targetMinutes":[5,1]}`,
		"non-positive targets only":  `{"alarmAdvanceMinutes":5,"targetMinutes":[0,-1]}`,
		"undecodable":                `{"alarmAdvanceMinutes":`,
	} {
		t.Run(name, func(t *testing.T) {
			filePath := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
				t.Fatalf("write settings: %v", err)
			}

			if service, err := NewSettingsService(filePath, Settings{AlarmAdvanceMinutes: 5}, slog.New(slog.DiscardHandler)); err == nil {
				t.Fatalf("NewSettingsService() = %+v, nil; want error", service.Get())
			}
		})
	}
}

// 정규화 결과가 저장값과 달라도 파일을 다시 쓰지 않는다. 다음 Update가 정규화된 값을 기록한다.
func TestSettingsService_DoesNotRewriteNonCanonicalTargetMinutes(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	original := `{"alarmAdvanceMinutes":5,"targetMinutes":[1,5,5]}`

	if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	service := mustNewSettingsService(t, filePath, Settings{}, slog.New(slog.DiscardHandler))
	if got := service.Get().TargetMinutes; !slices.Equal(got, []int{5, 1}) {
		t.Fatalf("TargetMinutes = %v, want normalized [5 1]", got)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), "settings.json")
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	if string(raw) != original {
		t.Fatalf("settings file rewritten on load: %q", string(raw))
	}
}

func mustNewSettingsService(t *testing.T, filePath string, defaults Settings, logger *slog.Logger) *Service {
	t.Helper()

	service, err := NewSettingsService(filePath, defaults, logger)
	if err != nil {
		t.Fatalf("NewSettingsService() error = %v", err)
	}

	return service
}
