package adminruntime

import (
	"errors"
	"fmt"

	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

func buildAdminAPICommunityShortsOpsRepository(infra *sharedmodules.InfraModule) (server.YouTubeCommunityShortsOpsRepository, error) {
	if infra == nil || infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil, errors.New("community shorts repository: postgres pool is required")
	}

	repository, err := telemetry.NewRepository(infra.Postgres.GetPool())
	if err != nil {
		return nil, fmt.Errorf("create community shorts repository: %w", err)
	}

	return repository, nil
}
