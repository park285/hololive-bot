package polling

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/service/cache"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
)

func BuildJobRunGuardClaimer(cacheClient cache.Client, activeActiveConfig settings.ScraperActiveActiveConfig) (polling.JobClaimer, error) {
	if !activeActiveConfig.Enabled {
		return nil, nil
	}
	if cacheClient == nil {
		return nil, fmt.Errorf("active-active job run guard requires cache service")
	}
	guard := ingestionlease.NewJobRunGuard(cacheClient, ingestionlease.JobRunGuardConfig{
		Namespace:  activeActiveConfig.Namespace,
		InstanceID: activeActiveConfig.InstanceID,
	})
	return guard, nil
}
