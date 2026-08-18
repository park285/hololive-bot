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
	"github.com/park285/iris-client-go/v2/iris"
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
		name  string
		input string
		want  string
	}{
		{
			name:  "bearer token after auth header",
			input: "iris request failed: Authorization: Bearer eyJhbGciOi.secret-part after",
			want:  "iris request failed: Authorization: [redacted] after",
		},
		{
			name:  "bearer token with percent encoding",
			input: "iris auth failed: Bearer ab%2Fcd!xyz tail",
			want:  "iris auth failed: [redacted] tail",
		},
		{
			name:  "key value token",
			input: "config invalid: auth_token=supervalue rest",
			want:  "config invalid: auth_token=[redacted] rest",
		},
		{
			name:  "api key header",
			input: "header rejected: X-Api-Key: abc123 tail",
			want:  "header rejected: X-Api-Key=[redacted] tail",
		},
		{
			name:  "url query string",
			input: "post https://iris.internal/reply?auth=abc&room=123 returned 500",
			want:  "post https://iris.internal/reply?[redacted] returned 500",
		},
		{
			name:  "long quoted payload",
			input: `decode failed for "` + strings.Repeat("x", 70) + `" at offset 3`,
			want:  `decode failed for "[redacted]" at offset 3`,
		},
		{
			name:  "plain span between two quoted strings survives",
			input: `decode "alpha" failed: ` + strings.Repeat("y", 70) + ` near "beta"`,
			want:  `decode "alpha" failed: ` + strings.Repeat("y", 70) + ` near "beta"`,
		},
		{
			name:  "invalid utf8 replaced before storage",
			input: "payload \xff\xfe broken",
			want:  "payload � broken",
		},
		{
			name:  "plain message unchanged",
			input: "iris https://iris.internal/reply returned 502",
			want:  "iris https://iris.internal/reply returned 502",
		},
		{
			name:  "empty stays empty",
			input: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeStoredError(tc.input)
			if got != tc.want {
				t.Fatalf("sanitizeStoredError(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if again := sanitizeStoredError(got); again != got {
				t.Fatalf("sanitize is not idempotent: first %q, second %q", got, again)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("sanitizeStoredError(%q) produced invalid UTF-8", tc.input)
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
