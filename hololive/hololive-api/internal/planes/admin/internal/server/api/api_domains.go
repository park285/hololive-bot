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
	"errors"
	"log/slog"

	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
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

type StatsHandler struct {
	repository *member.Repository
	alarm      AlarmManager
	acl        *acl.Service
	logger     *slog.Logger
}

func NewMemberHandler(repository *member.Repository, service *MemberAdminService, logger *slog.Logger) *MemberHandler {
	return &MemberHandler{repository: repository, service: service, logger: logger}
}

// Deps는 admin route handler 조립 입력이다. Repository·Holodex·Alarm·RoomNames·ACL·Settings는 필수이고
// 요청 처리에서 다시 확인하지 않는다. MemberCache·Applier가 없으면 해당 endpoint가 503을 돌려주고,
// Iris가 없으면 joined-rooms가 503, DispatchOps가 없으면 원장 운영 endpoint가 503, XSpaceSessions가 없으면
// X 세션 관리가 비활성 응답을 돌려준다.
type Deps struct {
	Logger   *slog.Logger
	Activity *activity.Logger

	Repository  *member.Repository
	MemberCache *member.Cache
	Holodex     *holodexprovider.Service
	Alarm       AlarmManager
	RoomNames   RoomNameSetter
	ACL         *acl.Service
	Settings    adminsettings.ReadWriter
	Applier     adminsettings.SettingsApplier

	Iris           IrisRoomLister
	DispatchOps    DispatchOperations
	XSpaceSessions *xspaces.Store
	// SchedulePosters가 nil이면 일정 포스터 route는 503을 돌려준다.
	SchedulePosters *scheduleposter.Store
}

type DomainHandlers struct {
	Member         *MemberHandler
	Alarm          *AlarmHandler
	Room           *RoomHandler
	Stream         *StreamHandler
	Stats          *StatsHandler
	Settings       *SettingsHandler
	SchedulePoster *SchedulePosterHandler
}

// NewDomainHandlers는 admin route handler를 한 번 만든다. 필수 의존성이 없으면 조립 오류를 돌려준다.
func NewDomainHandlers(deps Deps) (*DomainHandlers, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	return &DomainHandlers{
		Member:         newMemberHandler(deps),
		Alarm:          &AlarmHandler{alarm: deps.Alarm, dispatchOps: deps.DispatchOps, activity: deps.Activity, logger: deps.Logger},
		Room:           &RoomHandler{acl: deps.ACL, iris: deps.Iris, activity: deps.Activity, logger: deps.Logger},
		Stream:         &StreamHandler{holodex: deps.Holodex, logger: deps.Logger},
		Stats:          &StatsHandler{repository: deps.Repository, alarm: deps.Alarm, acl: deps.ACL, logger: deps.Logger},
		Settings:       newSettingsHandler(deps),
		SchedulePoster: &SchedulePosterHandler{store: deps.SchedulePosters, logger: deps.Logger},
	}, nil
}

func (d Deps) validate() error {
	required := []struct {
		missing bool
		err     string
	}{
		{d.Repository == nil, "member repository must not be nil"},
		{d.Holodex == nil, "holodex service must not be nil"},
		{d.Alarm == nil, "alarm client must not be nil"},
		{d.RoomNames == nil, "room name setter must not be nil"},
		{d.ACL == nil, "acl service must not be nil"},
		{d.Settings == nil, "settings service must not be nil"},
	}
	for _, dependency := range required {
		if dependency.missing {
			return errors.New(dependency.err)
		}
	}

	return nil
}

// activityLog는 nil *activity.Logger를 nil이 아닌 interface로 넘기지 않는다.
func (d Deps) activityLog() SettingsActivityLogger {
	if d.Activity == nil {
		return nil
	}

	return d.Activity
}

func newMemberHandler(deps Deps) *MemberHandler {
	var cache memberAdminCache

	if deps.MemberCache != nil {
		cache = deps.MemberCache
	}

	return NewMemberHandler(deps.Repository, NewMemberAdminService(deps.Repository, cache, deps.activityLog(), deps.Logger), deps.Logger)
}

func newSettingsHandler(deps Deps) *SettingsHandler {
	return &SettingsHandler{
		SettingsApplier: deps.Applier,
		Logger:          deps.Logger,
		Alarm:           deps.RoomNames,
		Activity:        deps.activityLog(),
		Settings:        deps.Settings,
		xSpaceSessions:  deps.XSpaceSessions,
		operations:      newSettingsOperationGate(),
	}
}
