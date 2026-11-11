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
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

func TestSettingsService_LoadDefaultAndPersist(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "settings.json")

	defaults := settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
	}
	service := mustNewSettingsService(t, filePath, defaults)
	got := service.Get()

	if got.AlarmAdvanceMinutes != 5 {
		t.Fatalf("expected default 5, got %d", got.AlarmAdvanceMinutes)
	}

	updated := settingssvc.Settings{AlarmAdvanceMinutes: 12, TargetMinutes: []int{12, 3, 1}}
	if err := service.Update(updated); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got = service.Get()
	if got.AlarmAdvanceMinutes != 12 || !slices.Equal(got.TargetMinutes, updated.TargetMinutes) {
		t.Fatalf("published settings = %+v, want %+v", got, updated)
	}

	reloaded := mustNewSettingsService(t, filePath, defaults)

	got = reloaded.Get()

	if got.AlarmAdvanceMinutes != 12 || !slices.Equal(got.TargetMinutes, updated.TargetMinutes) {
		t.Fatalf("persisted settings = %+v, want %+v", got, updated)
	}
}

// 생성자는 디렉터리를 만들지 않는다. 첫 Update가 상위 디렉터리를 만들고 저장해야 한다.
func TestSettingsService_UpdateCreatesMissingParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	filePath := filepath.Join(dir, "settings.json")
	service := mustNewSettingsService(t, filePath, settingssvc.Settings{AlarmAdvanceMinutes: 5})

	if err := service.Update(settingssvc.Settings{AlarmAdvanceMinutes: 7, TargetMinutes: []int{7, 3, 1}}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	stored, found, err := settingssvc.ReadFile(filePath)
	if err != nil || !found {
		t.Fatalf("ReadFile() = %+v, %v, %v; want saved settings", stored, found, err)
	}

	if stored.AlarmAdvanceMinutes != 7 || !slices.Equal(stored.TargetMinutes, []int{7, 3, 1}) {
		t.Fatalf("stored settings = %+v, want alarmAdvanceMinutes=7 targetMinutes=[7 3 1]", stored)
	}
}

// 저장 파일의 알 수 없는 필드는 기동을 막지 않고, Update가 다시 쓴 파일에는 남지 않는다.
func TestSettingsService_DropsUnknownStoredFieldsOnUpdate(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")

	if err := os.WriteFile(filePath, []byte(`{"alarmAdvanceMinutes":5,"unknownToggle":true,"targetMinutes":[5,1]}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	service := mustNewSettingsService(t, filePath, settingssvc.Settings{AlarmAdvanceMinutes: 5})
	if err := service.Update(settingssvc.Settings{AlarmAdvanceMinutes: 7, TargetMinutes: []int{7}}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), "settings.json")
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	if bytes.Contains(raw, []byte("unknownToggle")) {
		t.Fatalf("updated settings file kept an unknown field: %s", raw)
	}
}

func TestSettingsService_PreservesTargetMinutesOnReload(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "settings.json")

	defaults := settingssvc.Settings{
		AlarmAdvanceMinutes: 30,
		TargetMinutes:       []int{30, 15, 5, 1},
	}
	service := mustNewSettingsService(t, filePath, defaults)
	current := service.Get()

	if err := service.Update(current); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	reloaded := mustNewSettingsService(t, filePath, settingssvc.Settings{AlarmAdvanceMinutes: 5})

	if got, want := reloaded.Get().TargetMinutes, []int{30, 15, 5, 1}; !slices.Equal(got, want) {
		t.Fatalf("expected target minutes %v, got %v", want, got)
	}
}

func TestSettingsService_DoesNotRewriteExplicitTargetMinutesOnReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")

	original := `{"alarmAdvanceMinutes":5,"targetMinutes":[5,1]}`
	if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	reloaded := mustNewSettingsService(t, filePath, settingssvc.Settings{AlarmAdvanceMinutes: 9, TargetMinutes: []int{9, 5, 1}})
	if got, want := reloaded.Get().TargetMinutes, []int{5, 1}; !slices.Equal(got, want) {
		t.Fatalf("expected target minutes %v, got %v", want, got)
	}

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
	service := mustNewSettingsService(t, path, settingssvc.Settings{AlarmAdvanceMinutes: 5})

	if err := service.Update(settingssvc.Settings{AlarmAdvanceMinutes: 7}); err != nil {
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

	reloaded := mustNewSettingsService(t, path, settingssvc.Settings{AlarmAdvanceMinutes: 5})
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
	service := mustNewSettingsService(t, path, settingssvc.Settings{AlarmAdvanceMinutes: 5})

	previous := settingssvc.Settings{AlarmAdvanceMinutes: 9, TargetMinutes: []int{9, 5, 1}}
	if err := service.Update(previous); err != nil {
		t.Fatalf("seed Update() error = %v", err)
	}

	before, readErr := fs.ReadFile(os.DirFS(dir), "settings.json")
	if readErr != nil {
		t.Fatalf("read seeded settings: %v", readErr)
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

	if err := service.Update(settingssvc.Settings{AlarmAdvanceMinutes: 11, TargetMinutes: []int{11, 3, 1}}); err == nil {
		t.Fatal("Update() error = nil, want failure on a read-only directory")
	}

	if got := service.Get(); got.AlarmAdvanceMinutes != previous.AlarmAdvanceMinutes || !slices.Equal(got.TargetMinutes, previous.TargetMinutes) {
		t.Fatalf("published settings after failed Update = %+v, want %+v", got, previous)
	}

	after, readErr := fs.ReadFile(os.DirFS(dir), "settings.json")
	if readErr != nil {
		t.Fatalf("read settings after failed Update: %v", readErr)
	}

	if !bytes.Equal(after, before) {
		t.Fatalf("settings file changed on failed Update: before=%s after=%s", before, after)
	}

	reloaded := mustNewSettingsService(t, path, settingssvc.Settings{AlarmAdvanceMinutes: 5})
	if got := reloaded.Get(); got.AlarmAdvanceMinutes != previous.AlarmAdvanceMinutes || !slices.Equal(got.TargetMinutes, previous.TargetMinutes) {
		t.Fatalf("persisted settings after failed Update = %+v, want %+v", got, previous)
	}
}

// 저장 파일 검증은 settingssvc.ReadFile이 맡는다. 생성자는 읽을 수 없는 파일을 기본값으로 대신하지 않고 기동 오류로 돌려준다.
func TestSettingsService_RejectsUnsupportedStoredFile(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(filePath, []byte(`{"alarmAdvanceMinutes":1}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if service, err := NewSettingsService(filePath, settingssvc.Settings{AlarmAdvanceMinutes: 5}); err == nil {
		t.Fatalf("NewSettingsService() = %+v, nil; want error", service.Get())
	}
}

func mustNewSettingsService(t *testing.T, filePath string, defaults settingssvc.Settings) *Service {
	t.Helper()

	service, err := NewSettingsService(filePath, defaults)
	if err != nil {
		t.Fatalf("NewSettingsService() error = %v", err)
	}

	return service
}
