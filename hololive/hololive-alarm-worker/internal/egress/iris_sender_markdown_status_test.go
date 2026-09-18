package egress

import (
	"context"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/require"
)

func TestMarkdownAdmissionWaitsForExactHandoffOutcome(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
		want  error
	}{
		{name: "confirmed", state: "handoff_completed"},
		{name: "failed", state: "failed", want: ErrKaringStatusFailed},
		{name: "unknown", state: "outcome_unknown", want: ErrKaringOutcomeUnknown},
		{name: "timeout", state: "sending", want: ErrKaringOutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			client := &irisSenderTestClient{getStatus: func(_ int, id string) (*iris.ReplyStatusSnapshot, error) {
				require.Equal(t, "markdown-request-1", id)

				if tc.name == "timeout" {
					cancel()
				}

				return &iris.ReplyStatusSnapshot{RequestID: id, State: tc.state}, nil
			}}
			sender := NewIrisMessageSender(client, WithMarkdownReplies(true),
				WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}))

			sender.karingStatusPollInterval = time.Nanosecond

			err := sender.SendMessage(ctx, testIrisSenderRoomID, "synthetic markdown")

			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}

			require.Len(t, client.markdownCalls, 1)
			require.GreaterOrEqual(t, client.statusCalls, 1)
			require.Empty(t, client.textCalls)
		})
	}
}

func TestMarkdownMalformedAdmissionCannotReportSuccess(t *testing.T) {
	for _, accepted := range []*iris.ReplyAcceptedResponse{
		nil,
		{Success: false, Delivery: irisSenderQueued, RequestID: "request"},
		{Success: true, Delivery: "sent", RequestID: "request"},
		{Success: true, Delivery: irisSenderQueued},
	} {
		client := &irisSenderTestClient{markdownAccepted: accepted, markdownAcceptedSet: true}
		sender := NewIrisMessageSender(client, WithMarkdownReplies(true),
			WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}))
		err := sender.SendMessage(t.Context(), testIrisSenderRoomID, "synthetic markdown")
		require.ErrorIs(t, err, ErrKaringOutcomeUnknown)
		require.Zero(t, client.statusCalls)
		require.Len(t, client.markdownCalls, 1)
	}
}
