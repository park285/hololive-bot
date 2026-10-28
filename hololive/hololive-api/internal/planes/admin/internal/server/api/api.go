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

	"github.com/park285/iris-client-go/v4/iris"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	"github.com/kapu/hololive-shared/pkg/domain"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

// Handler는 런타임의 도메인별 의존성 조립만 담당한다. 요청은 DomainHandlers가 처리한다.
type Handler struct {
	repository         *member.Repository
	memberCache        *member.Cache
	alarm              AlarmManager
	roomNames          RoomNameSetter
	holodex            *holodexprovider.Service
	activity           *activity.Logger
	settings           settings.ReadWriter
	settingsApplier    sharedsettings.SettingsApplier
	settingsOperations settingsOperationGate
	acl                *acl.Service
	iris               IrisRoomLister
	logger             *slog.Logger
	memberIndexLoader  func(context.Context) ([]*domain.Member, error)
	xSpaceSessions     *xspaces.Store
}

// SetXSpaceSessions는 시작 단계에서만 관리자 세션 저장소를 연결한다. Secret은 응답하지 않는다.
func (h *Handler) SetXSpaceSessions(store *xspaces.Store) { h.xSpaceSessions = store }

type statusMessageResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

func (h *Handler) ensureDefaults() *Handler {
	if h == nil {
		h = &Handler{}
	}

	if h.memberIndexLoader == nil && h.repository != nil {
		h.memberIndexLoader = h.repository.GetAllMembers
	}

	return h
}

type CommonDeps struct {
	Logger   *slog.Logger
	Activity *activity.Logger
}

type MemberDeps struct {
	Repository *member.Repository
	Cache      *member.Cache
}

type StreamDeps struct {
	Holodex *holodexprovider.Service
}

type StatsDeps struct {
	Alarm AlarmManager
	ACL   *acl.Service
	Iris  IrisRoomLister
}

type SettingsDeps struct {
	Settings  settings.ReadWriter
	Applier   sharedsettings.SettingsApplier
	RoomNames RoomNameSetter
}

// AlarmManager는 관리 목록·통계 조회와 구독 삭제에 필요한 작업만 제공한다.
type AlarmManager interface {
	GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error)
	CountAlarmEntries(ctx context.Context) (int, error)
	RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error)
}

type HandlerDeps struct {
	Common   CommonDeps
	Member   MemberDeps
	Stream   StreamDeps
	Stats    StatsDeps
	Settings SettingsDeps
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
		repository:        deps.Member.Repository,
		memberCache:       deps.Member.Cache,
		alarm:             deps.Stats.Alarm,
		roomNames:         deps.Settings.RoomNames,
		holodex:           deps.Stream.Holodex,
		activity:          deps.Common.Activity,
		settings:          deps.Settings.Settings,
		settingsApplier:   deps.Settings.Applier,
		acl:               deps.Stats.ACL,
		iris:              deps.Stats.Iris,
		logger:            deps.Common.Logger,
		memberIndexLoader: memberIndexLoader,
	}).ensureDefaults()
}
