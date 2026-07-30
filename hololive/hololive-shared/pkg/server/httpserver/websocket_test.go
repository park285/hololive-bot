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

package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestParseOrigins(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty string", raw: "", want: nil},
		{name: "whitespace only", raw: "   ", want: nil},
		{name: "single origin", raw: "https://example.com", want: []string{"https://example.com"}},
		{name: "multiple origins", raw: "https://a.com,https://b.com", want: []string{"https://a.com", "https://b.com"}},
		{name: "trim whitespace", raw: " https://a.com , https://b.com ", want: []string{"https://a.com", "https://b.com"}},
		{name: "skip empty parts", raw: "https://a.com,,https://b.com,", want: []string{"https://a.com", "https://b.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseOrigins(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("parseOrigins(%q) len = %d, want %d", tt.raw, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseOrigins(%q)[%d] = %q, want %q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCheckOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		origin  string
		want    bool
	}{
		{
			name:    "allowed origin passes",
			origins: []string{"https://bot.example.com", "https://admin.example.com"},
			origin:  "https://bot.example.com",
			want:    true,
		},
		{
			name:    "disallowed origin fails",
			origins: []string{"https://bot.example.com"},
			origin:  "https://evil.example.com",
			want:    false,
		},
		{
			name:    "empty env var denies all",
			origins: nil,
			origin:  "https://bot.example.com",
			want:    false,
		},
		{
			name:    "case insensitive matching",
			origins: []string{"https://Bot.Example.COM"},
			origin:  "https://bot.example.com",
			want:    true,
		},
		{
			name:    "empty origin header denied",
			origins: []string{"https://bot.example.com"},
			origin:  "",
			want:    false,
		},
		{
			name:    "host header fallback removed",
			origins: []string{},
			origin:  "https://localhost:8080",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 테스트용 오리진 설정
			orig := wsAllowedOrigins
			wsAllowedOrigins = tt.origins
			defer func() { wsAllowedOrigins = orig }()

			r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/ws", http.NoBody)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			r.Host = "localhost:8080"

			got := checkOrigin(r)
			if got != tt.want {
				t.Errorf("checkOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitWSUpgrader(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "https://a.com,https://b.com")

	InitWSUpgrader()

	if len(wsAllowedOrigins) != 2 {
		t.Fatalf("wsAllowedOrigins len = %d, want 2", len(wsAllowedOrigins))
	}
	if wsAllowedOrigins[0] != "https://a.com" {
		t.Errorf("wsAllowedOrigins[0] = %q, want %q", wsAllowedOrigins[0], "https://a.com")
	}
	if wsAllowedOrigins[1] != "https://b.com" {
		t.Errorf("wsAllowedOrigins[1] = %q, want %q", wsAllowedOrigins[1], "https://b.com")
	}
}

func TestInitWSUpgrader_EmptyDeniesAll(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "")

	InitWSUpgrader()

	if len(wsAllowedOrigins) != 0 {
		t.Fatalf("wsAllowedOrigins should be empty, got %d", len(wsAllowedOrigins))
	}

	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/ws", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.Header.Set("Origin", "https://anything.com")

	if checkOrigin(r) {
		t.Error("checkOrigin should deny when WEBSOCKET_ALLOWED_ORIGINS is empty")
	}
}

func TestWSUpgrader_DisallowedOriginReturns403(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "https://allowed.example.com")
	InitWSUpgrader()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := WSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if err := conn.Close(); err != nil {
			t.Errorf("close websocket connection: %v", err)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": []string{"https://evil.example.com"},
	})
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Errorf("close handshake response body: %v", err)
			}
		}()
	}
	if conn != nil {
		if err := conn.Close(); err != nil {
			t.Errorf("close websocket connection: %v", err)
		}
	}
	if err == nil {
		t.Fatal("expected websocket handshake failure for disallowed origin")
	}
	if resp == nil {
		t.Fatal("expected HTTP response on handshake failure")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}
