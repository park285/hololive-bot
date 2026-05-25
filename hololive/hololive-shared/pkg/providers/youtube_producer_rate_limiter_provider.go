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

package providers

import (
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// 분산 제한이 활성화된 경우 Valkey 기반 SlidingWindowLimiter를 함께 구성합니다.
func ProvideYouTubeProducerRateLimiter(cacheClient cache.Client, logger *slog.Logger) (*scraper.RateLimiter, error) {
	return ProvideYouTubeProducerRateLimiterWithConfig(config.DefaultYouTubeOperationalConfig(), cacheClient, logger)
}

func ProvideYouTubeProducerRateLimiterWithConfig(ytCfg config.YouTubeConfig, cacheClient cache.Client, logger *slog.Logger) (*scraper.RateLimiter, error) {
	limiter := scraper.NewRateLimiter(ytCfg.ProducerRequestInterval)

	drl := ytCfg.ProducerDistributedRateLimit
	if !drl.Enabled {
		return limiter, nil
	}

	distributedLimiter, err := ratelimit.NewSlidingWindowLimiter(
		cacheClient,
		drl.KeyPrefix,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize youtube producer distributed rate limiter: %w", err)
	}
	if err := limiter.ConfigureDistributed(
		distributedLimiter,
		drl.Limit,
		drl.Window,
	); err != nil {
		return nil, fmt.Errorf("configure youtube producer distributed rate limiter: %w", err)
	}

	return limiter, nil
}
