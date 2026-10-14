package collecterr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestERR004OperationCodeCannotPersistToFailureColumns(t *testing.T) {
	t.Parallel()

	for _, code := range AllOperationCodes() {
		if contract.CollectionErrorCode(code).Valid() {
			t.Fatalf("operation code %q is also a CollectionErrorCode", code)
		}

		if _, err := contract.NewFailureDiagnostic(contract.CollectionErrorCode(code), contract.ClassTransient, "operation"); err == nil {
			t.Fatalf("NewFailureDiagnostic accepted operation code %q", code)
		}
	}

	for _, leftover := range []string{"pagination_gap", "lease_lost", "lease_not_acquired"} {
		if contract.CollectionErrorCode(leftover).Valid() {
			t.Fatalf("%q must not be a CollectionErrorCode", leftover)
		}

		if OperationCode(leftover).Valid() {
			t.Fatalf("%q must not be an OperationCode", leftover)
		}
	}
}

func TestERR005UnknownErrorNormalizesToInternalInvariant(t *testing.T) {
	t.Parallel()

	err := errors.New("mystery helper boom")
	if CodeOf(err) != contract.ErrorInternalInvariant || ClassOf(err) != contract.ClassInternal {
		t.Fatalf("CodeOf/ClassOf = %s/%s", CodeOf(err), ClassOf(err))
	}

	if normalized := Normalize(err); normalized == nil || normalized.code != contract.ErrorInternalInvariant || normalized.class != contract.ClassInternal {
		t.Fatalf("Normalize = %#v", normalized)
	}

	if CodeOf(FromContext(err)) != contract.ErrorInternalInvariant {
		t.Fatal("FromContext must not guess TRANSIENT")
	}
}

func TestERR006WrapWithRetryNormalizePreserveErrorChain(t *testing.T) {
	t.Parallel()

	cause := errors.New("root-cause")
	wrapped := Wrap(contract.ErrorCollectionFailed, contract.ClassTransient, cause)

	if !errors.Is(wrapped, cause) {
		t.Fatal("Wrap lost cause")
	}

	hint, err := NewRetryAfterHint(time.Second)
	if err != nil {
		t.Fatal(err)
	}

	retried := WithRetry(wrapped, hint)
	if !errors.Is(retried, cause) {
		t.Fatal("WithRetry lost cause")
	}

	if _, ok := errors.AsType[*Error](retried); !ok {
		t.Fatal("errors.As lost typed error")
	}

	if normalized := Normalize(retried); normalized == nil || !errors.Is(normalized, cause) {
		t.Fatal("Normalize lost cause")
	}
}

func TestERR007RejectsImpossibleCodeClassRetryDiagnosticTuple(t *testing.T) {
	t.Parallel()

	// 알려진 code라도 계약 밖 class는 기본 class로 고치지 않고 Internal로 닫으며 원래 tuple을 원인에 남긴다.
	err := New(contract.ErrorCollectionFailed, contract.ClassTimeout, "impossible")
	if CodeOf(err) != contract.ErrorInternalInvariant || ClassOf(err) != contract.ClassInternal {
		t.Fatalf("known code with impossible class = %s/%s, want fail-closed internal", CodeOf(err), ClassOf(err))
	}

	if !strings.Contains(err.Error(), "invalid collection failure tuple collection_failed/TIMEOUT") {
		t.Fatalf("impossible tuple error = %q, want original tuple preserved", err.Error())
	}

	unknown := New(contract.CollectionErrorCode("not_a_real_code"), contract.ClassTransient, "unknown")
	if CodeOf(unknown) != contract.ErrorInternalInvariant || ClassOf(unknown) != contract.ClassInternal {
		t.Fatalf("unknown code = %s/%s", CodeOf(unknown), ClassOf(unknown))
	}

	invalidHint := RetryHint{kind: RetryAfter, after: -time.Second}
	if CodeOf(WithRetry(New(contract.ErrorCollectionFailed, contract.ClassTransient, "ok"), invalidHint)) != contract.ErrorInternalInvariant {
		t.Fatal("invalid retry hint must close to INTERNAL")
	}

	if _, diagErr := contract.NewFailureDiagnostic(contract.ErrorCollectionFailed, contract.ClassTimeout, "impossible"); diagErr == nil {
		t.Fatal("shared constructor must reject impossible tuple")
	}
}

func TestERR008ContextDeadlineAndCancelMapping(t *testing.T) {
	t.Parallel()

	if CodeOf(context.DeadlineExceeded) != contract.ErrorCollectionTimeout || ClassOf(context.DeadlineExceeded) != contract.ClassTimeout {
		t.Fatal("deadline mapping")
	}

	if CodeOf(context.Canceled) != contract.ErrorCollectionCanceled || ClassOf(context.Canceled) != contract.ClassCanceled {
		t.Fatal("cancel mapping")
	}

	if CodeOf(FromContext(context.DeadlineExceeded)) != contract.ErrorCollectionTimeout {
		t.Fatal("FromContext deadline")
	}

	if CodeOf(FromContext(context.Canceled)) != contract.ErrorCollectionCanceled {
		t.Fatal("FromContext cancel")
	}
}

