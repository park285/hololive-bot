package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
)

type LiveRoomResolver interface {
	LiveRooms(context.Context, string) ([]string, error)
}

func registerLiveRoomsRoute(group *gin.RouterGroup, resolver LiveRoomResolver, logger *slog.Logger) {
	group.GET(contract.BasePath+contract.LiveRoomsRoute, func(c *gin.Context) {
		channelID := strings.TrimSpace(c.Param("id"))
		if channelID == "" {
			ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request", errChannelIDRequired.Error()))

			return
		}

		rooms, err := resolver.LiveRooms(c.Request.Context(), channelID)
		if err != nil {
			logger.WarnContext(c.Request.Context(), "live rooms lookup failed", slog.String("channel_id", channelID), slog.Any("error", err))
			ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("live_rooms_failed", "live rooms lookup failed"))

			return
		}

		ginjson.Respond(c, http.StatusOK, contract.Envelope[contract.LiveRoomsResponse]{Success: true, Data: contract.LiveRoomsResponse{RoomIDs: rooms}})
	})
}
