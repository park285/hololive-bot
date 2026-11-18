package holodexprovider

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"sync"
	"time"

	apiclient "github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
	htmlscraper "github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newInMemoryCacheClient() *cachemocks.Client {
	var mu sync.Mutex

	store := make(map[string][]byte)

	return &cachemocks.Client{
		GetFunc: func(_ context.Context, key string, dest any) error {
			mu.Lock()

			payload, ok := store[key]
			mu.Unlock()

			if !ok {
				return nil
			}

			return jsonv2.Unmarshal(payload, dest)
		},
		SetFunc: func(_ context.Context, key string, value any, _ time.Duration) error {
			payload, err := jsonv2.Marshal(value)
			if err != nil {
				return fmt.Errorf("marshal: %w", err)
			}

			mu.Lock()

			store[key] = payload
			mu.Unlock()

			return nil
		},
	}
}

func newServiceForFallbackTest(requester apiclient.Requester) *Service {
	logger := slog.New(slog.DiscardHandler)
	cacheClient := newInMemoryCacheClient()

	return &Service{
		requester:    requester,
		logger:       logger,
		cacheManager: newCacheManager(cacheClient, logger),
		mapper:       streammapping.NewStreamMapper(logger),
		filter:       streammapping.NewStreamFilter(logger),
	}
}

func newServiceForFallbackTestWithScraper(requester apiclient.Requester, scraperService *htmlscraper.Service) *Service {
	service := newServiceForFallbackTest(requester)

	service.scraper = scraperService

	return service
}
