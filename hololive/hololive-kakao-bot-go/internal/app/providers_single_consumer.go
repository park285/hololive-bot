// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// ProvideChzzkClient - Chzzk API 클라이언트 생성.
func ProvideChzzkClient(httpClient *http.Client, cfg config.ChzzkConfig, logger *slog.Logger) *chzzk.Client {
	return chzzk.NewClientWithConfig(chzzk.ClientConfig{
		HTTPClient:   httpClient,
		BaseURL:      chzzk.DefaultBaseURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Logger:       logger,
	})
}

// ProvideTwitchClient - Twitch Helix API 클라이언트 생성.
func ProvideTwitchClient(cfg config.TwitchConfig, logger *slog.Logger) *twitch.Client {
	return twitch.NewClient(twitch.ClientConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, logger)
}

// ProvideMemberCacheWithoutValkey - Valkey 없이 멤버 캐시만 구성.
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

// ProvideFetchProfilesLogger - fetch_profiles 전용 로거.
func ProvideFetchProfilesLogger() (*slog.Logger, func(), error) {
	logger := slog.Default()
	cleanup := func() {} // slog는 Sync 필요 없음

	return logger, cleanup, nil
}

// ProvideFetchProfilesHTTPClient - fetch_profiles 전용 HTTP 클라이언트.
func ProvideFetchProfilesHTTPClient() *http.Client {
	return httputil.NewExternalAPIClient(constants.OfficialProfileConfig.RequestTimeout)
}
