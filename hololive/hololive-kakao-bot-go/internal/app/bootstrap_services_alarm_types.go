package app

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/member"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

type alarmModeComponents struct {
	alarmCRUD        domain.AlarmCRUD
	alarmService     *notification.AlarmService
	chzzkClient      *chzzk.Client
	twitchClient     *twitch.Client
	memberDataSource member.DataProvider
}

type alarmDependencies struct {
	alarmService       *notification.AlarmService
	memberDataProvider member.DataProvider
	chzzkClient        *chzzk.Client
	twitchClient       *twitch.Client
}
