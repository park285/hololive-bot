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

package holodexprovider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

func SupportedStreamOrgParams() []string {
	return []string{
		strings.ToLower(constants.HolodexAPIParams.OrgHololive),
		strings.ToLower(constants.HolodexAPIParams.OrgVSpo),
		strings.ToLower(constants.HolodexAPIParams.OrgStellive),
		strings.ToLower(constants.HolodexAPIParams.OrgIndie),
		constants.HolodexAPIParams.OrgAll,
	}
}

func (h *Service) GetLiveStreams(ctx context.Context) ([]*domain.Stream, error) {
	return h.GetLiveStreamsByOrg(ctx, constants.HolodexAPIParams.OrgHololive)
}

// org 미지정 시 Hololive를 기본값으로 사용합니다.
func (h *Service) GetLiveStreamsByOrg(ctx context.Context, org string) ([]*domain.Stream, error) {
	resolvedOrg, err := resolveStreamOrg(org)
	if err != nil {
		return nil, err
	}

	return h.getStreamsByOrgWithFallback(ctx, streamFetchPlan{
		resolvedOrg: resolvedOrg,
		status:      constants.HolodexAPIParams.StatusLive,
		operation:   "live_streams",
		cacheGet: func(cacheCtx context.Context, org string, _ int) ([]*domain.Stream, bool) {
			return h.cacheManager.GetLiveStreamsByOrg(cacheCtx, org)
		},
		cacheSet: func(cacheCtx context.Context, org string, _ int, streams []*domain.Stream) {
			h.cacheManager.SetLiveStreamsByOrg(cacheCtx, org, streams)
		},
		primaryFilter: func(streams []*domain.Stream) []*domain.Stream {
			return filterStreamsByStatus(streams, domain.StreamStatusLive)
		},
		scraperFilter: func(streams []*domain.Stream) []*domain.Stream {
			return filterStreamsByStatus(streams, domain.StreamStatusLive)
		},
		retryKey: fmt.Sprintf("live_streams_%s", strings.ToLower(resolvedOrg)),
		retry: func(retryCtx context.Context, org string, _ int) {
			_, _ = h.GetLiveStreamsByOrg(retryCtx, org)
		},
		fallbackLogMessage: "Primary org fetch returned no live streams, using scraper fallback",
	})
}

func (h *Service) GetUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, error) {
	return h.GetUpcomingStreamsByOrg(ctx, hours, constants.HolodexAPIParams.OrgHololive)
}

// org 미지정 시 Hololive를 기본값으로 사용합니다.
func (h *Service) GetUpcomingStreamsByOrg(ctx context.Context, hours int, org string) ([]*domain.Stream, error) {
	resolvedOrg, err := resolveStreamOrg(org)
	if err != nil {
		return nil, err
	}

	return h.getStreamsByOrgWithFallback(ctx, streamFetchPlan{
		resolvedOrg: resolvedOrg,
		status:      constants.HolodexAPIParams.StatusUpcoming,
		hours:       hours,
		operation:   "upcoming_streams",
		cacheGet: func(cacheCtx context.Context, org string, hours int) ([]*domain.Stream, bool) {
			return h.cacheManager.GetUpcomingStreamsByOrg(cacheCtx, org, hours)
		},
		cacheSet: func(cacheCtx context.Context, org string, hours int, streams []*domain.Stream) {
			h.cacheManager.SetUpcomingStreamsByOrg(cacheCtx, org, hours, streams)
		},
		primaryFilter: func(streams []*domain.Stream) []*domain.Stream {
			return h.filter.FilterUpcomingStreams(filterStreamsByStatus(streams, domain.StreamStatusUpcoming))
		},
		scraperFilter: func(streams []*domain.Stream) []*domain.Stream {
			return h.filter.FilterUpcomingStreams(filterStreamsByStatus(streams, domain.StreamStatusUpcoming))
		},
		retryKey: fmt.Sprintf("upcoming_%s_%d", strings.ToLower(resolvedOrg), hours),
		retry: func(retryCtx context.Context, org string, hours int) {
			_, _ = h.GetUpcomingStreamsByOrg(retryCtx, hours, org)
		},
		fallbackLogMessage: "Primary org fetch returned no upcoming streams, using scraper fallback",
	})
}

type streamFetchPlan struct {
	resolvedOrg        string
	status             string
	hours              int
	operation          string
	retryKey           string
	fallbackLogMessage string
	cacheGet           func(ctx context.Context, org string, hours int) ([]*domain.Stream, bool)
	cacheSet           func(ctx context.Context, org string, hours int, streams []*domain.Stream)
	primaryFilter      func(streams []*domain.Stream) []*domain.Stream
	scraperFilter      func(streams []*domain.Stream) []*domain.Stream
	retry              func(ctx context.Context, org string, hours int)
}

