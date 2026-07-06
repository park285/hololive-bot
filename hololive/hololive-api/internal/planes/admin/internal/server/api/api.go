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

	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/system"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/park285/iris-client-go/iris"
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
	communityShortsOps         YouTubeCommunityShortsOpsRepository
	activity                   *activity.Logger
	settings                   settings.ReadWriter
	settingsApplier            sharedsettings.SettingsApplier
	acl                        *acl.Service
	iris                       IrisRoomLister
	logger                     *slog.Logger
	systemStats                *system.Collector
	templateAdmin              *template.AdminService
	majorEventScheduler        MajorEventScheduler
	majorEventMonthlyScheduler MajorEventMonthlyScheduler
	startTime                  time.Time
	streamState                *sharedserver.StreamState
	memberIndexLoader          func(context.Context) ([]*domain.Member, error)
}

type statusMessageResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
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

type CommonDeps struct {
	Logger   *slog.Logger
	Activity *activity.Logger
}

type MemberDeps struct {
	Repository *member.Repository
	Cache      *member.Cache
	Profiles   *member.ProfileService
}

type StreamDeps struct {
	Holodex     *holodex.Service
	YouTube     youtube.Service
	ValkeyCache cache.Client
}

type StatsDeps struct {
	Alarm       domain.AlarmCRUD
	ACL         *acl.Service
	Iris        IrisRoomLister
	SystemStats *system.Collector
}

type SettingsDeps struct {
	Settings settings.ReadWriter
	Applier  sharedsettings.SettingsApplier
}

type TemplateDeps struct {
	Admin *template.AdminService
}

type MajorEventDeps struct {
	Scheduler        MajorEventScheduler
	MonthlyScheduler MajorEventMonthlyScheduler
}

type YouTubeOpsDeps struct {
	CommunityShortsOps YouTubeCommunityShortsOpsRepository
}

type HandlerDeps struct {
	Common     CommonDeps
	Member     MemberDeps
	Stream     StreamDeps
	Stats      StatsDeps
	Settings   SettingsDeps
	Template   TemplateDeps
	MajorEvent MajorEventDeps
	YouTubeOps YouTubeOpsDeps
}

func NewHandler(deps *HandlerDeps) *Handler {
	if deps == nil {
		deps = &HandlerDeps{}
	}

	var memberIndexLoader func(context.Context) ([]*domain.Member, error)

	if deps != nil && deps.Member.Repository != nil {
		memberIndexLoader = deps.Member.Repository.GetAllMembers
	}

	return (&Handler{
		repository:                 deps.Member.Repository,
		memberCache:                deps.Member.Cache,
		valkeyCache:                deps.Stream.ValkeyCache,
		profiles:                   deps.Member.Profiles,
		alarm:                      deps.Stats.Alarm,
		holodex:                    deps.Stream.Holodex,
		youtube:                    deps.Stream.YouTube,
		communityShortsOps:         deps.YouTubeOps.CommunityShortsOps,
		activity:                   deps.Common.Activity,
		settings:                   deps.Settings.Settings,
		settingsApplier:            deps.Settings.Applier,
		acl:                        deps.Stats.ACL,
		iris:                       deps.Stats.Iris,
		systemStats:                deps.Stats.SystemStats,
		templateAdmin:              deps.Template.Admin,
		majorEventScheduler:        deps.MajorEvent.Scheduler,
		majorEventMonthlyScheduler: deps.MajorEvent.MonthlyScheduler,
		logger:                     deps.Common.Logger,
		startTime:                  time.Now(),
		streamState:                newStreamState(),
		memberIndexLoader:          memberIndexLoader,
	}).ensureDefaults()
}
