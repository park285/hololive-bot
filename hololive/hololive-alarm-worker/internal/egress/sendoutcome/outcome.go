package sendoutcome

import (
	"context"
	"errors"
	"net"
	"slices"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

type Kind uint8

const (
	Success Kind = iota
	Failed
	OutcomeUnknown
	TransportAmbiguous
)

var ErrAdmissionOutcomeUnknown = errors.New("iris reply admission outcome unknown")

func Classify(err error) Kind {
	if err == nil {
		return Success
	}

	if errors.Is(err, ErrAdmissionOutcomeUnknown) || containsStructuredUnknown(err) {
		return OutcomeUnknown
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return TransportAmbiguous
	}

	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok && joined != nil && slices.ContainsFunc(joined.Unwrap(), func(cause error) bool {
		return Classify(cause) == TransportAmbiguous
	}) {
		return TransportAmbiguous
	}

	if errors.Is(err, iris.ErrNotSent) {
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

func Reissuable(err error) bool {
	return Classify(err) == Failed && iris.IsPreHandoffClientRequestIDConflict(err)
}

type AttemptRules struct {
	TimeoutCause               error
	OutcomeUnknownCause        error
	UnclassifiedOutcomeUnknown bool
}

func AttemptOutcome(err error, rules AttemptRules) workercontract.AttemptOutcome {
	kind := Classify(err)

	switch {
	case kind == Success:
		return workercontract.AttemptSuccess
	case kind == OutcomeUnknown:
		return workercontract.AttemptOutcomeUnknown
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, rules.TimeoutCause):
		return workercontract.AttemptTimeout
	case errors.Is(err, context.Canceled):
		return workercontract.AttemptCanceled
	case kind == TransportAmbiguous, errors.Is(err, rules.OutcomeUnknownCause):
		return workercontract.AttemptOutcomeUnknown
	case rules.UnclassifiedOutcomeUnknown && !irisFailure(err):
		return workercontract.AttemptOutcomeUnknown
	default:
		return workercontract.AttemptFailed
	}
}

func irisFailure(err error) bool {
	return errors.Is(err, iris.ErrPermanent) || errors.Is(err, iris.ErrAuthFailed) || errors.Is(err, iris.ErrRateLimited) ||
		errors.Is(err, iris.ErrTransport) || errors.Is(err, iris.ErrNotSent)
}

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
