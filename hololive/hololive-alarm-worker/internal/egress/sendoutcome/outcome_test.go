package sendoutcome

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	testPostOperation = "post"
	testDialOperation = "dial"
)

func TestClassifyPreservesDeliveryEvidence(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Kind
	}{
		{"success", nil, Success},
		{"admission unknown dominates timeout", errors.Join(ErrAdmissionOutcomeUnknown, context.DeadlineExceeded), OutcomeUnknown},
		{"structured unknown", testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`), OutcomeUnknown},
		{"later structured unknown dominates failed", errors.Join(testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`), testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`)), OutcomeUnknown},
		{"timeout", context.DeadlineExceeded, TransportAmbiguous},
		{"joined timeout dominates failure", errors.Join(testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`), context.DeadlineExceeded), TransportAmbiguous},
		{"canceled after invocation", context.Canceled, TransportAmbiguous},
		{"post response lost", &iris.TransportError{Op: testPostOperation, Err: io.ErrUnexpectedEOF}, TransportAmbiguous},
		{"dial rejected", &iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}}, Failed},
		{"dns rejected", &iris.TransportError{Op: testPostOperation, Err: &net.DNSError{Err: "no host"}}, Failed},
		{"not sent before dial", &iris.TransportError{Op: testPostOperation, Err: fmt.Errorf("%w: resolve h3 dial addr: %w", iris.ErrNotSent, &net.DNSError{Err: "no host"})}, Failed},
		{"egress denied is not sent", fmt.Errorf("%w: egress denied", iris.ErrNotSent), Failed},
		{"joined confirmed failure preserves lost response", errors.Join(testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`), &iris.TransportError{Op: testPostOperation, Err: io.ErrUnexpectedEOF}), TransportAmbiguous},
		{"joined dial failure preserves read ambiguity", errors.Join(
			&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}},
			&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: "read", Err: io.ErrUnexpectedEOF}},
		), TransportAmbiguous},
		{"transport joined cause preserves lost response", &iris.TransportError{Op: testPostOperation, Err: errors.Join(
			&net.OpError{Op: testDialOperation, Err: errors.New("refused")}, io.ErrUnexpectedEOF,
		)}, TransportAmbiguous},
		{"joined pre-handoff failures remain known", errors.Join(
			&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}},
			testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`),
		), Failed},
		{"transport joined dial and dns failures remain known", &iris.TransportError{Op: testPostOperation, Err: errors.Join(
			&net.OpError{Op: testDialOperation, Err: errors.New("refused")}, &net.DNSError{Err: "no host"},
		)}, Failed},
		{"payload mismatch", testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`), Failed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			if err != nil {
				err = fmt.Errorf("wrapped: %w", err)
			}

			if got := Classify(err); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestAttemptOutcomeOrdersEvidenceAndLedgerRules(t *testing.T) {
	ledgerTimeout := errors.New("ledger send timeout")
	ledgerUnknown := errors.New("ledger outcome unknown")
	ledger := AttemptRules{TimeoutCause: ledgerTimeout, OutcomeUnknownCause: ledgerUnknown, UnclassifiedOutcomeUnknown: true}
	preHandoff := testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`)
	responseLost := &iris.TransportError{Op: testPostOperation, Err: io.ErrUnexpectedEOF}

	cases := []struct {
		name       string
		err        error
		wantPlain  workercontract.AttemptOutcome
		wantLedger workercontract.AttemptOutcome
	}{
		{"success", nil, workercontract.AttemptSuccess, workercontract.AttemptSuccess},
		{"admission unknown", ErrAdmissionOutcomeUnknown, workercontract.AttemptOutcomeUnknown, workercontract.AttemptOutcomeUnknown},
		{"structured unknown", testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`), workercontract.AttemptOutcomeUnknown, workercontract.AttemptOutcomeUnknown},
		{"unknown dominates deadline", errors.Join(ErrAdmissionOutcomeUnknown, context.DeadlineExceeded), workercontract.AttemptOutcomeUnknown, workercontract.AttemptOutcomeUnknown},
		{"unknown dominates cancel", errors.Join(ErrAdmissionOutcomeUnknown, context.Canceled), workercontract.AttemptOutcomeUnknown, workercontract.AttemptOutcomeUnknown},
		{"unknown dominates ledger timeout", errors.Join(ledgerTimeout, ErrAdmissionOutcomeUnknown), workercontract.AttemptOutcomeUnknown, workercontract.AttemptOutcomeUnknown},
		{"deadline", context.DeadlineExceeded, workercontract.AttemptTimeout, workercontract.AttemptTimeout},
		{"deadline dominates pre-handoff failure", errors.Join(preHandoff, context.DeadlineExceeded), workercontract.AttemptTimeout, workercontract.AttemptTimeout},
		{"ledger timeout with known failure", errors.Join(ledgerTimeout, iris.ErrPermanent), workercontract.AttemptFailed, workercontract.AttemptTimeout},
		{"cancel", context.Canceled, workercontract.AttemptCanceled, workercontract.AttemptCanceled},
		{"response lost", responseLost, workercontract.AttemptOutcomeUnknown, workercontract.AttemptOutcomeUnknown},
		{"ledger unknown with known failure", errors.Join(ledgerUnknown, iris.ErrPermanent), workercontract.AttemptFailed, workercontract.AttemptOutcomeUnknown},
		{"dial rejected", &iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}}, workercontract.AttemptFailed, workercontract.AttemptFailed},
		{"not sent", fmt.Errorf("%w: egress denied", iris.ErrNotSent), workercontract.AttemptFailed, workercontract.AttemptFailed},
		{"pre-handoff failure", preHandoff, workercontract.AttemptFailed, workercontract.AttemptFailed},
		{"payload mismatch", testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`), workercontract.AttemptFailed, workercontract.AttemptFailed},
		{"rate limited", &iris.HTTPError{StatusCode: http.StatusTooManyRequests}, workercontract.AttemptFailed, workercontract.AttemptFailed},
		{"server error", &iris.HTTPError{StatusCode: http.StatusServiceUnavailable}, workercontract.AttemptFailed, workercontract.AttemptOutcomeUnknown},
		{"unclassified", errors.New("provider failed"), workercontract.AttemptFailed, workercontract.AttemptOutcomeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			if err != nil {
				err = fmt.Errorf("wrapped: %w", err)
			}

			if got := AttemptOutcome(err, AttemptRules{}); got != tc.wantPlain {
				t.Fatalf("plain rules: got %v want %v", got, tc.wantPlain)
			}

			if got := AttemptOutcome(err, ledger); got != tc.wantLedger {
				t.Fatalf("ledger rules: got %v want %v", got, tc.wantLedger)
			}
		})
	}
}

func TestReissuableRequiresUncontestedPreHandoffFailure(t *testing.T) {
	preHandoff := testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`)

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, false},
		{"pre-handoff failure", preHandoff, true},
		{"joined known unsent", errors.Join(&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}}, preHandoff), true},
		{"admission unknown", errors.Join(preHandoff, ErrAdmissionOutcomeUnknown), false},
		{"deadline", errors.Join(preHandoff, context.DeadlineExceeded), false},
		{"response lost", errors.Join(preHandoff, &iris.TransportError{Op: testPostOperation, Err: io.ErrUnexpectedEOF}), false},
		{"structured unknown", testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`), false},
		{"payload mismatch", testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Reissuable(tc.err); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
