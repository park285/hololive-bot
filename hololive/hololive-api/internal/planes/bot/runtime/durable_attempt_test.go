package botruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/iris-client-go/v4/webhook"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestInboxAttemptTrackingEndsOnReturnAndPanic(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			inbox := durability.NewInboxRepository(pool)
			_, err := inbox.Admit(t.Context(), durability.InboxMessage{
				MessageID: "message:attempt", RoomID: testRoomID, OrderingKey: testOrderingKey,
				Payload: testWebhookBody(t, "attempt", "!help"),
			})
			require.NoError(t, err)

			commands := durability.NewCommandExecutionRepository(pool)
			tracker := workercontract.NewExecutorTracker()
			runtime := &durableRuntime{
				inbox: inbox, commands: commands,
				inboxTracker: tracker, inboxTotals: &workercontract.Counters{},
				claimLease: time.Minute, handlerTimeout: time.Second,
				heartbeatEvery: time.Hour, settlementTimeout: time.Second,
				inboxHeartbeat: inbox.Heartbeat, commandHeartbeat: commands.Heartbeat,
				bot: durableMessageProcessorFunc(func(context.Context, *webhook.Message) error {
					require.Equal(t, int64(1), tracker.Snapshot(time.Now()).InFlight)

					switch outcome {
					case "panic":
						panic("inbox attempt panic")
					case "failure":
						return errors.New("handler failed")
					default:
						return nil
					}
				}),
			}
			claimed := 0
			process := func() {
				require.True(t, runtime.processNextInbox(t.Context(), func() { claimed++ }))
			}

			if outcome == "panic" {
				require.PanicsWithValue(t, "inbox attempt panic", process)
			} else {
				process()

				totals := runtime.inboxTotals.Snapshot().Attempts

				if outcome == "success" {
					require.Equal(t, uint64(1), totals.Success)
				} else {
					require.Equal(t, uint64(1), totals.Failed)
				}
			}

			require.Equal(t, 1, claimed)
			require.Zero(t, tracker.Snapshot(time.Now()).InFlight)
			require.Zero(t, tracker.Snapshot(time.Now()).OldestInFlightAgeMS)
		})
	}
}

type attemptReplyClient struct {
	stubIrisClient

	send func() error
}

func (c *attemptReplyClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, c.send()
}

func TestOutboxAttemptTrackingEndsOnReturnAndPanic(t *testing.T) {
	for _, panics := range []bool{false, true} {
		name := "failure"

		if panics {
			name = "panic"
		}

		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			outbox := newTestReplyOutboxRepository(t, pool)
			_, err := outbox.Insert(t.Context(), &durability.ReplyOutboxEntry{
				MessageID: "message:attempt", Phase: transport.ReplyPhase, RoomID: testRoomID,
				Payload:         []byte(`{"kind":"text","message":"answer"}`),
				ClientRequestID: transport.ReplyClientRequestID("message:attempt", 0),
			})
			require.NoError(t, err)

			tracker := workercontract.NewExecutorTracker()
			runtime := &durableRuntime{
				outbox: outbox, outboxTracker: tracker, outboxTotals: &workercontract.Counters{},
				outboxClaimLease: time.Minute, dispatchBudget: time.Second,
				settlementTimeout: time.Second, outboxMaxAttempts: testDurableProfile(t).BotReplyOutbox.MaxAttempts,
				outboxRetryAfter: time.Second,
				irisClient: &attemptReplyClient{send: func() error {
					require.Equal(t, int64(1), tracker.Snapshot(time.Now()).InFlight)

					if panics {
						panic("outbox attempt panic")
					}

					return iris.ErrRetryable
				}},
			}
			claimed := 0
			process := func() {
				require.True(t, runtime.processNextOutbox(t.Context(), func() { claimed++ }))
			}

			if panics {
				require.PanicsWithValue(t, "outbox attempt panic", process)
			} else {
				process()
				require.Equal(t, uint64(1), runtime.outboxTotals.Snapshot().Attempts.Failed)
			}

			require.Equal(t, 1, claimed)
			require.Zero(t, tracker.Snapshot(time.Now()).InFlight)
			require.Zero(t, tracker.Snapshot(time.Now()).OldestInFlightAgeMS)
		})
	}
}