func TestERR009RetryAfterDeltaSecondsAndHTTPDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	delta := FromStatus("holodex", http.StatusTooManyRequests, "12", now)

	if CodeOf(delta) != contract.ErrorCooldown || RetryOf(delta).Kind() != RetryAfter || RetryOf(delta).After() != 12*time.Second {
		t.Fatalf("delta-seconds hint = %s/%v", CodeOf(delta), RetryOf(delta))
	}

	httpDate := now.Add(30 * time.Second).UTC().Format(http.TimeFormat)
	dated := FromStatus("holodex", http.StatusTooManyRequests, httpDate, now)

	if RetryOf(dated).Kind() != RetryAt || !RetryOf(dated).At().Equal(now.Add(30*time.Second)) {
		t.Fatalf("HTTP-date hint = %#v", RetryOf(dated))
	}

	overflow := FromStatus("holodex", http.StatusTooManyRequests, "2147483648", now)
	if CodeOf(overflow) != contract.ErrorCooldown || RetryOf(overflow).Kind() != RetryDefault {
		t.Fatalf("overflow = %s/%v", CodeOf(overflow), RetryOf(overflow))
	}

	invalid := FromStatus("holodex", http.StatusTooManyRequests, "not-a-date", now)
	if CodeOf(invalid) != contract.ErrorCooldown || RetryOf(invalid).Kind() != RetryDefault {
		t.Fatalf("invalid = %s/%v", CodeOf(invalid), RetryOf(invalid))
	}

	zero := FromStatus("holodex", http.StatusTooManyRequests, "0", now)
	if RetryOf(zero).Kind() != RetryDefault {
		t.Fatalf("zero delta = %v", RetryOf(zero))
	}

	svc := FromStatus("holodex", http.StatusServiceUnavailable, "5", now)
	if CodeOf(svc) != contract.ErrorCooldown || RetryOf(svc).Kind() != RetryAfter {
		t.Fatalf("503 + Retry-After = %s/%v", CodeOf(svc), RetryOf(svc))
	}

	bare503 := FromStatus("holodex", http.StatusServiceUnavailable, "", now)
	if CodeOf(bare503) != contract.ErrorCollectionFailed || ClassOf(bare503) != contract.ClassTransient {
		t.Fatalf("503 without Retry-After = %s/%s", CodeOf(bare503), ClassOf(bare503))
	}
}

func TestERR011SanitizeDetailRedactsSecretsBoundsUTF8AndIsIdempotent(t *testing.T) {
	t.Parallel()

	const (
		bearer   = "bearer-secret"
		query    = "query-secret"
		userinfo = "userinfo-secret"
	)

	detail := "Authorization: Bearer " + bearer +
		" https://example.test/path?token=" + query +
		" postgres://user:" + userinfo + "@example.test/db " +
		strings.Repeat("가", MaxDetailBytes)
	got := SanitizeDetail(detail)

	for _, secret := range []string{bearer, query, userinfo} {
		if strings.Contains(got, secret) {
			t.Fatalf("SanitizeDetail leaked %q: %q", secret, got)
		}
	}

	if len(got) > MaxDetailBytes {
		t.Fatalf("SanitizeDetail length = %d", len(got))
	}

	if !utf8.ValidString(got) {
		t.Fatal("SanitizeDetail returned invalid UTF-8")
	}

	if again := SanitizeDetail(got); again != got {
		t.Fatalf("SanitizeDetail is not idempotent: %q vs %q", again, got)
	}

	invalid := SanitizeDetail("prefix \xff\xfe suffix")
	if !utf8.ValidString(invalid) || !strings.Contains(invalid, "�") {
		t.Fatalf("invalid UTF-8 result = %q", invalid)
	}
}

func TestERR015NewWriterRejectsLegacyHelperClassNames(t *testing.T) {
	t.Parallel()

	err := New(contract.ErrorCollectionFailed, contract.FailureClass("InnertubeError"), "innertube down")
	if CodeOf(err) != contract.ErrorInternalInvariant || ClassOf(err) != contract.ClassInternal {
		t.Fatalf("legacy helper class = %s/%s, want fail-closed internal", CodeOf(err), ClassOf(err))
	}

	if _, diagErr := contract.NewFailureDiagnostic(contract.ErrorCollectionFailed, contract.FailureClass("InnertubeError"), "innertube down"); diagErr == nil {
		t.Fatal("new writer must not persist InnertubeError")
	}
}

