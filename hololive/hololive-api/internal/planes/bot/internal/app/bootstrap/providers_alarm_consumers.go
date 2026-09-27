package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func ProvideMatcher(
	membersData domain.MemberDataProvider,
	cacheClient cache.Client,
	logger *slog.Logger,
) *matcher.Matcher {
	return matcher.NewMatcher(membersData, cacheClient, nil, logger)
}
