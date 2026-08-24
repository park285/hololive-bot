package transport

import (
	"context"
	"strings"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

type roomChatContextKey struct{}

type roomChat struct {
	roomType   string
	roomLinkID string
}

func WithRoomChat(ctx context.Context, roomType, roomLinkID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	roomType = strings.TrimSpace(roomType)
	roomLinkID = strings.TrimSpace(roomLinkID)

	if roomType == "" && roomLinkID == "" {
		return ctx
	}

	return context.WithValue(ctx, roomChatContextKey{}, roomChat{roomType: roomType, roomLinkID: roomLinkID})
}

func RoomChatFromContext(ctx context.Context) (roomType, roomLinkID string, ok bool) {
	if ctx == nil {
		return "", "", false
	}

	value, exists := ctx.Value(roomChatContextKey{}).(roomChat)
	if !exists {
		return "", "", false
	}

	return value.roomType, value.roomLinkID, true
}

type RoomChat interface {
	OpenChat(ctx context.Context, roomID string) bool
}

func markdownForRoom(ctx context.Context, roomID string, enabled bool, rooms RoomChat) bool {
	if !enabled {
		return false
	}

	if roomType, roomLinkID, ok := RoomChatFromContext(ctx); ok {
		return kakaoformat.IsOpenChat(roomType, roomLinkID)
	}

	if rooms != nil {
		return rooms.OpenChat(ctx, roomID)
	}

	return false
}
