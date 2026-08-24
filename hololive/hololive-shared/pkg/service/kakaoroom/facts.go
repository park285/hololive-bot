package kakaoroom

import (
	"strconv"
	"strings"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

type Facts struct {
	RoomID     string
	RoomType   string
	RoomLinkID string
}

func (f Facts) OpenChat() bool {
	return kakaoformat.IsOpenChat(f.RoomType, f.RoomLinkID)
}

func normalizeFacts(roomID, roomType, roomLinkID string) Facts {
	return Facts{
		RoomID:     strings.TrimSpace(roomID),
		RoomType:   strings.TrimSpace(roomType),
		RoomLinkID: strings.TrimSpace(roomLinkID),
	}
}

func factsFromSummary(summary iris.RoomSummary) Facts {
	facts := Facts{RoomID: strconv.FormatInt(summary.ChatID, 10)}
	if summary.Type != nil {
		facts.RoomType = strings.TrimSpace(*summary.Type)
	}

	if summary.LinkID != nil && *summary.LinkID > 0 {
		facts.RoomLinkID = strconv.FormatInt(*summary.LinkID, 10)
	}

	if facts.RoomLinkID == "" && summary.LinkURL != nil {
		facts.RoomLinkID = strings.TrimSpace(*summary.LinkURL)
	}

	return facts
}
