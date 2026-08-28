package dispatchoutbox

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/iris-client-go/v2/iris"
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
// KV 패턴은 대괄호로 감싼 값 전체를 먼저 소비해 bracket-prefixed secret도 남김없이 지운다.
// 치환 산출물 [redacted]도 같은 분기로 다시 치환되므로 결과는 멱등이고,
// quoted 패턴의 opener 앞글자 제한은 닫는 quote를 여는 quote로 오인해
// 두 인용구 사이 평문까지 지우는 것을 막는다.
var (
	storedErrorBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=%!-]+`)
	storedErrorKVPattern     = regexp.MustCompile(`(?i)\b([\w.-]*(?:authorization|token|secret|password|passwd|credential|cookie|signature)|api[-_]?key|apikey)["']?\s*[:=]\s*["']?(?:\[[^\s"',;)}]+|[^\s"',;)\]}]+)`)
	storedErrorQueryPattern  = regexp.MustCompile(`\?[^?\s"']*=[^?\s"']*`)
	storedErrorQuotedPattern = regexp.MustCompile(`(^|[\s:=,(\[])"[^"]{65,}"`)
)

func sanitizeStoredError(message string) string {
	message = strings.ToValidUTF8(message, "�")
	message = storedErrorBearerPattern.ReplaceAllString(message, "[redacted]")
	message = storedErrorKVPattern.ReplaceAllStringFunc(message, func(match string) string {
		if strings.HasSuffix(strings.ToLower(match), "[redacted]") {
			return match
		}

		parts := storedErrorKVPattern.FindStringSubmatch(match)

		return parts[1] + "=[redacted]"
	})
	message = storedErrorQueryPattern.ReplaceAllString(message, "?[redacted]")
	message = storedErrorQuotedPattern.ReplaceAllString(message, `${1}"[redacted]"`)

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

	if httpErr, ok := errors.AsType[*iris.HTTPError](cause); ok {
		return classifyHTTPStatus(httpErr.StatusCode)
	}

	return classifyInfraErrorCode(cause)
}

func classifyInfraErrorCode(cause error) string {
	if _, ok := errors.AsType[*pgconn.PgError](cause); ok {
		return ErrorCodePG
	}

	if _, ok := errors.AsType[*pgconn.ConnectError](cause); ok {
		return ErrorCodePG
	}

	if _, ok := errors.AsType[net.Error](cause); ok {
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
