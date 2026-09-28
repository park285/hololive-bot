package app

import (
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

func buildAdminAPICommunityShortsOpsRepository(infra *sharedmodules.InfraModule) server.YouTubeCommunityShortsOpsRepository {
	if infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil
	}

	return telemetry.NewRepository(infra.Postgres.GetPool())
}
