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

package middleware

import (
	"testing"
)

func TestHasClientHints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ch   ClientHints
		want bool
	}{
		{
			name: "빈 구조체 → false",
			ch:   ClientHints{},
			want: false,
		},
		{
			name: "Platform 설정 → true",
			ch:   ClientHints{Platform: "Android"},
			want: true,
		},
		{
			name: "Model 설정 → true",
			ch:   ClientHints{Model: "SM-S928N"},
			want: true,
		},
		{
			name: "PlatformVersion 설정 → true",
			ch:   ClientHints{PlatformVersion: "16.0.0"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.ch.HasClientHints(); got != tt.want {
				t.Fatalf("HasClientHints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ch   ClientHints
		want string
	}{
		{
			name: "Android 16 모델 포함",
			ch: ClientHints{
				Platform:        "Android",
				PlatformVersion: "16.0.0",
				Model:           "SM-S928N",
			},
			want: "Android 16 (SM-S928N)",
		},
		{
			name: "Windows 11: platformVersion 13.0.0",
			ch: ClientHints{
				Platform:        "Windows",
				PlatformVersion: "13.0.0",
			},
			want: "Windows 11",
		},
		{
			name: "Windows 10: platformVersion 5.0.0",
			ch: ClientHints{
				Platform:        "Windows",
				PlatformVersion: "5.0.0",
			},
			want: "Windows 10",
		},
		{
			name: "macOS 14",
			ch: ClientHints{
				Platform:        "macOS",
				PlatformVersion: "14.2.1",
			},
			want: "macOS 14",
		},
		{
			name: "Platform만 있는 경우",
			ch: ClientHints{
				Platform: "Linux",
			},
			want: "Linux",
		},
		{
			name: "Windows 11 x64 아키텍처 포함",
			ch: ClientHints{
				Platform:        "Windows",
				PlatformVersion: "13.0.0",
				Architecture:    "x86",
				Bitness:         "64",
			},
			want: "Windows 11 x64",
		},
		{
			name: "Android 모바일 모델 없음",
			ch: ClientHints{
				Platform: "Android",
				Mobile:   true,
			},
			want: "Android [Mobile]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.ch.Summary()
			if got != tt.want {
				t.Fatalf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranslateWindowsVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		majorVersion string
		want         string
	}{
		{"13", "11"},
		{"15", "11"},
		{"5", "10"},
		{"1", "10"},
		{"0", "8.1 or older"},
	}

	for _, tt := range tests {
		t.Run("major="+tt.majorVersion, func(t *testing.T) {
			t.Parallel()

			got := translateWindowsVersion(tt.majorVersion)
			if got != tt.want {
				t.Fatalf("translateWindowsVersion(%q) = %q, want %q", tt.majorVersion, got, tt.want)
			}
		})
	}
}

func TestFormatArchitecture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		arch    string
		bitness string
		want    string
	}{
		{
			name:    "x86 + 64비트 → x64",
			arch:    "x86",
			bitness: "64",
			want:    "x64",
		},
		{
			name:    "arm + 64비트 → arm64",
			arch:    "arm",
			bitness: "64",
			want:    "arm64",
		},
		{
			name:    "x86 + 32비트 → x86",
			arch:    "x86",
			bitness: "32",
			want:    "x86",
		},
		{
			name:    "arm + 비트 없음 → arm",
			arch:    "arm",
			bitness: "",
			want:    "arm",
		},
		{
			name:    "riscv + 64비트 → riscv64",
			arch:    "riscv",
			bitness: "64",
			want:    "riscv64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatArchitecture(tt.arch, tt.bitness)
			if got != tt.want {
				t.Fatalf("formatArchitecture(%q, %q) = %q, want %q", tt.arch, tt.bitness, got, tt.want)
			}
		})
	}
}
