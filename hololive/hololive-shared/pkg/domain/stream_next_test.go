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

package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNextStreamStatus_IsLive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status domain.NextStreamStatus
		want   bool
	}{
		{
			// live 상태 → true
			name:   "live",
			status: domain.NextStreamStatusLive,
			want:   true,
		},
		{
			// upcoming 상태 → false
			name:   "upcoming",
			status: domain.NextStreamStatusUpcoming,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.status.IsLive()
			if got != tt.want {
				t.Errorf("IsLive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextStreamStatus_IsUpcoming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status domain.NextStreamStatus
		want   bool
	}{
		{
			// upcoming 상태 → true
			name:   "upcoming",
			status: domain.NextStreamStatusUpcoming,
			want:   true,
		},
		{
			// live 상태 → false
			name:   "live",
			status: domain.NextStreamStatusLive,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.status.IsUpcoming()
			if got != tt.want {
				t.Errorf("IsUpcoming() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextStreamStatus_IsValid(t *testing.T) {
	t.Parallel()

	// 유효한 4개 상태 상수
	validStatuses := []domain.NextStreamStatus{
		domain.NextStreamStatusLive,
		domain.NextStreamStatusUpcoming,
		domain.NextStreamStatusNoUpcoming,
		domain.NextStreamStatusTimeUnknown,
	}

	for _, s := range validStatuses {
		t.Run(string(s)+"_유효", func(t *testing.T) {
			t.Parallel()
			if !s.IsValid() {
				t.Errorf("IsValid() = false, want true (status=%q)", s)
			}
		})
	}

	invalidStatuses := []struct {
		name   string
		status domain.NextStreamStatus
	}{
		{"정의되지 않은 상태", domain.NextStreamStatus("invalid")},
		{"빈 문자열", domain.NextStreamStatus("")},
	}

	for _, tt := range invalidStatuses {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.status.IsValid() {
				t.Errorf("IsValid() = true, want false (status=%q)", tt.status)
			}
		})
	}
}

func TestNextStreamStatus_String(t *testing.T) {
	t.Parallel()

	// live → "live" 검증
	got := domain.NextStreamStatusLive.String()
	if got != "live" {
		t.Errorf("NextStreamStatusLive.String() = %q, want %q", got, "live")
	}
}
