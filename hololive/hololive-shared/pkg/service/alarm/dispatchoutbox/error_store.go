package dispatchoutbox

import (
	"context"
	"errors"
	"net"
	"regexp"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/iris-client-go/iris"
)

const (
	ErrorCodeTimeout  = "timeout"
	ErrorCodeCanceled = "canceled"
	ErrorCodeHTTP4xx  = "http_4xx"
	ErrorCodeHTTP5xx  = "http_5xx"
	ErrorCodeNetwork  = "network"
	ErrorCodePG       = "pg"
	ErrorCodePayload  = "payload"
	ErrorCodeUnknown  = "unknown"
)

const maxStoredErrorBytes = 2048

// bearer 패턴이 kv 패턴보다 먼저 돌아야 "Authorization: Bearer x"에서 값 잔여물이 남지 않는다.
var (
	storedErrorBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	storedErrorKVPattern     = regexp.MustCompile(`(?i)\b([\w.-]*(?:authorization|token|secret|password|passwd|credential|cookie|signature)|api[-_]?key|apikey)["']?\s*[:=]\s*["']?[^\s"',;)\]}]+`)
	storedErrorQueryPattern  = regexp.MustCompile(`\?[^?\s"']*=[^?\s"']*`)
	storedErrorQuotedPattern = regexp.MustCompile(`"[^"]{65,}"`)
)

func sanitizeStoredError(message string) string {
	message = storedErrorBearerPattern.ReplaceAllString(message, "[redacted]")
	message = storedErrorKVPattern.ReplaceAllString(message, "${1}=[redacted]")
	message = storedErrorQueryPattern.ReplaceAllString(message, "?[redacted]")
	message = storedErrorQuotedPattern.ReplaceAllString(message, `"[redacted]"`)
	return truncateStoredError(message)
}

func truncateStoredError(message string) string {
	if len(message) <= maxStoredErrorBytes {
		return message
	}
	cut := maxStoredErrorBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}

func storedErrorFromCause(cause error) (message, code string) {
	if cause == nil {
		return "", ""
	}
	return sanitizeStoredError(cause.Error()), ClassifyErrorCode(cause)
}

func ClassifyErrorCode(cause error) string {
	if cause == nil {
		return ""
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return ErrorCodeTimeout
	}
	if errors.Is(cause, context.Canceled) {
		return ErrorCodeCanceled
	}
	var httpErr *iris.HTTPError
	if errors.As(cause, &httpErr) {
		return classifyHTTPStatus(httpErr.StatusCode)
	}
	return classifyInfraErrorCode(cause)
}

func classifyInfraErrorCode(cause error) string {
	var pgErr *pgconn.PgError
	if errors.As(cause, &pgErr) {
		return ErrorCodePG
	}
	var pgConnectErr *pgconn.ConnectError
	if errors.As(cause, &pgConnectErr) {
		return ErrorCodePG
	}
	var netErr net.Error
	if errors.As(cause, &netErr) {
		return ErrorCodeNetwork
	}
	if errors.Is(cause, iris.ErrTransport) {
		return ErrorCodeNetwork
	}
	return ErrorCodeUnknown
}

func classifyHTTPStatus(status int) string {
	switch {
	case status >= 500:
		return ErrorCodeHTTP5xx
	case status >= 400:
		return ErrorCodeHTTP4xx
	default:
		return ErrorCodeUnknown
	}
}
