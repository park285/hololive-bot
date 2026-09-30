package egress

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"
)

func TestMarkdownHandoffStaysBoundedWhenStatusCannotFinish(t *testing.T) {
	for _, pollFailure := range []bool{false, true} {
		for _, cancelParent := range []bool{false, true} {
			synctest.Test(t, func(t *testing.T) {
				client := &irisSenderTestClient{getStatus: func(_ int, requestID string) (*iris.ReplyStatusSnapshot, error) {
					if pollFailure {
						return nil, errors.New("status unavailable")
					}

					return &iris.ReplyStatusSnapshot{RequestID: requestID, State: "queued"}, nil
				}}
				sender := newMarkdownTestSender(client)

				sender.replyStatusPollInterval = replyStatusPollInterval

				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

				defer cancel()

				expected := 10 * time.Second

				if cancelParent {
					expected = time.Second

					go func() { time.Sleep(time.Second); cancel() }()
				}

				started := time.Now()
				err := sender.SendMessage(ctx, testIrisSenderRoomID, "message")
				require.ErrorIs(t, err, ErrReplyHandoffOutcomeUnknown)

				if cancelParent {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}

				require.Equal(t, expected, time.Since(started))
				require.Len(t, client.markdownCalls, 1)
			})
		}
	}
}
