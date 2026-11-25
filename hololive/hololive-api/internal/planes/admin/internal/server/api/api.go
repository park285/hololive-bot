package api

import (
	"context"
	"time"

	"github.com/park285/iris-client-go/v6/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const adminRequestTimeout = 10 * time.Second

type statusMessageResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

type AlarmManager interface {
	GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error)
	CountAlarmEntries(ctx context.Context) (int, error)
	RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error)
}
