package bootstrap

import (
	"context"

	"github.com/park285/iris-client-go/v5/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
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

// PlaneDependencies는 process 조립이 소유하고 bot plane이 빌려 쓰는 in-process 의존성이다.
// ACL은 admin plane과 같은 인스턴스다.
type PlaneDependencies struct {
	ACL     *acl.Service
	Holodex *holodexprovider.Service
}

type CoreIntegrationServices struct {
	ACLService *acl.Service
}

type BotWebhookRuntimeDependencies struct {
	Cache cache.LowLevelCache
}