func (h *Service) fetchStreamsByOrg(ctx context.Context, org, status string, hours int) ([]*domain.Stream, error) {
	if org == constants.HolodexAPIParams.OrgIndie {
		streams, err := h.fetchIndieStreams(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch indie streams: %w", err)
		}
		return streams, nil
	}

	params := url.Values{}
	params.Set("org", org)
	params.Set("status", status)
	params.Set("type", constants.HolodexAPIParams.TypeStream)
	if status == constants.HolodexAPIParams.StatusUpcoming {
		params.Set("max_upcoming_hours", fmt.Sprintf("%d", util.Min(hours, constants.HolodexAPIParams.MaxUpcomingHours)))
		params.Set("order", "asc")
		params.Set("sort", "start_scheduled")
	}

	body, err := h.requester.DoRequest(ctx, "GET", "/live", params)
	if err != nil {
		return nil, fmt.Errorf("get streams by org (%s): %w", org, err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal streams by org (%s): %w", org, err)
	}

	return h.mapper.MapStreamsResponse(rawStreams), nil
}

func resolveStreamOrg(org string) (string, error) {
	if resolvedOrg, ok := streamOrgAliases()[normalizeStreamOrg(org)]; ok {
		return resolvedOrg, nil
	}
	return "", fmt.Errorf("%w: %s", ErrInvalidStreamOrg, stringutil.TrimSpace(org))
}

func streamOrgAliases() map[string]string {
	return map[string]string{
		"":     constants.HolodexAPIParams.OrgHololive,
		"holo": constants.HolodexAPIParams.OrgHololive,
		strings.ToLower(constants.HolodexAPIParams.OrgHololive): constants.HolodexAPIParams.OrgHololive,
		strings.ToLower(constants.HolodexAPIParams.OrgVSpo):     constants.HolodexAPIParams.OrgVSpo,
		strings.ToLower(constants.HolodexAPIParams.OrgStellive): constants.HolodexAPIParams.OrgStellive,
		strings.ToLower(constants.HolodexAPIParams.OrgIndie):    constants.HolodexAPIParams.OrgIndie,
		constants.HolodexAPIParams.OrgAll:                       constants.HolodexAPIParams.OrgAll,
	}
}

func streamTargetOrgs(org string) []string {
	if org != constants.HolodexAPIParams.OrgAll {
		return []string{org}
	}

	targets := make([]string, 0, len(constants.HolodexAPIParams.SyncTargetOrgs)+1)
	targets = append(targets, constants.HolodexAPIParams.SyncTargetOrgs...)
	targets = append(targets, constants.HolodexAPIParams.OrgIndie)
	return targets
}

func holodexOrgFetchParallelism(org string) int {
	if org != constants.HolodexAPIParams.OrgAll {
		return 1
	}
	if constants.HolodexConcurrencyConfig.OrgAllParallelism > 1 {
		return constants.HolodexConcurrencyConfig.OrgAllParallelism
	}
	return 1
}

func filterStreamsByRequestedOrg(streams []*domain.Stream, org string) []*domain.Stream {
	if org == constants.HolodexAPIParams.OrgAll {
		return streams
	}

	target := normalizeStreamOrg(org)
	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream.Channel == nil || stream.Channel.Org == nil {
			continue
		}
		if normalizeStreamOrg(*stream.Channel.Org) == target {
			filtered = append(filtered, stream)
		}
	}
	return filtered
}

func filterStreamsByStatus(streams []*domain.Stream, status domain.StreamStatus) []*domain.Stream {
	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream.Status == status {
			filtered = append(filtered, stream)
		}
	}
	return filtered
}

func supportsScraperFallback(org string) bool {
	return org == constants.HolodexAPIParams.OrgHololive
}

func normalizeStreamOrg(org string) string {
	normalized := strings.ToLower(stringutil.TrimSpace(org))
	return strings.TrimSuffix(normalized, "!")
}

// fetchIndieStreams: 개인세 VTuber 채널의 라이브 스트림을 조회합니다.
// Holodex /users/live API를 사용하여 채널 ID 기반으로 조회합니다.
func (h *Service) fetchIndieStreams(ctx context.Context) ([]*domain.Stream, error) {
	if len(constants.IndieChannelIDs) == 0 {
		return nil, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(constants.IndieChannelIDs, ","))

	body, err := h.requester.DoRequest(ctx, "GET", "/users/live", params)
	if err != nil {
		return nil, fmt.Errorf("fetch indie streams: %w", err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal indie streams: %w", err)
	}

	streams := h.mapper.MapStreamsResponse(rawStreams)
	h.hydrateIndieStreamChannels(streams, constants.IndieChannelIDs)

	return streams, nil
}
