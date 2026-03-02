package adapter

import (
	"reflect"
	"testing"
)

func TestParseUpcomingArgs(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")
	tests := []struct {
		name string
		args []string
		want map[string]any
	}{
		{
			name: "empty args",
			args: nil,
			want: map[string]any{},
		},
		{
			name: "limit and member",
			args: []string{"12", "페코라"},
			want: map[string]any{"limit": 12, "member": "페코라"},
		},
		{
			name: "all removes limit",
			args: []string{"20", "all", "미코"},
			want: map[string]any{"all": true, "member": "미코"},
		},
		{
			name: "non positive numbers treated as member tokens",
			args: []string{"0", "-3", "스이세이"},
			want: map[string]any{"member": "0 -3 스이세이"},
		},
		{
			name: "second positive number goes to member",
			args: []string{"5", "3", "카나타"},
			want: map[string]any{"limit": 5, "member": "3 카나타"},
		},
	}

	for _, tt := range tests {
		tt := tt
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
		want map[string]any
	}{
		{
			name: "empty args",
			args: nil,
			want: map[string]any{},
		},
		{
			name: "default days",
			args: []string{"페코라"},
			want: map[string]any{"member": "페코라", "days": 7},
		},
		{
			name: "invalid days keeps default",
			args: []string{"페코라", "abc"},
			want: map[string]any{"member": "페코라", "days": 7},
		},
		{
			name: "clamps lower bound",
			args: []string{"페코라", "0"},
			want: map[string]any{"member": "페코라", "days": 1},
		},
		{
			name: "clamps upper bound",
			args: []string{"페코라", "99"},
			want: map[string]any{"member": "페코라", "days": 30},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := adapter.parseScheduleArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseScheduleArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseStatsArgs(t *testing.T) {
	t.Parallel()

	adapter := NewMessageAdapter("!")
	tests := []struct {
		name string
		args []string
		want map[string]any
	}{
		{
			name: "default action only",
			args: nil,
			want: map[string]any{"action": "gainers"},
		},
		{
			name: "token period keyword",
			args: []string{"today"},
			want: map[string]any{"action": "gainers", "period": "today"},
		},
		{
			name: "explicit key and canonical value",
			args: []string{"period=7d"},
			want: map[string]any{"action": "gainers", "period": "days:7"},
		},
		{
			name: "korean period key with keyword value",
			args: []string{"기간=week"},
			want: map[string]any{"action": "gainers", "period": "week"},
		},
		{
			name: "unknown key with canonical value",
			args: []string{"foo=last 2 weeks"},
			want: map[string]any{"action": "gainers", "period": "weeks:2"},
		},
		{
			name: "period key with unknown value keeps raw",
			args: []string{"period=??"},
			want: map[string]any{"action": "gainers", "period": "??"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := adapter.parseStatsArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseStatsArgs() = %#v, want %#v", got, tt.want)
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
		tt := tt
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMember, gotType := adapter.extractMemberAndType(tt.args)
			if gotMember != tt.wantMember || gotType != tt.wantType {
				t.Fatalf("extractMemberAndType() = (%q, %q), want (%q, %q)", gotMember, gotType, tt.wantMember, tt.wantType)
			}
		})
	}
}
