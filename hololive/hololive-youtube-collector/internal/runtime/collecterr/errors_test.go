package collecterr

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
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
	if CodeOf(err) != Internal || ClassOf(err) != ClassInternal {
		t.Fatalf("CodeOf/ClassOf = %s/%s", CodeOf(err), ClassOf(err))
	}

	if normalized := Normalize(err); normalized == nil || normalized.code != Internal || normalized.class != ClassInternal {
		t.Fatalf("Normalize = %#v", normalized)
	}

	if CodeOf(FromContext(err)) != Internal {
		t.Fatal("FromContext must not guess TRANSIENT")
	}
}

func TestERR006WrapWithRetryNormalizePreserveErrorChain(t *testing.T) {
	t.Parallel()

	cause := errors.New("root-cause")
	wrapped := Wrap(Failed, ClassTransient, cause)

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

	err := New(Failed, ClassTimeout, "impossible")
	if CodeOf(err) != Failed || ClassOf(err) != ClassTransient {
		t.Fatalf("known code with impossible class = %s/%s", CodeOf(err), ClassOf(err))
	}

	unknown := New(ErrorCode("not_a_real_code"), ClassTransient, "unknown")
	if CodeOf(unknown) != Internal || ClassOf(unknown) != ClassInternal {
		t.Fatalf("unknown code = %s/%s", CodeOf(unknown), ClassOf(unknown))
	}

	invalidHint := RetryHint{kind: RetryAfter, after: -time.Second}
	if CodeOf(WithRetry(New(Failed, ClassTransient, "ok"), invalidHint)) != Internal {
		t.Fatal("invalid retry hint must close to INTERNAL")
	}

	if _, diagErr := contract.NewFailureDiagnostic(Failed, ClassTimeout, "impossible"); diagErr == nil {
		t.Fatal("shared constructor must reject impossible tuple")
	}
}

func TestERR008ContextDeadlineAndCancelMapping(t *testing.T) {
	t.Parallel()

	if CodeOf(context.DeadlineExceeded) != Timeout || ClassOf(context.DeadlineExceeded) != ClassTimeout {
		t.Fatal("deadline mapping")
	}

	if CodeOf(context.Canceled) != Canceled || ClassOf(context.Canceled) != ClassCanceled {
		t.Fatal("cancel mapping")
	}

	if CodeOf(FromContext(context.DeadlineExceeded)) != Timeout {
		t.Fatal("FromContext deadline")
	}

	if CodeOf(FromContext(context.Canceled)) != Canceled {
		t.Fatal("FromContext cancel")
	}
}

func TestERR009RetryAfterDeltaSecondsAndHTTPDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	delta := FromStatus("holodex", http.StatusTooManyRequests, "12", now)

	if CodeOf(delta) != Cooldown || RetryOf(delta).Kind() != RetryAfter || RetryOf(delta).After() != 12*time.Second {
		t.Fatalf("delta-seconds hint = %s/%v", CodeOf(delta), RetryOf(delta))
	}

	httpDate := now.Add(30 * time.Second).UTC().Format(http.TimeFormat)
	dated := FromStatus("holodex", http.StatusTooManyRequests, httpDate, now)

	if RetryOf(dated).Kind() != RetryAt || !RetryOf(dated).At().Equal(now.Add(30*time.Second)) {
		t.Fatalf("HTTP-date hint = %#v", RetryOf(dated))
	}

	overflow := FromStatus("holodex", http.StatusTooManyRequests, "2147483648", now)
	if CodeOf(overflow) != Cooldown || RetryOf(overflow).Kind() != RetryDefault {
		t.Fatalf("overflow = %s/%v", CodeOf(overflow), RetryOf(overflow))
	}

	invalid := FromStatus("holodex", http.StatusTooManyRequests, "not-a-date", now)
	if CodeOf(invalid) != Cooldown || RetryOf(invalid).Kind() != RetryDefault {
		t.Fatalf("invalid = %s/%v", CodeOf(invalid), RetryOf(invalid))
	}

	zero := FromStatus("holodex", http.StatusTooManyRequests, "0", now)
	if RetryOf(zero).Kind() != RetryDefault {
		t.Fatalf("zero delta = %v", RetryOf(zero))
	}

	svc := FromStatus("holodex", http.StatusServiceUnavailable, "5", now)
	if CodeOf(svc) != Cooldown || RetryOf(svc).Kind() != RetryAfter {
		t.Fatalf("503 + Retry-After = %s/%v", CodeOf(svc), RetryOf(svc))
	}

	bare503 := FromStatus("holodex", http.StatusServiceUnavailable, "", now)
	if CodeOf(bare503) != Failed || ClassOf(bare503) != ClassTransient {
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

	err := New(Failed, FailureClass("InnertubeError"), "innertube down")
	if CodeOf(err) != Failed || ClassOf(err) != ClassTransient {
		t.Fatalf("legacy class normalization = %s/%s", CodeOf(err), ClassOf(err))
	}

	if _, diagErr := contract.NewFailureDiagnostic(Failed, FailureClass("InnertubeError"), "innertube down"); diagErr == nil {
		t.Fatal("new writer must not persist InnertubeError")
	}
}

