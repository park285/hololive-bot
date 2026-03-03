package iris

import "testing"

func TestDedupKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal", in: "abc-123", want: "iris:msg:{abc-123}"},
		{name: "trim spaces", in: "  id-1  ", want: "iris:msg:{id-1}"},
		{name: "empty", in: "", want: ""},
		{name: "spaces only", in: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DedupKey(tt.in)
			if got != tt.want {
				t.Fatalf("DedupKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveTokens(t *testing.T) {
	t.Parallel()

	webhook, bot := ResolveTokens("", "bot-token", "shared-token")
	if webhook != "shared-token" {
		t.Fatalf("webhook token = %q, want %q", webhook, "shared-token")
	}
	if bot != "bot-token" {
		t.Fatalf("bot token = %q, want %q", bot, "bot-token")
	}
}
