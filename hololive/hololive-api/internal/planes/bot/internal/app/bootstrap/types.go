package bootstrap

import (
	"context"
	"io"

	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type BotInfrastructure struct {
	Deps           *orchestration.Dependencies
	IrisRoomLister IrisRoomLister
	Postgres       database.Client
	Cache          cache.Client
	Cleanup        func() error
}

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

type AlarmModeComponents struct {
	AlarmCRUD        handlercore.AlarmService
	MemberDataSource domain.MemberDataProvider
	// AlarmClient는 AlarmCRUD의 alarm-worker H3 transport 소유자다. bot plane Close에서 닫는다.
	AlarmClient io.Closer
}

type CoreIntegrationServices struct {
	ACLService           *acl.Service
	MajorEventRepository handlercore.MajorEventRepository
	MemberNewsService    handlercore.MemberNewsService
	// SchedulerTransports는 llm-scheduler client의 H3 transport다. bot plane Close에서 닫는다.
	SchedulerTransports []io.Closer
	CommandBuilders     []orchcmd.CommandBuilder
}

type BotWebhookRuntimeDependencies struct {
	Cache cache.Client
}
