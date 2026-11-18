package botruntime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestSDKReplyStatusKeepsPrivateHandoffDecision(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		state  string
		code   string
	}{
		{"different identity", http.StatusOK, `{"requestId":"other","state":"handoff_completed"}`, durability.ReplyOutboxManualReview, contractsalarm.HandoffCodeOutcomeUnknown},
		{"missing identity", http.StatusOK, `{"state":"handoff_completed"}`, durability.ReplyOutboxManualReview, contractsalarm.HandoffCodeOutcomeUnknown},
		{"padded identity", http.StatusOK, `{"requestId":" iris-request ","state":"handoff_completed"}`, durability.ReplyOutboxManualReview, contractsalarm.HandoffCodeOutcomeUnknown},
		{"failed other identity", http.StatusOK, `{"requestId":"other","state":"failed"}`, durability.ReplyOutboxManualReview, contractsalarm.HandoffCodeOutcomeUnknown},
		{"completed", http.StatusOK, `{"requestId":"iris-request","state":"handoff_completed"}`, durability.ReplyOutboxHandoffCompleted, ""},
		{"unknown state", http.StatusOK, `{"requestId":"iris-request","state":"future_state"}`, durability.ReplyOutboxManualReview, contractsalarm.HandoffCodeOutcomeUnknown},
		{"unauthorized", http.StatusUnauthorized, `{}`, "", ""},
		{"forbidden", http.StatusForbidden, `{}`, "", ""},
		{"unavailable", http.StatusServiceUnavailable, `{}`, "", ""},
		{"missing", http.StatusNotFound, `{}`, durability.ReplyOutboxManualReview, contractsalarm.HandoffCodeStatusMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/epoch" {
					_, err := fmt.Fprint(w, `{"epoch":"0123456789abcdef0123456789abcdef"}`)
					assert.NoError(t, err)

					return
				}

				requests.Add(1)
				w.WriteHeader(test.status)

				_, err := fmt.Fprint(w, test.body)
				assert.NoError(t, err)
			}))
			defer server.Close()

			client, err := iris.NewClient(iris.WithBaseURL(server.URL), iris.WithBotToken("synthetic-token"), iris.WithHTTPClient(server.Client()), iris.WithReplyRetry(0))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })

			snapshot, queryErr := client.GetReplyStatus(t.Context(), testHandoffRequestID)
			claim := durability.ReplyHandoffClaim{IrisRequestID: testHandoffRequestID, Attempts: 1}
			got := decideReplyHandoff(claim, snapshot, queryErr, time.Hour, 3)
			require.Equal(t, replyHandoffDecision{status: test.state, lastError: test.code}, got)
			require.EqualValues(t, 1, requests.Load())
		})
	}
}
