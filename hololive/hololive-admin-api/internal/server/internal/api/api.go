// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-admin-api/internal/service/system"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

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
type Handler struct {
	repository                 *member.Repository
	memberCache                *member.Cache
	valkeyCache                cache.Client
	profiles                   *member.ProfileService
	alarm                      domain.AlarmCRUD
	holodex                    *holodex.Service
	youtube                    youtube.Service
	statsRepository            stats.StatsDashboardRepository
	communityShortsOps         YouTubeCommunityShortsOpsRepository
	activity                   *activity.Logger
	settings                   settings.ReadWriter
	settingsApplier            sharedsettings.SettingsApplier
	acl                        *acl.Service
	logger                     *slog.Logger
	systemStats                *system.Collector
	templateAdmin              *template.AdminService
	majorEventScheduler        MajorEventScheduler
	majorEventMonthlyScheduler MajorEventMonthlyScheduler
	startTime                  time.Time
	streamState                *sharedserver.StreamState
	memberIndexLoader          func(context.Context) ([]*domain.Member, error)
}

func newStreamState() *sharedserver.StreamState {
	return sharedserver.NewStreamState(channelStatsCacheWorkers, channelStatsRefreshWorkers)
}

func (h *Handler) ensureDefaults() *Handler {
	if h == nil {
		h = &Handler{}
	}

	if h.streamState == nil {
		h.streamState = newStreamState()
	}

	if h.memberIndexLoader == nil && h.repository != nil {
		h.memberIndexLoader = h.repository.GetAllMembers
	}

	if h.startTime.IsZero() {
		h.startTime = time.Now()
	}

	return h
}

// streamState 접근자. 생성자에서 반드시 초기화되므로 nil이 될 수 없다.
func (h *Handler) ensureStreamState() *sharedserver.StreamState {
	if h == nil {
		return newStreamState()
	}

	if h.streamState == nil {
		h.streamState = newStreamState()
	}

	return h.streamState
}

func (h *Handler) HasCommunityShortsOpsRepository() bool {
	return h != nil && h.communityShortsOps != nil
}

func NewHandler(
	repository *member.Repository,
	memberCache *member.Cache,
	valkeyCache cache.Client,
	profilesService *member.ProfileService,
	alarm domain.AlarmCRUD,
	holodexService *holodex.Service,
	youtubeService youtube.Service,
	statsRepository stats.StatsDashboardRepository,
	communityShortsOps YouTubeCommunityShortsOpsRepository,
	activityLogger *activity.Logger,
	settingsService settings.ReadWriter,
	settingsApplier sharedsettings.SettingsApplier,
	aclService *acl.Service,
	systemService *system.Collector,
	templateAdmin *template.AdminService,
	majorEventScheduler MajorEventScheduler,
	majorEventMonthlyScheduler MajorEventMonthlyScheduler,
	logger *slog.Logger,
) *Handler {
	var memberIndexLoader func(context.Context) ([]*domain.Member, error)

	if repository != nil {
		memberIndexLoader = repository.GetAllMembers
	}

	return (&Handler{
		repository:                 repository,
		memberCache:                memberCache,
		valkeyCache:                valkeyCache,
		profiles:                   profilesService,
		alarm:                      alarm,
		holodex:                    holodexService,
		youtube:                    youtubeService,
		statsRepository:            statsRepository,
		communityShortsOps:         communityShortsOps,
		activity:                   activityLogger,
		settings:                   settingsService,
		settingsApplier:            settingsApplier,
		acl:                        aclService,
		systemStats:                systemService,
		templateAdmin:              templateAdmin,
		majorEventScheduler:        majorEventScheduler,
		majorEventMonthlyScheduler: majorEventMonthlyScheduler,
		logger:                     logger,
		startTime:                  time.Now(),
		streamState:                newStreamState(),
		memberIndexLoader:          memberIndexLoader,
	}).ensureDefaults()
}
