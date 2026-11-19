package collecterr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	UnknownClass   = "unknown_error"
	MaxDetailBytes = 2048
)

var ErrInvalidFailureTuple = errors.New("invalid collection failure tuple")

type Error struct {
	code         contract.CollectionErrorCode
	class        contract.FailureClass
	retry        RetryHint
	err          error
	unclassified bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	if e.err == nil {
		return string(e.code)
	}

	return e.err.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.err
}

func New(code contract.CollectionErrorCode, class contract.FailureClass, message string) error {
	return newError(code, class, defaultRetryHint(), fmt.Errorf("%s", message))
}

func Wrap(code contract.CollectionErrorCode, class contract.FailureClass, err error) error {
	if err == nil {
		return nil
	}

	return newError(code, class, defaultRetryHint(), err)
}

func WithRetry(err error, hint RetryHint) error {
	if err == nil {
		return nil
	}

	normalized := Normalize(err)

	if hint.Validate() != nil {
		degraded := newError(contract.ErrorInternalInvariant, contract.ClassInternal, defaultRetryHint(), err)

		degraded.unclassified = normalized.unclassified

		return degraded
	}

	return &Error{
		code:         normalized.code,
		class:        normalized.class,
		retry:        hint,
		err:          err,
		unclassified: normalized.unclassified,
	}
}

func Normalize(err error) *Error {
	if err == nil {
		return nil
	}

	if typed, ok := errors.AsType[*Error](err); ok && typed != nil {
		return typed.normalized()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{code: contract.ErrorCollectionTimeout, class: contract.ClassTimeout, retry: defaultRetryHint(), err: err}
	}

	if errors.Is(err, context.Canceled) {
		return &Error{code: contract.ErrorCollectionCanceled, class: contract.ClassCanceled, retry: defaultRetryHint(), err: err}
	}

	if recognizedTransportTimeout(err) {
		return &Error{code: contract.ErrorCollectionTimeout, class: contract.ClassTimeout, retry: defaultRetryHint(), err: err}
	}

	if recognizedTransientNetwork(err) {
		return &Error{code: contract.ErrorCollectionFailed, class: contract.ClassTransient, retry: defaultRetryHint(), err: err}
	}

	return &Error{code: contract.ErrorInternalInvariant, class: contract.ClassInternal, retry: defaultRetryHint(), err: err, unclassified: true}
}

func CodeOf(err error) contract.CollectionErrorCode {
	if normalized := Normalize(err); normalized != nil {
		return normalized.code
	}

	return contract.ErrorInternalInvariant
}

func ClassOf(err error) contract.FailureClass {
	if normalized := Normalize(err); normalized != nil {
		return normalized.class
	}

	return contract.ClassInternal
}

func RetryOf(err error) RetryHint {
	if normalized := Normalize(err); normalized != nil {
		return normalized.retry
	}

	return defaultRetryHint()
}

func DiagnosticOf(err error) contract.FailureDiagnostic {
	normalized := Normalize(err)
	if normalized == nil {
		diagnostic, diagErr := contract.NewFailureDiagnostic(contract.ErrorInternalInvariant, contract.ClassInternal, string(contract.ErrorInternalInvariant))
		if diagErr != nil {
			return contract.FailureDiagnostic{}
		}

		return diagnostic
	}

	detail := SanitizeDetail(normalized.Error())
	if strings.TrimSpace(detail) == "" {
		detail = string(normalized.code)
	}

	diagnostic, diagErr := contract.NewFailureDiagnostic(normalized.code, normalized.class, detail)
	if diagErr != nil {
		fallback, fallbackErr := contract.NewFailureDiagnostic(contract.ErrorInternalInvariant, contract.ClassInternal, string(contract.ErrorInternalInvariant))
		if fallbackErr != nil {
			return contract.FailureDiagnostic{}
		}

		return fallback
	}

	return diagnostic
}

func SanitizeDetail(detail string) string {
	detail = strings.ToValidUTF8(detail, "�")
	detail = sharedlogging.RedactDiagnostic(detail)

	if len(detail) <= MaxDetailBytes {
		return detail
	}

	cut := MaxDetailBytes
	for cut > 0 && !utf8.RuneStart(detail[cut]) {
		cut--
	}

	return detail[:cut]
}

func FromContext(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return Wrap(contract.ErrorCollectionTimeout, contract.ClassTimeout, err)
	}

	if errors.Is(err, context.Canceled) {
		return Wrap(contract.ErrorCollectionCanceled, contract.ClassCanceled, err)
	}

	if recognizedTransportTimeout(err) {
		return Wrap(contract.ErrorCollectionTimeout, contract.ClassTimeout, err)
	}

	if recognizedTransientNetwork(err) {
		return Wrap(contract.ErrorCollectionFailed, contract.ClassTransient, err)
	}

	return unclassifiedError(err)
}

func unclassifiedError(err error) error {
	typed := newError(contract.ErrorInternalInvariant, contract.ClassInternal, defaultRetryHint(), err)

	typed.unclassified = true

	return typed
}

func IsUnclassified(err error) bool {
	normalized := Normalize(err)

	return normalized != nil && normalized.unclassified
}

func newError(code contract.CollectionErrorCode, class contract.FailureClass, hint RetryHint, cause error) *Error {
	typed := &Error{code: code, class: class, retry: hint, err: cause}
	if hint.Validate() != nil {
		typed.code = contract.ErrorInternalInvariant
		typed.class = contract.ClassInternal
		typed.retry = defaultRetryHint()

		return typed
	}

	if contract.ValidDurableFailureTuple(code, class) {
		return typed
	}

	typed.code = contract.ErrorInternalInvariant
	typed.class = contract.ClassInternal
	typed.retry = defaultRetryHint()
	typed.err = fmt.Errorf("%w %s/%s: %w", ErrInvalidFailureTuple, code, class, cause)
	typed.unclassified = true

	return typed
}

func (e *Error) normalized() *Error {
	if e == nil {
		return nil
	}

	hint := e.retry
	if hint.Validate() != nil {
		hint = defaultRetryHint()
	}

	if contract.ValidDurableFailureTuple(e.code, e.class) {
		return &Error{code: e.code, class: e.class, retry: hint, err: e.err, unclassified: e.unclassified}
	}

	if errors.Is(e.err, context.DeadlineExceeded) {
		return &Error{code: contract.ErrorCollectionTimeout, class: contract.ClassTimeout, retry: defaultRetryHint(), err: e.err}
	}

	if errors.Is(e.err, context.Canceled) {
		return &Error{code: contract.ErrorCollectionCanceled, class: contract.ClassCanceled, retry: defaultRetryHint(), err: e.err}
	}

	return &Error{code: contract.ErrorInternalInvariant, class: contract.ClassInternal, retry: defaultRetryHint(), err: e.err, unclassified: e.unclassified}
}

func recognizedTransportTimeout(err error) bool {
	if urlErr, ok := errors.AsType[*url.Error](err); ok && urlErr != nil && urlErr.Timeout() {
		return true
	}

	opErr, ok := errors.AsType[*net.OpError](err)

	return ok && opErr != nil && opErr.Op == "dial" && opErr.Timeout()
}

func recognizedTransientNetwork(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return true
	}

	if transientPostgresError(err) {
		return true
	}

	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH)
}

func transientPostgresError(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr == nil {
		return false
	}

	switch pgErr.Code {
	case "57P01", "57P02", "57P03":
		return true
	default:
		return strings.HasPrefix(pgErr.Code, "08")
	}
}
