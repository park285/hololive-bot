package sendoutcome

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/park285/iris-client-go/v3/iris"
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
		{"handoff unknown dominates timeout", errors.Join(ErrHandoffOutcomeUnknown, context.DeadlineExceeded), OutcomeUnknown},
		{"structured unknown", &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`}, OutcomeUnknown},
		{"handoff failed", ErrHandoffFailed, Failed},
		{"later structured unknown dominates failed", errors.Join(&iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`}, &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`}), OutcomeUnknown},
		{"timeout", context.DeadlineExceeded, TransportAmbiguous},
		{"joined timeout dominates failure", errors.Join(ErrHandoffFailed, context.DeadlineExceeded), TransportAmbiguous},
		{"canceled after invocation", context.Canceled, TransportAmbiguous},
		{"post response lost", &iris.TransportError{Op: testPostOperation, Err: io.ErrUnexpectedEOF}, TransportAmbiguous},
		{"dial rejected", &iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}}, Failed},
		{"dns rejected", &iris.TransportError{Op: testPostOperation, Err: &net.DNSError{Err: "no host"}}, Failed},
		{"joined handoff failure preserves lost response", errors.Join(ErrHandoffFailed, &iris.TransportError{Op: testPostOperation, Err: io.ErrUnexpectedEOF}), TransportAmbiguous},
		{"joined dial failure preserves read ambiguity", errors.Join(
			&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}},
			&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: "read", Err: io.ErrUnexpectedEOF}},
		), TransportAmbiguous},
		{"transport joined cause preserves lost response", &iris.TransportError{Op: testPostOperation, Err: errors.Join(
			&net.OpError{Op: testDialOperation, Err: errors.New("refused")}, io.ErrUnexpectedEOF,
		)}, TransportAmbiguous},
		{"joined pre-handoff failures remain known", errors.Join(
			&iris.TransportError{Op: testPostOperation, Err: &net.OpError{Op: testDialOperation, Err: errors.New("refused")}},
			&iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`},
		), Failed},
		{"transport joined dial and dns failures remain known", &iris.TransportError{Op: testPostOperation, Err: errors.Join(
			&net.OpError{Op: testDialOperation, Err: errors.New("refused")}, &net.DNSError{Err: "no host"},
		)}, Failed},
		{"payload mismatch", &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`}, Failed},
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
