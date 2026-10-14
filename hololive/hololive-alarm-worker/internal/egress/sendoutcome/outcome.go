// Package sendoutcome는 Iris 발송 오류의 공통 의미를 분류합니다.
package sendoutcome

import (
	"context"
	"errors"
	"net"
	"slices"

	"github.com/park285/iris-client-go/v3/iris"
)

// Kind는 발송 결과의 증거를 나타냅니다. 재시도 허용은 각 저장소 정책이 결정합니다.
type Kind uint8

const (
	Success Kind = iota
	Failed
	OutcomeUnknown
	TransportAmbiguous
)

var (
	// ErrHandoffOutcomeUnknown은 접수 이후 handoff 결과를 확정할 수 없음을 나타냅니다.
	ErrHandoffOutcomeUnknown = errors.New("iris reply handoff outcome unknown")
	// ErrHandoffFailed는 Iris가 handoff 실패를 확정했음을 나타냅니다.
	ErrHandoffFailed = errors.New("iris reply handoff failed")
)

// Classify는 sender 호출 이후 결과를 분류합니다. 호출 전 취소는 호출자가 먼저 검사해야 합니다.
// TransportAmbiguous는 동일한 저장 request의 재시도 정책을 적용할 수 있도록 확정 unknown과 구분합니다.
func Classify(err error) Kind {
	if err == nil {
		return Success
	}

	if errors.Is(err, ErrHandoffOutcomeUnknown) || containsStructuredUnknown(err) {
		return OutcomeUnknown
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return TransportAmbiguous
	}

	// 결합 오류의 확정 실패가 다른 분기의 응답 소실 증거를 가리면 안 된다.
	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok && joined != nil && slices.ContainsFunc(joined.Unwrap(), func(cause error) bool {
		return Classify(cause) == TransportAmbiguous
	}) {
		return TransportAmbiguous
	}

	if errors.Is(err, ErrHandoffFailed) {
		return Failed
	}

	if errors.Is(err, iris.ErrTransport) {
		cause := err
		if transport, ok := errors.AsType[*iris.TransportError](err); ok && transport != nil {
			cause = transport.Err
		}

		if transportCauseKnownUnsent(cause) {
			return Failed
		}

		return TransportAmbiguous
	}

	return Failed
}

// TransportError 자체의 결합 원인은 모든 분기가 전달 전 실패를 증명해야 한다.
func transportCauseKnownUnsent(err error) bool {
	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok && joined != nil {
		causes := joined.Unwrap()

		return len(causes) > 0 && !slices.ContainsFunc(causes, func(cause error) bool {
			return !transportCauseKnownUnsent(cause)
		})
	}

	if op, ok := errors.AsType[*net.OpError](err); ok && op != nil {
		return op.Op == "dial"
	}

	_, ok := errors.AsType[*net.DNSError](err)

	return ok
}

// 결합 오류의 첫 HTTP 오류가 확정 실패여도 다른 분기의 결과 불명 증거를 보존한다.
func containsStructuredUnknown(err error) bool {
	if iris.HTTPErrorCode(err) == iris.HTTPErrorCodeClientRequestIDOutcomeUnknown {
		return true
	}

	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok && joined != nil {
		return slices.ContainsFunc(joined.Unwrap(), containsStructuredUnknown)
	}

	return false
}