func TestERR015bKnownCodeLegacyClassKeepsClosedCode(t *testing.T) {
	t.Parallel()

	parser := New(ParserDrift, FailureClass("RpcResponseError"), "parser drift")
	if CodeOf(parser) != ParserDrift || ClassOf(parser) != ClassDataContract {
		t.Fatalf("parser_drift+RpcResponseError = %s/%s", CodeOf(parser), ClassOf(parser))
	}

	if DiagnosticOf(parser).Code() != ParserDrift || DiagnosticOf(parser).Class() != ClassDataContract {
		t.Fatalf("DiagnosticOf parser = %s/%s", DiagnosticOf(parser).Code(), DiagnosticOf(parser).Class())
	}

	failed := New(Failed, FailureClass("InnertubeError"), "innertube down")
	if CodeOf(failed) != Failed || ClassOf(failed) != ClassTransient {
		t.Fatalf("collection_failed+InnertubeError = %s/%s", CodeOf(failed), ClassOf(failed))
	}

	if DiagnosticOf(failed).Code() != Failed || DiagnosticOf(failed).Class() != ClassTransient {
		t.Fatalf("DiagnosticOf failed = %s/%s", DiagnosticOf(failed).Code(), DiagnosticOf(failed).Class())
	}

	leftover := New(ErrorCode("pagination_gap"), FailureClass("HelperError"), "gap")
	if CodeOf(leftover) != Internal || DiagnosticOf(leftover).Code() != Internal {
		t.Fatalf("leftover Normalize/DiagnosticOf = %s/%s", CodeOf(leftover), DiagnosticOf(leftover).Code())
	}
}

func TestERR016EmptySanitizedDetailFallsBackToCodeString(t *testing.T) {
	t.Parallel()

	err := New(Failed, ClassTransient, "")
	diagnostic := DiagnosticOf(err)

	if err := diagnostic.Validate(); err != nil {
		t.Fatal(err)
	}

	if diagnostic.Detail() != string(Failed) {
		t.Fatalf("detail = %q, want code fallback", diagnostic.Detail())
	}

	if diagnostic.Code() == "" || diagnostic.Class() == "" {
		t.Fatal("diagnostic must be all-present")
	}
}

func TestRecognizedNetworkNormalizesToTransient(t *testing.T) {
	t.Parallel()

	err := &net.OpError{Op: "read", Err: syscall.ECONNRESET}
	if CodeOf(err) != Failed || ClassOf(err) != ClassTransient {
		t.Fatalf("reset = %s/%s", CodeOf(err), ClassOf(err))
	}

	if CodeOf(io.EOF) != Failed || ClassOf(io.EOF) != ClassTransient {
		t.Fatalf("EOF = %s/%s", CodeOf(io.EOF), ClassOf(io.EOF))
	}
}
