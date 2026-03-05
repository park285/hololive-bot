package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/member"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// ProvideChzzkClient - Chzzk API 클라이언트 생성
func ProvideChzzkClient(httpClient *http.Client, cfg config.ChzzkConfig, logger *slog.Logger) *chzzk.Client {
	return chzzk.NewClientWithConfig(chzzk.ClientConfig{
		HTTPClient:   httpClient,
		BaseURL:      chzzk.DefaultBaseURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Logger:       logger,
	})
}

// ProvideTwitchClient - Twitch Helix API 클라이언트 생성
func ProvideTwitchClient(cfg config.TwitchConfig, logger *slog.Logger) *twitch.Client {
	return twitch.NewClient(twitch.ClientConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, logger)
}

// ProvideMemberCacheWithoutValkey - Valkey 없이 멤버 캐시만 구성
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

// ProvideFetchProfilesLogger - fetch_profiles 전용 로거
func ProvideFetchProfilesLogger() (*slog.Logger, func(), error) {
	logger := slog.Default()
	cleanup := func() {} // slog는 Sync 필요 없음
	return logger, cleanup, nil
}

// ProvideFetchProfilesHTTPClient - fetch_profiles 전용 HTTP 클라이언트
func ProvideFetchProfilesHTTPClient() *http.Client {
	return &http.Client{Timeout: constants.OfficialProfileConfig.RequestTimeout}
}
