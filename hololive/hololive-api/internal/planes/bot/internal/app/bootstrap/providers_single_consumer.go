package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/member"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func ProvideChzzkClient(httpClient *http.Client, chzzkConfig config.ChzzkConfig, logger *slog.Logger) *chzzk.Client {
	return chzzk.NewClientWithConfig(&chzzk.ClientConfig{
		HTTPClient:   httpClient,
		BaseURL:      chzzk.DefaultBaseURL,
		ClientID:     chzzkConfig.ClientID,
		ClientSecret: chzzkConfig.ClientSecret,
		Logger:       logger,
	})
}

func ProvideTwitchClient(twitchConfig *config.TwitchConfig, logger *slog.Logger) *twitch.Client {
	if twitchConfig == nil {
		return twitch.NewClient(&twitch.ClientConfig{}, logger)
	}
	return twitch.NewClient(&twitch.ClientConfig{
		ClientID:     twitchConfig.ClientID,
		ClientSecret: twitchConfig.ClientSecret,
	}, logger)
}

func ProvideMemberCacheWithoutValkey(
	ctx context.Context,
	repository *member.Repository,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := member.NewMemberCache(ctx, repository, nil, logger, member.CacheConfig{
		WarmUp:    true,
		ValkeyTTL: constants.MemberCacheDefaults.ValkeyTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create member cache: %w", err)
	}

	return memberCache, nil
}
