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

func ProvideChzzkClient(httpClient *http.Client, cfg config.ChzzkConfig, logger *slog.Logger) *chzzk.Client {
	return chzzk.NewClientWithConfig(chzzk.ClientConfig{
		HTTPClient:   httpClient,
		BaseURL:      chzzk.DefaultBaseURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Logger:       logger,
	})
}

func ProvideTwitchClient(cfg config.TwitchConfig, logger *slog.Logger) *twitch.Client {
	return twitch.NewClient(twitch.ClientConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, logger)
}

func ProvideMemberCacheWithoutValkey(
	ctx context.Context,
	repo *member.Repository,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := member.NewMemberCache(ctx, repo, nil, logger, member.CacheConfig{
		WarmUp:    true,
		ValkeyTTL: constants.MemberCacheDefaults.ValkeyTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create member cache: %w", err)
	}

	return memberCache, nil
}
