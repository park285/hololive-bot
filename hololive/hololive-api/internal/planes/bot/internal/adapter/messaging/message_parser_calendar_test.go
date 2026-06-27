package messaging

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestTryCelebrationCalendarCommand(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!", "")

	tests := []struct {
		name    string
		command string
		args    []string
		raw     string
		want    bool
		month   int
		offset  int
	}{
		{
			name:    "달력 without args",
			command: "달력",
			args:    nil,
			raw:     "!달력",
			want:    true,
		},
		{
			name:    "달력 with valid month",
			command: "달력",
			args:    []string{"6"},
			raw:     "!달력 6",
			want:    true,
			month:   6,
		},
		{
			name:    "달력 next month",
			command: "달력",
			args:    []string{"다음달"},
			raw:     "!달력 다음달",
			want:    true,
			offset:  1,
		},
		{
			name:    "달력 previous month",
			command: "달력",
			args:    []string{"저번달"},
			raw:     "!달력 저번달",
			want:    true,
			offset:  -1,
		},
		{
			name:    "기념일 alias",
			command: "기념일",
			args:    nil,
			raw:     "!기념일",
			want:    true,
		},
		{
			name:    "calendar alias",
			command: "calendar",
			args:    nil,
			raw:     "!calendar",
			want:    true,
		},
		{
			name:    "invalid month ignored",
			command: "달력",
			args:    []string{"13"},
			raw:     "!달력 13",
			want:    true,
		},
		{
			name:    "non-numeric month ignored",
			command: "달력",
			args:    []string{"abc"},
			raw:     "!달력 abc",
			want:    true,
		},
		{
			name:    "unrelated command",
			command: "도움말",
			args:    nil,
			raw:     "!도움말",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, ok := adapter.tryCelebrationCalendarCommand(tt.command, tt.args, tt.raw)
			if ok != tt.want {
				t.Fatalf("tryCelebrationCalendarCommand() matched = %v, want %v", ok, tt.want)
			}

			if !ok {
				return
			}

			if cmd.Type != domain.CommandCalendar {
				t.Errorf("Type = %v, want %v", cmd.Type, domain.CommandCalendar)
			}

			assertMonthParam(t, cmd.Params, tt.month)
			assertOffsetParam(t, cmd.Params, tt.offset)
		})
	}
}

func assertMonthParam(t *testing.T, params map[string]any, want int) {
	t.Helper()
	if want <= 0 {
		return
	}
	m, hasMonth := params["month"].(int)
	if !hasMonth || m != want {
		t.Errorf("Params[month] = %v, want %d", params["month"], want)
	}
}

func assertOffsetParam(t *testing.T, params map[string]any, want int) {
	t.Helper()
	if want == 0 {
		return
	}
	offset, hasOffset := params["monthOffset"].(int)
	if !hasOffset || offset != want {
		t.Errorf("Params[monthOffset] = %v, want %d", params["monthOffset"], want)
	}
}
