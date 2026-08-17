package collecterr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"unicode/utf8"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

type ErrorCode = contract.CollectionErrorCode
type FailureClass = contract.FailureClass

const (
	Timeout                = contract.ErrorCollectionTimeout
	Canceled               = contract.ErrorCollectionCanceled
	ParserDrift            = contract.ErrorParserDrift
	Cooldown               = contract.ErrorCooldown
	PublishRejected        = contract.ErrorPublishRejected
	Failed                 = contract.ErrorCollectionFailed
	ResponseTooLarge       = contract.ErrorResponseTooLarge
	Configuration          = contract.ErrorConfiguration
	HelperBusy             = contract.ErrorHelperBusy
	HelperProtocolMismatch = contract.ErrorHelperProtocolMismatch
	Internal               = contract.ErrorInternalInvariant
	TargetRosterTooLarge   = contract.ErrorTargetRosterTooLarge

	ClassTransient     = contract.ClassTransient
	ClassTimeout       = contract.ClassTimeout
	ClassCanceled      = contract.ClassCanceled
	ClassCooldown      = contract.ClassCooldown
	ClassDataContract  = contract.ClassDataContract
	ClassResourceLimit = contract.ClassResourceLimit
	ClassConfiguration = contract.ClassConfiguration
	ClassProtocol      = contract.ClassProtocol
	ClassSuperseded    = contract.ClassSuperseded
	ClassInternal      = contract.ClassInternal

	UnknownClass   = "unknown_error"
	MaxDetailBytes = 2048
)

type Error struct {
	code  ErrorCode
	class FailureClass
	retry RetryHint
	err   error
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

func New(code ErrorCode, class FailureClass, message string) error {
	return newError(code, class, defaultRetryHint(), fmt.Errorf("%s", message))
}

func Wrap(code ErrorCode, class FailureClass, err error) error {
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
		return newError(Internal, ClassInternal, defaultRetryHint(), err)
	}
	return &Error{code: normalized.code, class: normalized.class, retry: hint, err: err}
}

func Normalize(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return typed.normalized()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{code: Timeout, class: ClassTimeout, retry: defaultRetryHint(), err: err}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{code: Canceled, class: ClassCanceled, retry: defaultRetryHint(), err: err}
	}
	if recognizedTransientNetwork(err) {
		return &Error{code: Failed, class: ClassTransient, retry: defaultRetryHint(), err: err}
	}
	return &Error{code: Internal, class: ClassInternal, retry: defaultRetryHint(), err: err}
}

func CodeOf(err error) ErrorCode {
	if normalized := Normalize(err); normalized != nil {
		return normalized.code
	}
	return Internal
}

func ClassOf(err error) FailureClass {
	if normalized := Normalize(err); normalized != nil {
		return normalized.class
	}
	return ClassInternal
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
		diagnostic, diagErr := contract.NewFailureDiagnostic(Internal, ClassInternal, string(Internal))
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
		fallback, fallbackErr := contract.NewFailureDiagnostic(Internal, ClassInternal, string(Internal))
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
		return Wrap(Timeout, ClassTimeout, err)
	}
	if errors.Is(err, context.Canceled) {
		return Wrap(Canceled, ClassCanceled, err)
	}
	if recognizedTransientNetwork(err) {
		return Wrap(Failed, ClassTransient, err)
	}
	return Wrap(Internal, ClassInternal, err)
}

func newError(code ErrorCode, class FailureClass, hint RetryHint, cause error) *Error {
	typed := &Error{code: code, class: class, retry: hint, err: cause}
	if hint.Validate() != nil {
		typed.code = Internal
		typed.class = ClassInternal
		typed.retry = defaultRetryHint()
		return typed
	}
	if repaired, ok := repairFailureTuple(code, class); ok {
		typed.code = repaired.code
		typed.class = repaired.class
		return typed
	}
	typed.code = Internal
	typed.class = ClassInternal
	typed.retry = defaultRetryHint()
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
	if repaired, ok := repairFailureTuple(e.code, e.class); ok {
		return &Error{code: repaired.code, class: repaired.class, retry: hint, err: e.err}
	}
	if errors.Is(e.err, context.DeadlineExceeded) {
		return &Error{code: Timeout, class: ClassTimeout, retry: defaultRetryHint(), err: e.err}
	}
	if errors.Is(e.err, context.Canceled) {
		return &Error{code: Canceled, class: ClassCanceled, retry: defaultRetryHint(), err: e.err}
	}
	return &Error{code: Internal, class: ClassInternal, retry: defaultRetryHint(), err: e.err}
}

type repairedFailure struct {
	code  ErrorCode
	class FailureClass
}

func repairFailureTuple(code ErrorCode, class FailureClass) (repairedFailure, bool) {
	if contract.ValidDurableFailureTuple(code, class) {
		return repairedFailure{code: code, class: class}, true
	}
	if repaired, ok := contract.DefaultFailureClass(code); ok {
		return repairedFailure{code: code, class: repaired}, true
	}
	return repairedFailure{}, false
}

func recognizedTransientNetwork(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}
