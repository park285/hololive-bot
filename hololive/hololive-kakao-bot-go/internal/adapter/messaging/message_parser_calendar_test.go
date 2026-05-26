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

			if tt.month > 0 {
				m, hasMonth := cmd.Params["month"].(int)
				if !hasMonth || m != tt.month {
					t.Errorf("Params[month] = %v, want %d", cmd.Params["month"], tt.month)
				}
			}
		})
	}
}
