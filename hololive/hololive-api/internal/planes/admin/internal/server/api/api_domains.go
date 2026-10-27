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

	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/system"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	"github.com/kapu/hololive-shared/pkg/domain"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

// 도메인 핸들러는 해당 경로가 사용하는 의존성만 보관한다.
type MemberHandler struct {
	repository *member.Repository
	service    *MemberAdminService
	logger     *slog.Logger
}

type AlarmHandler struct {
	alarm       AlarmManager
	dispatchOps DispatchOperations
	activity    *activity.Logger
	logger      *slog.Logger
}

type RoomHandler struct {
	acl      *acl.Service
	iris     IrisRoomLister
	activity *activity.Logger
	logger   *slog.Logger
}

type StreamHandler struct {
	holodex    *holodexprovider.Service
	repository *member.Repository
	logger     *slog.Logger
}

type StatsHandler struct {
	repository         *member.Repository
	alarm              AlarmManager
	acl                *acl.Service
	systemStats        *system.Collector
	communityShortsOps YouTubeCommunityShortsOpsRepository
	memberIndexLoader  func(context.Context) ([]*domain.Member, error)
	logger             *slog.Logger
}

type SettingsAPIHandler struct {
	settings           settings.ReadWriter
	settingsApplier    sharedsettings.SettingsApplier
	settingsOperations *settingsOperationGate
	roomNames          RoomNameSetter
	xSpaceSessions     *xspaces.Store
	activity           *activity.Logger
	logger             *slog.Logger
}

type TemplateHandler struct {
	templateAdmin *template.AdminService
	logger        *slog.Logger
}

type MajorEventHandler struct {
	majorEventScheduler        MajorEventScheduler
	majorEventMonthlyScheduler MajorEventMonthlyScheduler
	logger                     *slog.Logger
}

func NewMemberHandler(repository *member.Repository, service *MemberAdminService, logger *slog.Logger) *MemberHandler {
	return &MemberHandler{repository: repository, service: service, logger: logger}
}

func NewAlarmHandler(common CommonDeps, alarm AlarmManager) *AlarmHandler {
	return &AlarmHandler{alarm: alarm, activity: common.Activity, logger: common.Logger}
}

func NewRoomHandler(common CommonDeps, access *acl.Service, iris IrisRoomLister) *RoomHandler {
	return &RoomHandler{acl: access, iris: iris, activity: common.Activity, logger: common.Logger}
}

func NewStreamHandler(logger *slog.Logger, holodex *holodexprovider.Service, repository *member.Repository) *StreamHandler {
	return &StreamHandler{logger: logger, holodex: holodex, repository: repository}
}

func NewStatsHandler(logger *slog.Logger, repository *member.Repository, alarm AlarmManager, access *acl.Service, collector *system.Collector, ops YouTubeCommunityShortsOpsRepository, loadMembers func(context.Context) ([]*domain.Member, error)) *StatsHandler {
	return &StatsHandler{logger: logger, repository: repository, alarm: alarm, acl: access, systemStats: collector, communityShortsOps: ops, memberIndexLoader: loadMembers}
}

func NewSettingsAPIHandler(common CommonDeps, deps SettingsDeps, sessions *xspaces.Store) *SettingsAPIHandler {
	return &SettingsAPIHandler{logger: common.Logger, activity: common.Activity, settings: deps.Settings, settingsApplier: deps.Applier, roomNames: deps.RoomNames, xSpaceSessions: sessions, settingsOperations: &settingsOperationGate{}}
}

func NewTemplateHandler(logger *slog.Logger, admin *template.AdminService) *TemplateHandler {
	return &TemplateHandler{logger: logger, templateAdmin: admin}
}

func NewMajorEventHandler(logger *slog.Logger, deps MajorEventDeps) *MajorEventHandler {
	return &MajorEventHandler{logger: logger, majorEventScheduler: deps.Scheduler, majorEventMonthlyScheduler: deps.MonthlyScheduler}
}

type DomainHandlers struct {
	Member     *MemberHandler
	Alarm      *AlarmHandler
	Room       *RoomHandler
	Stream     *StreamHandler
	Stats      *StatsHandler
	Settings   *SettingsAPIHandler
	Template   *TemplateHandler
	MajorEvent *MajorEventHandler
}

func (h *Handler) DomainHandlers() *DomainHandlers {
	h = h.ensureDefaults()

	common := CommonDeps{Logger: h.logger, Activity: h.activity}
	settingsHandler := NewSettingsAPIHandler(common, SettingsDeps{Settings: h.settings, Applier: h.settingsApplier, RoomNames: h.roomNames}, h.xSpaceSessions)

	settingsHandler.settingsOperations = &h.settingsOperations

	var cache memberAdminCache

	if h.memberCache != nil {
		cache = h.memberCache
	}

	var activityLog SettingsActivityLogger

	if h.activity != nil {
		activityLog = h.activity
	}

	return &DomainHandlers{
		Member:     NewMemberHandler(h.repository, NewMemberAdminService(h.repository, cache, activityLog, h.logger), h.logger),
		Alarm:      NewAlarmHandler(common, h.alarm),
		Room:       NewRoomHandler(common, h.acl, h.iris),
		Stream:     NewStreamHandler(h.logger, h.holodex, h.repository),
		Stats:      NewStatsHandler(h.logger, h.repository, h.alarm, h.acl, h.systemStats, h.communityShortsOps, h.memberIndexLoader),
		Settings:   settingsHandler,
		Template:   NewTemplateHandler(h.logger, h.templateAdmin),
		MajorEvent: NewMajorEventHandler(h.logger, MajorEventDeps{Scheduler: h.majorEventScheduler, MonthlyScheduler: h.majorEventMonthlyScheduler}),
	}
}
