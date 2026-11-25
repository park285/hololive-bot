package api

import (
	"errors"
	"log/slog"
	"time"

	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

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
	pauses   DeliveryPauseStore
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

	Iris                    IrisRoomLister
	DeliveryPauses          DeliveryPauseStore
	DispatchOps             DispatchOperations
	XSpaceSessions          *xspaces.Store
	SchedulePosters         *scheduleposter.Store
	SchedulePosterCollector SchedulePosterCollector
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

func NewDomainHandlers(deps Deps) (*DomainHandlers, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	return &DomainHandlers{
		Member:         newMemberHandler(deps),
		Alarm:          &AlarmHandler{alarm: deps.Alarm, dispatchOps: deps.DispatchOps, activity: deps.Activity, logger: deps.Logger},
		Room:           &RoomHandler{acl: deps.ACL, iris: deps.Iris, pauses: deps.DeliveryPauses, activity: deps.Activity, logger: deps.Logger},
		Stream:         &StreamHandler{holodex: deps.Holodex, logger: deps.Logger},
		Stats:          &StatsHandler{repository: deps.Repository, alarm: deps.Alarm, acl: deps.ACL, logger: deps.Logger},
		Settings:       newSettingsHandler(deps),
		SchedulePoster: &SchedulePosterHandler{store: deps.SchedulePosters, collector: deps.SchedulePosterCollector, logger: deps.Logger, now: time.Now},
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
