package bootstrap

import (
	"context"
	"io"

	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// BotCache는 bot runtime 조립이 BotInfrastructure.Cache에서 쓰는 작업이다. 준비 상태 확인(IsConnected)과
// webhook nonce 저장소용 valkey client 획득(GetClient)을 쓴다.
type BotCache interface {
	IsConnected(ctx context.Context) bool
	cache.LowLevelCache
}

type BotInfrastructure struct {
	Deps           *orchestration.Dependencies
	IrisRoomLister IrisRoomLister
	Postgres       database.Client
	Cache          BotCache
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

// PlaneDependencies는 process 조립이 소유하고 bot plane이 빌려 쓰는 in-process 의존성이다.
// ACL은 admin plane과 같은 인스턴스이고, MajorEvent·MemberNews는 같은 프로세스 llm plane의 구독 저장소와
// digest 서비스다. 둘 중 하나가 nil이면 해당 명령은 꺼진다.
type PlaneDependencies struct {
	ACL        *acl.Service
	MajorEvent handlercore.MajorEventRepository
	MemberNews handlercore.MemberNewsService
}

type CoreIntegrationServices struct {
	ACLService           *acl.Service
	MajorEventRepository handlercore.MajorEventRepository
	MemberNewsService    handlercore.MemberNewsService
	CommandBuilders      []orchcmd.CommandBuilder
}

type BotWebhookRuntimeDependencies struct {
	Cache cache.LowLevelCache
}
