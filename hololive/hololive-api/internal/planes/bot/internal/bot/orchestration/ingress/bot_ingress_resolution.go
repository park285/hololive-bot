package ingress

import (
	"strings"

	"github.com/park285/iris-client-go/v6/webhook"
)

func roomChatFromMessage(message *webhook.Message) (roomType, roomLinkID string) {
	if message == nil || message.JSON == nil {
		return "", ""
	}

	return strings.TrimSpace(message.JSON.RoomType), strings.TrimSpace(message.JSON.RoomLinkID)
}

func resolveUser(message *webhook.Message) (userID, userName string) {
	userID = "unknown"
	userName = userID

	if message.JSON != nil && message.JSON.UserID != "" {
		userID = message.JSON.UserID
		userName = userID
	}

	if message.Sender != nil {
		userName = *message.Sender
	}

	return userID, userName
}
