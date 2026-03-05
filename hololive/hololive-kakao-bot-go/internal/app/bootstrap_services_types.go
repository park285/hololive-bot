package app

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

// coreInfrastructure 는 Bot 런타임 구성에 필요한 의존성/서비스 묶음을 담는다.
type coreInfrastructure struct {
	deps                         *bot.Dependencies
	alarmService                 *notification.AlarmService
	alarmCRUD                    domain.AlarmCRUD
	holodexService               *holodex.Service // 구체 타입 참조 (concrete 필요 시 사용)
	ytStack                      *providers.YouTubeStack
	photoSync                    *holodex.PhotoSyncService
	templateRenderer             *template.Renderer
	templateAdminSvc             *template.AdminService
	sharedRL                     *scraper.RateLimiter // YouTube 전역 RateLimiter
	runtimeAlarmSchedulerBuilder runtimeAlarmSchedulerBuilder
	cleanupCache                 func()
	cleanupDB                    func()
}
