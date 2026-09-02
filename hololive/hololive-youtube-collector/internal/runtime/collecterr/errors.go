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

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type (
	ErrorCode    = contract.CollectionErrorCode
	FailureClass = contract.FailureClass
)

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
	code         ErrorCode
	class        FailureClass
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
		degraded := newError(Internal, ClassInternal, defaultRetryHint(), err)

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
		return &Error{code: Timeout, class: ClassTimeout, retry: defaultRetryHint(), err: err}
	}

	if errors.Is(err, context.Canceled) {
		return &Error{code: Canceled, class: ClassCanceled, retry: defaultRetryHint(), err: err}
	}

	if recognizedTransientNetwork(err) {
		return &Error{code: Failed, class: ClassTransient, retry: defaultRetryHint(), err: err}
	}

	return &Error{code: Internal, class: ClassInternal, retry: defaultRetryHint(), err: err, unclassified: true}
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

	return unclassifiedError(err)
}

// unclassifiedError는 collecterr가 원인을 인식하지 못해 기본 버킷으로 떨어뜨린 오류를 만든다.
// 진단 tuple은 Internal/ClassInternal 그대로지만, 호출자가 명시적으로 붙인 것과 구별된다.
func unclassifiedError(err error) error {
	typed := newError(Internal, ClassInternal, defaultRetryHint(), err)

	typed.unclassified = true

	return typed
}

// IsUnclassified는 오류가 명시적 분류 없이 기본 버킷에 담겼는지 알린다.
// 미분류 여부로 치명 여부를 가르는 판정에만 쓴다.
func IsUnclassified(err error) bool {
	normalized := Normalize(err)

	return normalized != nil && normalized.unclassified
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
		return &Error{code: repaired.code, class: repaired.class, retry: hint, err: e.err, unclassified: e.unclassified}
	}

	if errors.Is(e.err, context.DeadlineExceeded) {
		return &Error{code: Timeout, class: ClassTimeout, retry: defaultRetryHint(), err: e.err}
	}

	if errors.Is(e.err, context.Canceled) {
		return &Error{code: Canceled, class: ClassCanceled, retry: defaultRetryHint(), err: e.err}
	}

	return &Error{code: Internal, class: ClassInternal, retry: defaultRetryHint(), err: e.err, unclassified: e.unclassified}
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

	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return true
	}

	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}
