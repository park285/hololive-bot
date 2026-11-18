package handoffcheck

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestSDKStatusObservationPreservesHandoffDecision(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		action Action
		code   string
	}{
		{"different identity", http.StatusOK, `{"requestId":"other","state":"handoff_completed"}`, ActionQuarantine, contractsalarm.HandoffCodeOutcomeUnknown},
		{"missing identity", http.StatusOK, `{"state":"handoff_completed"}`, ActionQuarantine, contractsalarm.HandoffCodeOutcomeUnknown},
		{"padded identity", http.StatusOK, `{"requestId":" iris-request-1 ","state":"handoff_completed"}`, ActionQuarantine, contractsalarm.HandoffCodeOutcomeUnknown},
		{"failed other identity", http.StatusOK, `{"requestId":"other","state":"failed"}`, ActionQuarantine, contractsalarm.HandoffCodeOutcomeUnknown},
		{"completed", http.StatusOK, `{"requestId":"iris-request-1","state":"handoff_completed"}`, ActionComplete, ""},
		{"unknown state", http.StatusOK, `{"requestId":"iris-request-1","state":"future_state"}`, ActionQuarantine, contractsalarm.HandoffCodeOutcomeUnknown},
		{"unauthorized", http.StatusUnauthorized, `{}`, ActionReschedule, ""},
		{"forbidden", http.StatusForbidden, `{}`, ActionReschedule, ""},
		{"unavailable", http.StatusServiceUnavailable, `{}`, ActionReschedule, ""},
		{"missing", http.StatusNotFound, `{}`, ActionQuarantine, contractsalarm.HandoffCodeStatusMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/epoch" {
					if _, err := fmt.Fprint(w, `{"epoch":"0123456789abcdef0123456789abcdef"}`); err != nil {
						t.Errorf("write epoch: %v", err)
					}

					return
				}

				requests.Add(1)

				w.WriteHeader(test.status)

				if _, err := fmt.Fprint(w, test.body); err != nil {
					t.Errorf("write status: %v", err)
				}
			}))
			defer server.Close()

			client, err := iris.NewClient(iris.WithBaseURL(server.URL), iris.WithBotToken("synthetic-token"), iris.WithHTTPClient(server.Client()), iris.WithReplyRetry(0))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			t.Cleanup(func() {
				if closeErr := client.Close(); closeErr != nil {
					t.Errorf("Close: %v", closeErr)
				}
			})

			snapshot, queryErr := client.GetReplyStatus(t.Context(), testRequestID)
			decision := Decide(Input{RequestID: testRequestID, Snapshot: snapshot, QueryErr: queryErr, Horizon: time.Hour, RetryBudgetLeft: true})

			if decision.Action != test.action || decision.Code != test.code {
				t.Fatalf("Decide after SDK=(%+v), want action=%s code=%q; snapshot=%+v error=%v", decision, test.action, test.code, snapshot, queryErr)
			}

			if requests.Load() != 1 {
				t.Errorf("status requests=%d, want one", requests.Load())
			}
		})
	}
}
