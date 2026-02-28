package irisx

import "testing"

func TestResolveToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		token      string
		shared     string
		wantResult string
	}{
		{
			name:       "prefer explicit token",
			token:      "webhook-token",
			shared:     "shared-token",
			wantResult: "webhook-token",
		},
		{
			name:       "fallback to shared token",
			token:      "",
			shared:     "shared-token",
			wantResult: "shared-token",
		},
		{
			name:       "trim spaces",
			token:      "  bot-token  ",
			shared:     "  shared-token  ",
			wantResult: "bot-token",
		},
		{
			name:       "both empty",
			token:      " ",
			shared:     " ",
			wantResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveToken(tt.token, tt.shared)
			if got != tt.wantResult {
				t.Fatalf("ResolveToken(%q, %q) = %q, want %q", tt.token, tt.shared, got, tt.wantResult)
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
