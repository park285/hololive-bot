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
	"github.com/kapu/hololive-shared/pkg/service/holodex"
)

// ProvideHolodexService - Holodex API 서비스 생성
func ProvideHolodexService(
	baseURL string,
	apiKey string,
	cacheClient cache.Client,
	scraperService *holodex.ScraperService,
	logger *slog.Logger,
) (*holodex.Service, error) {
	holodexCfg := config.DefaultHolodexOperationalConfig()
	holodexCfg.BaseURL = baseURL
	holodexCfg.APIKey = apiKey
	return ProvideHolodexServiceWithConfig(&holodexCfg, cacheClient, scraperService, logger)
}

func ProvideHolodexServiceWithConfig(
	holodexCfg *config.HolodexConfig,
	cacheClient cache.Client,
	scraperService *holodex.ScraperService,
	logger *slog.Logger,
) (*holodex.Service, error) {
	if holodexCfg == nil {
		cfg := config.DefaultHolodexOperationalConfig()
		holodexCfg = &cfg
	}
	service, err := holodex.NewHolodexServiceWithConfig(holodexCfg, holodexCfg.BaseURL, holodexCfg.APIKey, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create holodex service: %w", err)
	}
	return service, nil
}
