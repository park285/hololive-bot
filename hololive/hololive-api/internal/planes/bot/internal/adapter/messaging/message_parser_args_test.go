package messaging

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestParseUpcomingArgs(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")
	tests := []struct {
		name string
		args []string
		want botcommand.UpcomingParams
	}{
		{
			name: "empty args",
			args: nil,
			want: botcommand.UpcomingParams{},
		},
		{
			name: "limit and member",
			args: []string{"12", testMemberPekora},
			want: botcommand.UpcomingParams{Limit: 12, Member: testMemberPekora},
		},
		{
			name: "all removes limit",
			args: []string{"20", "all", "미코"},
			want: botcommand.UpcomingParams{All: true, Member: "미코"},
		},
		{
			name: "non positive numbers treated as member tokens",
			args: []string{"0", "-3", "스이세이"},
			want: botcommand.UpcomingParams{Member: "0 -3 스이세이"},
		},
		{
			name: "second positive number goes to member",
			args: []string{"5", "3", "카나타"},
			want: botcommand.UpcomingParams{Limit: 5, Member: "3 카나타"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := adapter.parseUpcomingArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseUpcomingArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseScheduleArgs(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")
	tests := []struct {
		name string
		args []string
		want botcommand.ScheduleParams
	}{
		{
			name: "empty args",
			args: nil,
			want: botcommand.ScheduleParams{},
		},
		{
			name: "default days",
			args: []string{testMemberPekora},
			want: botcommand.ScheduleParams{Member: testMemberPekora, Days: 7},
		},
		{
			name: "invalid days keeps default",
			args: []string{testMemberPekora, "abc"},
			want: botcommand.ScheduleParams{Member: testMemberPekora, Days: 7},
		},
		{
			name: "clamps lower bound",
			args: []string{testMemberPekora, "0"},
			want: botcommand.ScheduleParams{Member: testMemberPekora, Days: 1},
		},
		{
			name: "clamps upper bound",
			args: []string{testMemberPekora, "99"},
			want: botcommand.ScheduleParams{Member: testMemberPekora, Days: 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := adapter.parseScheduleArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseScheduleArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeCompactAlarmTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		command     string
		args        []string
		wantCommand string
		wantArgs    []string
		wantChanged bool
	}{
		{
			name:        "mapped command",
			command:     "알람초기화",
			args:        []string{"dummy"},
			wantCommand: "알람",
			wantArgs:    []string{"초기화", "dummy"},
			wantChanged: true,
		},
		{
			name:        "unmapped command",
			command:     "알람",
			args:        []string{"목록"},
			wantCommand: "알람",
			wantArgs:    []string{"목록"},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotCommand, gotArgs, gotChanged := normalizeCompactAlarmTokens(tt.command, tt.args)
			if gotCommand != tt.wantCommand || !reflect.DeepEqual(gotArgs, tt.wantArgs) || gotChanged != tt.wantChanged {
				t.Fatalf("normalizeCompactAlarmTokens() = (%q, %#v, %v), want (%q, %#v, %v)",
					gotCommand, gotArgs, gotChanged, tt.wantCommand, tt.wantArgs, tt.wantChanged)
			}
		})
	}
}

func TestExtractMemberAndType(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")
	tests := []struct {
		name       string
		args       []string
		wantMember string
		wantType   string
	}{
		{
			name:       "member with explicit type",
			args:       []string{"시온", "shorts"},
			wantMember: "시온",
			wantType:   "쇼츠",
		},
		{
			name:       "single token no type split",
			args:       []string{"shorts"},
			wantMember: "shorts",
			wantType:   "",
		},
		{
			name:       "empty args",
			args:       nil,
			wantMember: "",
			wantType:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMember, gotType := adapter.extractMemberAndType(tt.args)
			if gotMember != tt.wantMember || gotType != tt.wantType {
				t.Fatalf("extractMemberAndType() = (%q, %q), want (%q, %q)", gotMember, gotType, tt.wantMember, tt.wantType)
			}
		})
	}
}
