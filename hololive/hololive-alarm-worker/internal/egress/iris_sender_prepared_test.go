package egress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedMessagePreservesLaneAndBodyAcrossConfigurationChange(t *testing.T) {
	client := &irisSenderTestClient{}
	first := NewIrisMessageSender(client, WithMarkdownReplies(true), WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}))
	body, route, err := first.PrepareMessageRequest(t.Context(), testIrisSenderRoomID, "**fixed**")
	require.NoError(t, err)

	restarted := NewIrisMessageSender(client)
	require.NoError(t, restarted.SendPreparedMessage(t.Context(), testIrisSenderRoomID, body, route, "prepared:request"))
	require.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	require.Equal(t, "**fixed**", client.markdownCalls[0].message)

	plain, plainRoute, err := restarted.PrepareMessageRequest(t.Context(), testIrisSenderRoomID, "**fixed**")
	require.NoError(t, err)
	require.NoError(t, first.SendPreparedMessage(t.Context(), testIrisSenderRoomID, plain, plainRoute, "prepared:plain"))
	require.Len(t, client.textCalls, 1)
	require.Equal(t, plain, client.textCalls[0].message)
}
