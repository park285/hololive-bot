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
