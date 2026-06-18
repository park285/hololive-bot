package holodexprovider

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *Service) fetchHololiveChannelList(ctx context.Context) ([]*domain.Channel, error) {
	if cached, found := h.cacheManager.GetHololiveChannelList(ctx); found {
		return cached, nil
	}

	allChannels, err := h.fetchHololiveChannelListPages(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Debug("Fetched all Hololive channels", slog.Int("total", len(allChannels)))
	h.cacheManager.SetHololiveChannelList(ctx, allChannels, 5*time.Minute)

	return allChannels, nil
}

func (h *Service) fetchHololiveChannelListPages(ctx context.Context) ([]*domain.Channel, error) {
	var allChannels []*domain.Channel
	pageSize := constants.HolodexAPIParams.DefaultChannelLimit
	offset := 0
	for {
		channels, rawCount, err := h.fetchHololiveChannelListPage(ctx, pageSize, offset)
		if err != nil {
			return nil, err
		}

		allChannels = append(allChannels, channels...)
		if rawCount < pageSize {
			break
		}

		offset += pageSize
		if h.channelListPaginationLimitReached(offset) {
			break
		}
	}

	return allChannels, nil
}

func (h *Service) fetchHololiveChannelListPage(ctx context.Context, pageSize, offset int) ([]*domain.Channel, int, error) {
	params := url.Values{}
	params.Set("org", constants.HolodexAPIParams.OrgHololive)
	params.Set("type", constants.HolodexAPIParams.TypeVtuber)
	params.Set("limit", fmt.Sprintf("%d", pageSize))
	params.Set("offset", fmt.Sprintf("%d", offset))

	body, err := h.requester.DoRequest(ctx, "GET", "/channels", params)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch hololive channel list (offset=%d): %w", offset, err)
	}

	var rawChannels []ChannelRaw
	if err := json.Unmarshal(body, &rawChannels); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal channel list: %w", err)
	}

	return h.mapper.MapChannelsResponse(rawChannels), len(rawChannels), nil
}

func (h *Service) channelListPaginationLimitReached(offset int) bool {
	if offset < constants.HolodexAPIParams.MaxPaginationOffset {
		return false
	}
	h.logger.Warn("Pagination limit reached", slog.Int("offset", offset))
	return true
}

func (h *Service) fetchChannelsIndividually(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, missedIDs []string) (map[string]*domain.Channel, error) {
	const maxConcurrent = 5
	if len(missedIDs) == 0 {
		return result, nil
	}

	workerCount := min(maxConcurrent, len(missedIDs))
	jobs := make(chan string)
	resultChan := make(chan channelFetchResult, len(missedIDs))
	workerWG := h.startChannelFetchWorkers(ctx, workerCount, jobs, resultChan)

	go func() {
		enqueueChannelFetchJobs(ctx, jobs, missedIDs)
	}()

	go func() {
		workerWG.Wait()
		close(resultChan)
	}()

	return h.collectIndividualChannelFetchResults(ctx, channelIDs, result, resultChan)
}

func (h *Service) startChannelFetchWorkers(ctx context.Context, workerCount int, jobs <-chan string, resultChan chan<- channelFetchResult) *sync.WaitGroup {
	workerWG := &sync.WaitGroup{}
	workerWG.Add(workerCount)
	for range workerCount {
		go h.runChannelFetchWorker(ctx, jobs, resultChan, workerWG)
	}
	return workerWG
}

func (h *Service) runChannelFetchWorker(ctx context.Context, jobs <-chan string, resultChan chan<- channelFetchResult, workerWG *sync.WaitGroup) {
	defer workerWG.Done()
	for channelID := range jobs {
		if ctx.Err() != nil {
			resultChan <- channelFetchResult{id: channelID}
			continue
		}
		resultChan <- h.fetchIndividualChannel(ctx, channelID)
	}
}

func (h *Service) fetchIndividualChannel(ctx context.Context, channelID string) channelFetchResult {
	channel, err := h.fetchChannelDirect(ctx, channelID)
	if err != nil {
		h.logger.Warn("Failed to get channel in batch",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)
		return channelFetchResult{id: channelID}
	}
	return channelFetchResult{id: channelID, channel: channel}
}

func enqueueChannelFetchJobs(ctx context.Context, jobs chan<- string, missedIDs []string) {
	defer close(jobs)
	for _, id := range missedIDs {
		if !sendChannelFetchJob(ctx, jobs, id) {
			return
		}
	}
}

func sendChannelFetchJob(ctx context.Context, jobs chan<- string, id string) bool {
	select {
	case <-ctx.Done():
		return false
	case jobs <- id:
		return true
	}
}

func (h *Service) collectIndividualChannelFetchResults(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, resultChan <-chan channelFetchResult) (map[string]*domain.Channel, error) {
	for {
		r, ok, err := nextChannelFetchResult(ctx, resultChan)
		if err != nil {
			return result, err
		}
		if !ok {
			h.logger.Info("GetChannels batch complete (fallback)",
				slog.Int("requested", len(channelIDs)),
				slog.Int("returned", len(result)),
			)
			return result, nil
		}
		if r.channel != nil {
			result[r.id] = r.channel
		}
	}
}

func nextChannelFetchResult(ctx context.Context, resultChan <-chan channelFetchResult) (channelFetchResult, bool, error) {
	select {
	case <-ctx.Done():
		return channelFetchResult{}, false, fmt.Errorf("batch channel fetch canceled: %w", ctx.Err())
	case result, ok := <-resultChan:
		return result, ok, nil
	}
}
