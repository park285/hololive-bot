package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func ProvideMatcher(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
) *matcher.Matcher {
	return matcher.NewMatcher(membersData, nil, logger)
}
