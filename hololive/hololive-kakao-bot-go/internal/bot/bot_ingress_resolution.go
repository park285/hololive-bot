package bot

import (
	"regexp"

	"github.com/kapu/hololive-shared/pkg/iris"
)

var numericRoomIDRegex = regexp.MustCompile(`^\d+$`)

func resolveRoom(message *iris.Message) (chatID, roomName string) {
	isNumericRoom := message.Room != "" && numericRoomIDRegex.MatchString(message.Room)

	chatID = message.Room
	if !isNumericRoom && message.JSON != nil {
		chatID = message.JSON.ChatID
	}

	roomName = message.Room
	return chatID, roomName
}

func resolveUser(message *iris.Message) (userID, userName string) {
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
