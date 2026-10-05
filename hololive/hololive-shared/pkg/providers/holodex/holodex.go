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

// Package holodex는 runtime이 읽은 Holodex·공식 일정 설정으로 Holodex 서비스와 일정 폴백 서비스를 만드는 provider를 담는다.
package holodex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

// ProvideHolodexService는 runtime이 읽은 Holodex 설정으로 서비스를 만든다.
// 설정이 없으면 코드 기본값으로 대신하지 않고 오류를 돌려준다.
func ProvideHolodexService(
	holodexCfg *settings.HolodexConfig,
	cacheClient cache.Client,
	scraperService *htmlscraper.Service,
	logger *slog.Logger,
) (*holodexprovider.Service, error) {
	if holodexCfg == nil {
		return nil, errors.New("holodex config is nil")
	}

	service, err := holodexprovider.NewHolodexServiceWithConfig(holodexCfg, holodexCfg.BaseURL, holodexCfg.APIKey, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create holodex service: %w", err)
	}

	return service, nil
}

// ProvideScraperService는 적재한 공식 일정 runtime 설정으로 Holodex 일정 폴백이 쓰는 공식 일정 서비스를 만든다.
// 멤버 identity 색인을 ctx 안에서 적재하므로 호출자의 기동 ctx를 그대로 넘긴다. 패키지 기본값 변형은 두지 않는다.
func ProvideScraperService(
	ctx context.Context,
	members domain.MemberDataProvider,
	logger *slog.Logger,
	official settings.OfficialScheduleRuntimeConfig,
) (*htmlscraper.Service, error) {
	service, err := htmlscraper.NewService(ctx, members, nil, logger, official)
	if err != nil {
		return nil, fmt.Errorf("provide scraper service: %w", err)
	}

	return service, nil
}
