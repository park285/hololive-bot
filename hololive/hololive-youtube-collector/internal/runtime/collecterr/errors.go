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

// ErrInvalidFailureTuple은 호출 코드가 durable 계약 밖 code/class tuple을 만들었음을 알린다. 이런 오류는 미분류
// Internal/ClassInternal로 닫히고, collector는 youtube_collection_invalid_failure_tuple_total로 위반을 센다.
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

// unclassifiedError는 collecterr가 원인을 인식하지 못해 기본 버킷으로 떨어뜨린 오류를 만든다.
// 진단 tuple은 Internal/ClassInternal 그대로지만, 호출자가 명시적으로 붙인 것과 구별된다.
func unclassifiedError(err error) error {
	typed := newError(contract.ErrorInternalInvariant, contract.ClassInternal, defaultRetryHint(), err)

	typed.unclassified = true

	return typed
}

// IsUnclassified는 오류가 명시적 분류 없이 기본 버킷에 담겼는지 알린다.
// 미분류 여부로 치명 여부를 가르는 판정에만 쓴다.
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

	// 계약 밖 code/class tuple은 code의 기본 class로 조용히 고치지 않는다. helper 응답은 youtubejs.mapHelperFailure가
	// 고정 tuple로만 옮기므로 여기 닿는 비정규 tuple은 호출 코드 결함이다. 호출자가 붙인 분류를 믿을 수 없으므로
	// 미분류 Internal로 닫아(치명이 아닌 지연 처리) 원래 tuple을 원인에 남기고, scheduler가 ErrInvalidFailureTuple로
	// 위반 metric을 센다.
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

// recognizedTransportTimeout은 net/http client가 *url.Error로 돌려준 수송 계층 타임아웃(dial·TLS handshake·
// response header·Client.Timeout)과 DB 연결의 dial 타임아웃(*net.OpError Op "dial")을 알린다. 앞 단계가 context
// deadline을 먼저 분류하고, 연결 단계 밖에서 생긴 socket deadline(os.ErrDeadlineExceeded)은 원인이 모호해 계속
// 미분류로 남긴다.
func recognizedTransportTimeout(err error) bool {
	if urlErr, ok := errors.AsType[*url.Error](err); ok && urlErr != nil && urlErr.Timeout() {
		return true
	}

	opErr, ok := errors.AsType[*net.OpError](err)

	return ok && opErr != nil && opErr.Op == "dial" && opErr.Timeout()
}

// recognizedTransientNetwork는 상대 쪽 연결 종료·거부·경로 없음과 PostgreSQL의 연결 예외·관리자 종료를 일시 장애로
// 알린다. 연결 실패(pgconn.ConnectError) 전체를 일시 장애로 보지는 않는다. 인증 실패(28P01) 같은 설정 결함은
// 미분류로 남겨 장애 집계에 섞이지 않게 한다.
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

// transientPostgresError는 SQLSTATE class 08(connection exception)과 57P01·57P02·57P03(관리자 종료·crash 종료·
// 기동 또는 종료 중 연결 거부)을 알린다. 이 코드들은 DB 재시작·장애 조치 동안 나타나고 연결을 다시 맺으면 해소된다.
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
