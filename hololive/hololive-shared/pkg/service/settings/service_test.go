package settings

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadFile_IgnoresUnknownStoredFields(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(filePath, []byte(`{"alarmAdvanceMinutes":5,"unknownToggle":true,"targetMinutes":[5,1]}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, found, err := ReadFile(filePath)
	if err != nil || !found {
		t.Fatalf("ReadFile() = %+v, %v, %v; want stored settings", got, found, err)
	}

	if got.AlarmAdvanceMinutes != 5 || !slices.Equal(got.TargetMinutes, []int{5, 1}) {
		t.Fatalf("settings = %+v, want alarmAdvanceMinutes=5 targetMinutes=[5 1]", got)
	}
}

func TestReadFile_RejectsUnsupportedStoredFile(t *testing.T) {
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

			if stored, found, err := ReadFile(filePath); err == nil {
				t.Fatalf("ReadFile() = %+v, %v, nil; want error", stored, found)
			}
		})
	}
}

func TestReadFile_NormalizesTargetMinutesWithoutRewriting(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	original := `{"alarmAdvanceMinutes":5,"targetMinutes":[1,5,5]}`

	if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, found, err := ReadFile(filePath)
	if err != nil || !found {
		t.Fatalf("ReadFile() = %+v, %v, %v; want stored settings", got, found, err)
	}

	if !slices.Equal(got.TargetMinutes, []int{5, 1}) {
		t.Fatalf("TargetMinutes = %v, want normalized [5 1]", got.TargetMinutes)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), "settings.json")
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	if string(raw) != original {
		t.Fatalf("settings file rewritten on read: %q", string(raw))
	}
}
