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

func TestCommandType_IsValid(t *testing.T) {
	t.Parallel()

	// 유효한 16개 명령어 상수
	validCmds := []domain.CommandType{
		domain.CommandLive,
		domain.CommandUpcoming,
		domain.CommandSchedule,
		domain.CommandHelp,
		domain.CommandAlarmAdd,
		domain.CommandAlarmRemove,
		domain.CommandAlarmList,
		domain.CommandAlarmClear,
		domain.CommandAlarmInvalid,
		domain.CommandMemberInfo,
		domain.CommandStats,
		domain.CommandSubscriber,
		domain.CommandMemberNews,
		domain.CommandMemberNewsSubscription,
		domain.CommandMajorEvent,
		domain.CommandUnknown,
	}

	for _, cmd := range validCmds {
		t.Run(string(cmd)+"_유효", func(t *testing.T) {
			t.Parallel()
			if !cmd.IsValid() {
				t.Errorf("IsValid() = false, want true (cmd=%q)", cmd)
			}
		})
	}

	invalidCmds := []struct {
		name string
		cmd  domain.CommandType
	}{
		{"정의되지 않은 명령어", domain.CommandType("invalid_cmd")},
		{"빈 문자열", domain.CommandType("")},
		{"제거된 legacy status 명령어", domain.CommandType("settlement" + "_status")},
		{"제거된 legacy paid 명령어", domain.CommandType("settlement" + "_paid")},
		{"제거된 legacy register 명령어", domain.CommandType("settlement" + "_register")},
	}

	for _, tt := range invalidCmds {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.cmd.IsValid() {
				t.Errorf("IsValid() = true, want false (cmd=%q)", tt.cmd)
			}
		})
	}
}

func TestCommandType_String(t *testing.T) {
	t.Parallel()

	// CommandLive → "live" 검증
	got := domain.CommandLive.String()
	if got != "live" {
		t.Errorf("CommandLive.String() = %q, want %q", got, "live")
	}
}

func TestParseResults_IsSingle(t *testing.T) {
	t.Parallel()

	single := &domain.ParseResult{Command: domain.CommandLive}

	tests := []struct {
		name    string
		results *domain.ParseResults
		want    bool
	}{
		{
			// Single이 설정된 경우 → true
			name:    "Single 설정됨",
			results: &domain.ParseResults{Single: single},
			want:    true,
		},
		{
			// Single이 nil, Multiple만 있는 경우 → false
			name: "Single nil, Multiple만 있음",
			results: &domain.ParseResults{
				Multiple: []*domain.ParseResult{single},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.results.IsSingle()
			if got != tt.want {
				t.Errorf("IsSingle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseResults_IsMultiple(t *testing.T) {
	t.Parallel()

	single := &domain.ParseResult{Command: domain.CommandLive}

	tests := []struct {
		name    string
		results *domain.ParseResults
		want    bool
	}{
		{
			// Multiple에 항목이 있는 경우 → true
			name: "Multiple 비어있지 않음",
			results: &domain.ParseResults{
				Multiple: []*domain.ParseResult{single},
			},
			want: true,
		},
		{
			// Multiple이 빈 슬라이스 → false
			name:    "Multiple 빈 슬라이스",
			results: &domain.ParseResults{Multiple: []*domain.ParseResult{}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.results.IsMultiple()
			if got != tt.want {
				t.Errorf("IsMultiple() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseResults_GetCommands(t *testing.T) {
	t.Parallel()

	single := &domain.ParseResult{Command: domain.CommandLive}
	multi1 := &domain.ParseResult{Command: domain.CommandLive}
	multi2 := &domain.ParseResult{Command: domain.CommandUpcoming}

	tests := []struct {
		name      string
		results   *domain.ParseResults
		wantLen   int
		wantFirst domain.CommandType
	}{
		{
			// Single이 설정된 경우 → [Single] 반환
			name:      "Single → 단일 슬라이스",
			results:   &domain.ParseResults{Single: single},
			wantLen:   1,
			wantFirst: domain.CommandLive,
		},
		{
			// Multiple이 설정된 경우 → Multiple 반환
			name: "Multiple → 그대로 반환",
			results: &domain.ParseResults{
				Multiple: []*domain.ParseResult{multi1, multi2},
			},
			wantLen:   2,
			wantFirst: domain.CommandLive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.results.GetCommands()
			if len(got) != tt.wantLen {
				t.Fatalf("GetCommands() 길이 = %d, want %d", len(got), tt.wantLen)
			}
			if got[0].Command != tt.wantFirst {
				t.Errorf("GetCommands()[0].Command = %q, want %q", got[0].Command, tt.wantFirst)
			}
		})
	}
}
