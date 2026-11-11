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
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// 저장 파일의 알 수 없는 필드는 기동을 막지 않는다. 다시 저장할 때 남지 않는 것은 writer 쪽 시험이 확인한다.
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

// 구형 형식(targetMinutes 없음)이나 읽을 수 없는 파일을 기본값으로 대신하지 않고 오류로 드러낸다.
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

// 정규화 결과가 저장값과 달라도 파일을 다시 쓰지 않는다. 다음 저장이 정규화된 값을 기록한다.
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