func TestERR015bKnownCodeLegacyClassFailsClosed(t *testing.T) {
	t.Parallel()

	// 구 helper class 이름을 code 기본 class로 수리하던 경로는 지웠다. 알려진 code라도 durable 진단은 Internal이다.
	parser := New(contract.ErrorParserDrift, contract.FailureClass("RpcResponseError"), "parser drift")
	if CodeOf(parser) != contract.ErrorInternalInvariant || ClassOf(parser) != contract.ClassInternal {
		t.Fatalf("parser_drift+RpcResponseError = %s/%s, want fail-closed internal", CodeOf(parser), ClassOf(parser))
	}

	if DiagnosticOf(parser).Code() != contract.ErrorInternalInvariant || DiagnosticOf(parser).Class() != contract.ClassInternal {
		t.Fatalf("DiagnosticOf parser = %s/%s", DiagnosticOf(parser).Code(), DiagnosticOf(parser).Class())
	}

	failed := New(contract.ErrorCollectionFailed, contract.FailureClass("InnertubeError"), "innertube down")
	if DiagnosticOf(failed).Code() != contract.ErrorInternalInvariant || DiagnosticOf(failed).Class() != contract.ClassInternal {
		t.Fatalf("DiagnosticOf failed = %s/%s", DiagnosticOf(failed).Code(), DiagnosticOf(failed).Class())
	}

	leftover := New(contract.CollectionErrorCode("pagination_gap"), contract.FailureClass("HelperError"), "gap")
	if CodeOf(leftover) != contract.ErrorInternalInvariant || DiagnosticOf(leftover).Code() != contract.ErrorInternalInvariant {
		t.Fatalf("leftover Normalize/DiagnosticOf = %s/%s", CodeOf(leftover), DiagnosticOf(leftover).Code())
	}
}

func TestERR016EmptySanitizedDetailFallsBackToCodeString(t *testing.T) {
	t.Parallel()

	err := New(contract.ErrorCollectionFailed, contract.ClassTransient, "")
	diagnostic := DiagnosticOf(err)

	if err := diagnostic.Validate(); err != nil {
		t.Fatal(err)
	}

	if diagnostic.Detail() != string(contract.ErrorCollectionFailed) {
		t.Fatalf("detail = %q, want code fallback", diagnostic.Detail())
	}

	if diagnostic.Code() == "" || diagnostic.Class() == "" {
		t.Fatal("diagnostic must be all-present")
	}
}

func TestRecognizedNetworkNormalizesToTransient(t *testing.T) {
	t.Parallel()

	err := &net.OpError{Op: "read", Err: syscall.ECONNRESET}
	if CodeOf(err) != contract.ErrorCollectionFailed || ClassOf(err) != contract.ClassTransient {
		t.Fatalf("reset = %s/%s", CodeOf(err), ClassOf(err))
	}

	if CodeOf(io.EOF) != contract.ErrorCollectionFailed || ClassOf(io.EOF) != contract.ClassTransient {
		t.Fatalf("EOF = %s/%s", CodeOf(io.EOF), ClassOf(io.EOF))
	}
}

// net/http client가 돌려준 수송 계층 타임아웃(response header·dial·Client.Timeout)은 *url.Error의 Timeout()으로
// 드러난다. 이를 TIMEOUT으로 분류해야 운영 로그가 외부 지연을 내부 불변식 위반으로 기록하지 않는다.
func TestERR012TransportTimeoutClassifiesAsCollectionTimeout(t *testing.T) {
	t.Parallel()

	transportTimeout := &url.Error{Op: "Get", URL: "https://holodex.example/api/v2/live", Err: os.ErrDeadlineExceeded}

	for name, err := range map[string]error{
		"normalize":   transportTimeout,
		"fromContext": FromContext(transportTimeout),
		"wrapped":     fmt.Errorf("request holodex live: %w", transportTimeout),
	} {
		if CodeOf(err) != contract.ErrorCollectionTimeout || ClassOf(err) != contract.ClassTimeout {
			t.Fatalf("%s: CodeOf/ClassOf = %s/%s, want %s/%s", name, CodeOf(err), ClassOf(err), contract.ErrorCollectionTimeout, contract.ClassTimeout)
		}

		if IsUnclassified(err) {
			t.Fatalf("%s: IsUnclassified = true, want explicit timeout", name)
		}
	}

	// HTTP 요청 밖의 socket deadline과 타임아웃이 아닌 *url.Error는 기존대로 미분류다.
	for name, err := range map[string]error{
		"bare socket deadline":      os.ErrDeadlineExceeded,
		"url error without timeout": &url.Error{Op: "Get", URL: "https://holodex.example", Err: errors.New("unexpected EOF reading trailer")},
	} {
		if !IsUnclassified(FromContext(err)) {
			t.Fatalf("%s: IsUnclassified(FromContext) = false, want true", name)
		}
	}
}
