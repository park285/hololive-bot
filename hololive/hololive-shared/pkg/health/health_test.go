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

package health

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"0은 0s", 0, "0s"},
		{"1h30m15s 정확히 일치", 1*time.Hour + 30*time.Minute + 15*time.Second, "1h30m15s"},
		{"500ms는 1초로 반올림", 500 * time.Millisecond, "1s"},
		{"5m는 5m0s", 5 * time.Minute, "5m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestGetVersion_Default(t *testing.T) {
	t.Parallel()
	got := GetVersion()
	if got != "dev" {
		t.Errorf("GetVersion() = %q, want %q", got, "dev")
	}
}

func TestGet_StatusOK(t *testing.T) {
	t.Parallel()
	resp := Get()
	if resp.Status != "ok" {
		t.Errorf("Get().Status = %q, want %q", resp.Status, "ok")
	}
}
