package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/iris-client-go/iris"
)

func TestClassifyErrorCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		cause error
		want  string
	}{
		{name: "nil", cause: nil, want: ""},
		{name: "deadline", cause: fmt.Errorf("send alarm: %w", context.DeadlineExceeded), want: ErrorCodeTimeout},
		{name: "canceled", cause: fmt.Errorf("send alarm: %w", context.Canceled), want: ErrorCodeCanceled},
		{name: "http 404", cause: fmt.Errorf("send alarm: %w", &iris.HTTPError{StatusCode: 404}), want: ErrorCodeHTTP4xx},
		{name: "http 429", cause: fmt.Errorf("send alarm: %w", &iris.HTTPError{StatusCode: 429}), want: ErrorCodeHTTP4xx},
		{name: "http 503", cause: fmt.Errorf("send alarm: %w", &iris.HTTPError{StatusCode: 503}), want: ErrorCodeHTTP5xx},
		{name: "net op error", cause: fmt.Errorf("send alarm: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}), want: ErrorCodeNetwork},
		{name: "iris transport", cause: fmt.Errorf("send alarm: %w", &iris.TransportError{Op: "post", Err: errors.New("h3 stream reset")}), want: ErrorCodeNetwork},
		{name: "pg error", cause: fmt.Errorf("route failure: %w", &pgconn.PgError{Code: "40001", Message: "serialization failure"}), want: ErrorCodePG},
		{name: "deadline wrapped in net error wins", cause: fmt.Errorf("send alarm: %w", &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}), want: ErrorCodeTimeout},
		{name: "unknown", cause: errors.New("template render failed"), want: ErrorCodeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyErrorCode(tc.cause); got != tc.want {
				t.Fatalf("ClassifyErrorCode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeStoredError_RedactsSensitiveSpans(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		input       string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "bearer token",
			input:       "iris request failed: Authorization: Bearer eyJhbGciOi.secret-part after",
			wantContain: []string{"[redacted]", "after"},
			wantAbsent:  []string{"eyJhbGciOi", "secret-part"},
		},
		{
			name:        "key value token",
			input:       "config invalid: auth_token=supervalue rest",
			wantContain: []string{"auth_token=[redacted]", "rest"},
			wantAbsent:  []string{"supervalue"},
		},
		{
			name:        "api key header",
			input:       `header rejected: X-Api-Key: abc123 tail`,
			wantContain: []string{"[redacted]", "tail"},
			wantAbsent:  []string{"abc123"},
		},
		{
			name:        "url query string",
			input:       "post https://iris.internal/reply?auth=abc&room=123 returned 500",
			wantContain: []string{"?[redacted]", "returned 500"},
			wantAbsent:  []string{"auth=abc", "room=123"},
		},
		{
			name:        "long quoted payload",
			input:       `decode failed for "` + strings.Repeat("페이로드", 30) + `" at offset 3`,
			wantContain: []string{`"[redacted]"`, "at offset 3"},
			wantAbsent:  []string{"페이로드"},
		},
		{
			name:        "plain message unchanged",
			input:       "iris https://iris.internal/reply returned 502",
			wantContain: []string{"iris https://iris.internal/reply returned 502"},
		},
		{
			name:  "empty stays empty",
			input: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeStoredError(tc.input)
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Fatalf("sanitizeStoredError(%q) = %q, missing %q", tc.input, got, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Fatalf("sanitizeStoredError(%q) = %q, leaked %q", tc.input, got, absent)
				}
			}
			if tc.input == "" && got != "" {
				t.Fatalf("sanitizeStoredError(\"\") = %q, want empty", got)
			}
		})
	}
}

func TestTruncateStoredError_PreservesUTF8Boundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantLen int
	}{
		{name: "under limit unchanged", input: strings.Repeat("a", maxStoredErrorBytes), wantLen: maxStoredErrorBytes},
		{name: "ascii cut at limit", input: strings.Repeat("a", maxStoredErrorBytes+500), wantLen: maxStoredErrorBytes},
		{name: "hangul cut on rune boundary", input: strings.Repeat("가", 800), wantLen: 2046},
		{name: "emoji straddling limit", input: strings.Repeat("a", maxStoredErrorBytes-1) + "😀", wantLen: maxStoredErrorBytes - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateStoredError(tc.input)
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			if len(got) > maxStoredErrorBytes {
				t.Fatalf("len = %d exceeds %d", len(got), maxStoredErrorBytes)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("truncated string is not valid UTF-8: %q", got[len(got)-8:])
			}
			if !strings.HasPrefix(tc.input, got) {
				t.Fatal("truncated string is not a prefix of the input")
			}
		})
	}
}

func TestSanitizeStoredError_EnforcesByteBudgetAfterRedaction(t *testing.T) {
	t.Parallel()

	input := "Bearer " + strings.Repeat("t", 100) + " " + strings.Repeat("한", 900)
	got := sanitizeStoredError(input)
	if len(got) > maxStoredErrorBytes {
		t.Fatalf("len = %d exceeds %d", len(got), maxStoredErrorBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("sanitized string is not valid UTF-8")
	}
	if strings.Contains(got, strings.Repeat("t", 100)) {
		t.Fatal("bearer token leaked through sanitization")
	}
}
