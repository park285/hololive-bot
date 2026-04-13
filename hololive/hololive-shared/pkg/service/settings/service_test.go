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
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestSettingsService_LoadDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	defaults := Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: true,
	}
	svc := NewSettingsService(filePath, defaults, logger)
	got := svc.Get()
	if got.AlarmAdvanceMinutes != 5 {
		t.Fatalf("expected default 5, got %d", got.AlarmAdvanceMinutes)
	}
	if !got.ScraperProxyEnabled {
		t.Fatalf("expected default scraper proxy enabled true, got false")
	}

	updated := Settings{AlarmAdvanceMinutes: 12, ScraperProxyEnabled: false}
	if err := svc.Update(updated); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	reloaded := NewSettingsService(filePath, defaults, logger)
	got = reloaded.Get()
	if got.AlarmAdvanceMinutes != 12 {
		t.Fatalf("expected persisted 12, got %d", got.AlarmAdvanceMinutes)
	}
	if got.ScraperProxyEnabled {
		t.Fatalf("expected persisted scraper proxy enabled false, got true")
	}
}

func TestSettingsService_PreservesTargetMinutesOnReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	defaults := Settings{
		AlarmAdvanceMinutes: 30,
		ScraperProxyEnabled: false,
		TargetMinutes:       []int{30, 15, 5, 1},
	}
	svc := NewSettingsService(filePath, defaults, logger)
	current := svc.Get()
	current.ScraperProxyEnabled = true
	if err := svc.Update(current); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	reloaded := NewSettingsService(filePath, Settings{}, logger)
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
