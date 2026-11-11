package egress

import (
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const preparedTestRequestID = "prepared:request"

// 일반채팅은 RichText 서식을 표시하지 않으므로 오픈채팅 본문이 text 경로로 새면 안 된다.
func TestPreparedMessagePreservesLaneAndBodyAcrossConfigurationChange(t *testing.T) {
	message := delivery.MessageVariants{Plain: "일반 _본문_", RichText: "**fixed**"}
	client := &irisSenderTestClient{}
	first := NewIrisMessageSender(client, WithMarkdownReplies(true), WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}))
	body, route, err := first.PrepareMessageRequest(t.Context(), testIrisSenderRoomID, message)
	require.NoError(t, err)
	require.Equal(t, "richtext", route)
	require.Equal(t, message.RichText, body)

	restarted := NewIrisMessageSender(client)

	_, err = restarted.SendPreparedMessage(t.Context(), testIrisSenderRoomID, body, route, preparedTestRequestID)
	require.NoError(t, err)
	require.Empty(t, client.textCalls)
	require.Len(t, client.richTextCalls, 1)
	require.Equal(t, "**fixed**", client.richTextCalls[0].message)

	plain, plainRoute, err := restarted.PrepareMessageRequest(t.Context(), testIrisSenderRoomID, message)
	require.NoError(t, err)
	require.Equal(t, "text", plainRoute)
	require.Equal(t, kakaoformat.Render(message.Plain), plain)

	_, err = first.SendPreparedMessage(t.Context(), testIrisSenderRoomID, plain, plainRoute, "prepared:plain")
	require.NoError(t, err)
	require.Len(t, client.textCalls, 1)
	require.Equal(t, plain, client.textCalls[0].message)
}

// 이 값이 reconciler의 handoff 확인 대상이다.
func TestPreparedMessageReturnsAdmittedRequestIDOnAllRoutes(t *testing.T) {
	for _, route := range []string{"text", "richtext"} {
		t.Run(route, func(t *testing.T) {
			accepted := &iris.ReplyAcceptedResponse{Success: true, Delivery: " QUEUED ", RequestID: " iris-" + route + " "}
			client := &irisSenderTestClient{
				textAccepted: accepted, textAcceptedSet: true,
				richTextAccepted: accepted, richTextAcceptedSet: true,
			}

			requestID, err := NewIrisMessageSender(client).SendPreparedMessage(t.Context(), testIrisSenderRoomID, "body", route, preparedTestRequestID)
			require.NoError(t, err)
			require.Equal(t, "iris-"+route, requestID)
		})
	}
}

func TestPreparedFormattedMalformedAdmissionIsOutcomeUnknown(t *testing.T) {
	for _, route := range []string{"richtext"} {
		t.Run(route, func(t *testing.T) {
			for _, accepted := range []*iris.ReplyAcceptedResponse{
				nil,
				{Success: false, Delivery: irisSenderQueued, RequestID: "request"},
				{Success: true, Delivery: "async", RequestID: "request"},
				{Success: true, Delivery: irisSenderQueued, RequestID: "  "},
			} {
				client := &irisSenderTestClient{richTextAccepted: accepted, richTextAcceptedSet: true}
				requestID, err := NewIrisMessageSender(client).SendPreparedMessage(t.Context(), testIrisSenderRoomID, "body", route, preparedTestRequestID)
				require.ErrorIs(t, err, sendoutcome.ErrAdmissionOutcomeUnknown, "%+v", accepted)
				require.Empty(t, requestID)
				require.Len(t, client.richTextCalls, 1)
			}
		})
	}
}

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

func TestPreparedTextRequestDoesNotUseRichTextRoute(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}))

	const body = "**원문** [제목](https://example.com/a_b) @사용자_이름"

	for range 2 {
		_, err := sender.SendPreparedMessage(t.Context(), testIrisSenderRoomID, body, "text", preparedTestRequestID)
		require.NoError(t, err)
	}

	require.Empty(t, client.richTextCalls)
	require.Len(t, client.textCalls, 2)

	for _, call := range client.textCalls {
		require.Equal(t, body, call.message)
		require.Equal(t, testIrisSenderRoomID, call.roomID)
	}
}

func TestPreparedMarkdownRouteIsRejected(t *testing.T) {
	client := &irisSenderTestClient{}

	_, err := NewIrisMessageSender(client).SendPreparedMessage(t.Context(), testIrisSenderRoomID, "body", "markdown", preparedTestRequestID)
	require.Error(t, err)
	require.Empty(t, client.richTextCalls)
	require.Empty(t, client.textCalls)
}
