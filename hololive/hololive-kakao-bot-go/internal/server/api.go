package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/system"
)

// ScraperProxyToggler: 스크래퍼 스케줄러 프록시 토글 인터페이스
type ScraperProxyToggler = sharedserver.ScraperProxyToggler

// SettingsApplier: 설정 변경을 런타임에 적용하는 인터페이스
type SettingsApplier = sharedserver.SettingsApplier

// APIHandler: Hololive API 요청을 처리하는 핸들러입니다.
// Admin Dashboard와 Tauri 앱 모두에서 사용됩니다.
// 핸들러 메서드는 도메인별 파일로 분리됨:
//   - api_member.go: 멤버 관리 + 프로필 조회
//   - api_alarm.go: 알람 관리
//   - api_room.go: 룸/ACL 관리
//   - api_stream.go: 스트림/채널 통계
//   - api_stats.go: 봇 통계
//   - api_settings.go: 설정/활동 로그/이름매핑
//   - api_milestone.go: 마일스톤 조회
//   - api_template.go: 템플릿 관리
type APIHandler struct {
	repo                       *member.Repository
	memberCache                *member.Cache
	valkeyCache                *cache.Service
	profiles                   *member.ProfileService
	alarm                      domain.AlarmCRUD
	holodex                    *holodex.Service
	youtube                    *youtube.Service
	scraperProxyToggler        ScraperProxyToggler
	statsRepo                  youtube.StatsDashboardRepository
	activity                   *activity.Logger
	settings                   *settings.Service
	settingsApplier            SettingsApplier
	acl                        *acl.Service
	logger                     *slog.Logger
	systemStats                *system.Collector
	templateAdmin              *template.AdminService
	majorEventScheduler        MajorEventScheduler
	majorEventMonthlyScheduler MajorEventMonthlyScheduler
	startTime                  time.Time
	streamState                *streamState
	memberIndexLoader          func(context.Context) ([]*domain.Member, error)
}

type streamState = sharedserver.StreamState

func newStreamState() *streamState {
	return sharedserver.NewStreamState(channelStatsCacheWorkers, channelStatsRefreshWorkers)
}

// streamState 접근자. 생성자에서 반드시 초기화되므로 nil이 될 수 없다.
func (h *APIHandler) ensureStreamState() *streamState {
	return h.streamState
}

// NewAPIHandler: 새로운 API 핸들러를 생성합니다.
func NewAPIHandler(
	repo *member.Repository,
	memberCache *member.Cache,
	valkeyCache *cache.Service,
	profilesSvc *member.ProfileService,
	alarm domain.AlarmCRUD,
	holodexSvc *holodex.Service,
	youtubeSvc *youtube.Service,
	scraperProxyToggler ScraperProxyToggler,
	statsRepo youtube.StatsDashboardRepository,
	activityLogger *activity.Logger,
	settingsSvc *settings.Service,
	settingsApplier SettingsApplier,
	aclSvc *acl.Service,
	systemSvc *system.Collector,
	templateAdmin *template.AdminService,
	majorEventScheduler MajorEventScheduler,
	majorEventMonthlyScheduler MajorEventMonthlyScheduler,
	logger *slog.Logger,
) *APIHandler {
	var memberIndexLoader func(context.Context) ([]*domain.Member, error)
	if repo != nil {
		memberIndexLoader = repo.GetAllMembers
	}

	return &APIHandler{
		repo:                       repo,
		memberCache:                memberCache,
		valkeyCache:                valkeyCache,
		profiles:                   profilesSvc,
		alarm:                      alarm,
		holodex:                    holodexSvc,
		youtube:                    youtubeSvc,
		scraperProxyToggler:        scraperProxyToggler,
		statsRepo:                  statsRepo,
		activity:                   activityLogger,
		settings:                   settingsSvc,
		settingsApplier:            settingsApplier,
		acl:                        aclSvc,
		systemStats:                systemSvc,
		templateAdmin:              templateAdmin,
		majorEventScheduler:        majorEventScheduler,
		majorEventMonthlyScheduler: majorEventMonthlyScheduler,
		logger:                     logger,
		startTime:                  time.Now(),
		streamState:                newStreamState(),
		memberIndexLoader:          memberIndexLoader,
	}
}
