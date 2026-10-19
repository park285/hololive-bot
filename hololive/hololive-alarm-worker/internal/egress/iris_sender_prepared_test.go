package egress

import (
	"testing"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
)

const preparedTestRequestID = "prepared:request"

func TestPreparedMessagePreservesLaneAndBodyAcrossConfigurationChange(t *testing.T) {
	client := &irisSenderTestClient{}
	first := NewIrisMessageSender(client, WithMarkdownReplies(true), WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}))
	body, route, err := first.PrepareMessageRequest(t.Context(), testIrisSenderRoomID, "**fixed**")
	require.NoError(t, err)

	restarted := NewIrisMessageSender(client)

	_, err = restarted.SendPreparedMessage(t.Context(), testIrisSenderRoomID, body, route, preparedTestRequestID)
	require.NoError(t, err)
	require.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	require.Equal(t, "**fixed**", client.markdownCalls[0].message)

	plain, plainRoute, err := restarted.PrepareMessageRequest(t.Context(), testIrisSenderRoomID, "**fixed**")
	require.NoError(t, err)

	_, err = first.SendPreparedMessage(t.Context(), testIrisSenderRoomID, plain, plainRoute, "prepared:plain")
	require.NoError(t, err)
	require.Len(t, client.textCalls, 1)
	require.Equal(t, plain, client.textCalls[0].message)
}

// 두 경로 모두 202가 증명한 Iris requestId를 handoff 결과와 무관하게 바로 돌려준다. 이 값이 reconciler의 handoff
// 확인 대상이다.
func TestPreparedMessageReturnsAdmittedRequestIDOnBothLanes(t *testing.T) {
	for _, route := range []string{"text", "markdown"} {
		t.Run(route, func(t *testing.T) {
			accepted := &iris.ReplyAcceptedResponse{Success: true, Delivery: " QUEUED ", RequestID: " iris-" + route + " "}
			client := &irisSenderTestClient{
				textAccepted: accepted, textAcceptedSet: true,
				markdownAccepted: accepted, markdownAcceptedSet: true,
			}

			requestID, err := NewIrisMessageSender(client).SendPreparedMessage(t.Context(), testIrisSenderRoomID, "body", route, preparedTestRequestID)
			require.NoError(t, err)
			require.Equal(t, "iris-"+route, requestID)
		})
	}
}

// Markdown 202가 durable admission(성공·queued·requestId)을 증명하지 못하면 성공으로 보고하지 않고 결과 불명이다.
func TestPreparedMarkdownMalformedAdmissionIsOutcomeUnknown(t *testing.T) {
	for _, accepted := range []*iris.ReplyAcceptedResponse{
		nil,
		{Success: false, Delivery: irisSenderQueued, RequestID: "request"},
		{Success: true, Delivery: "async", RequestID: "request"},
		{Success: true, Delivery: irisSenderQueued, RequestID: "  "},
	} {
		client := &irisSenderTestClient{markdownAccepted: accepted, markdownAcceptedSet: true}

		requestID, err := NewIrisMessageSender(client).SendPreparedMessage(t.Context(), testIrisSenderRoomID, "body", "markdown", preparedTestRequestID)
		require.ErrorIs(t, err, sendoutcome.ErrAdmissionOutcomeUnknown, "%+v", accepted)
		require.Empty(t, requestID)
		require.Len(t, client.markdownCalls, 1)
	}
}

// 텍스트 202는 bot plane 텍스트 경로와 같이 requestId만 요구한다. Iris 계약이 정하지 않은 delivery·success 값은
// 결과를 바꾸지 않고, requestId가 없으면 확인할 대상이 없어 결과 불명이다.
func TestPreparedTextAdmissionRequiresOnlyRequestID(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted *iris.ReplyAcceptedResponse
		want     string
	}{
		{name: "async delivery", accepted: &iris.ReplyAcceptedResponse{Success: true, Delivery: "async", RequestID: " iris-text "}, want: "iris-text"},
		{name: "no delivery or success", accepted: &iris.ReplyAcceptedResponse{RequestID: "iris-text"}, want: "iris-text"},
		{name: "empty response"},
		{name: "async without request id", accepted: &iris.ReplyAcceptedResponse{Success: true, Delivery: "async", RequestID: "  "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &irisSenderTestClient{textAccepted: tc.accepted, textAcceptedSet: true}

			requestID, err := NewIrisMessageSender(client).SendPreparedMessage(t.Context(), testIrisSenderRoomID, "body", "text", preparedTestRequestID)

			if tc.want == "" {
				require.ErrorIs(t, err, sendoutcome.ErrAdmissionOutcomeUnknown)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.want, requestID)
			require.Len(t, client.textCalls, 1)
		})
	}
}
