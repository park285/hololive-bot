package holodexprovider

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var supportedStreamOrgParams = []string{
	strings.ToLower(constants.HolodexAPIParams.OrgHololive),
	strings.ToLower(constants.HolodexAPIParams.OrgVSpo),
	strings.ToLower(constants.HolodexAPIParams.OrgStellive),
	strings.ToLower(constants.HolodexAPIParams.OrgIndie),
	constants.HolodexAPIParams.OrgAll,
}

func SupportedStreamOrgParams() []string {
	return slices.Clone(supportedStreamOrgParams)
}

func (h *Service) GetLiveStreamsByOrg(ctx context.Context, org string) ([]*domain.Stream, error) {
	resolvedOrg, err := resolveStreamOrg(org)
	if err != nil {
		return nil, fmt.Errorf("resolve stream org: %w", err)
	}

	out, err := h.getStreamsByOrgWithFallback(ctx, &streamFetchPlan{
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
		retryKey: fmt.Sprintf("live_streams_%s", strings.ToLower(resolvedOrg)),
		retry: func(retryCtx context.Context, org string, _ int) {
			if _, getErr := h.GetLiveStreamsByOrg(retryCtx, org); getErr != nil && h.logger != nil {
				h.logger.Warn("holodex live streams retry failed", slog.String("org", org), slog.Any("error", getErr))
			}
		},
	})
	if err != nil {
		return out, fmt.Errorf("get streams by org with fallback: %w", err)
	}

	return out, nil
}

func (h *Service) GetUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, error) {
	out, err := h.GetUpcomingStreamsByOrg(ctx, hours, constants.HolodexAPIParams.OrgHololive)
	if err != nil {
		return out, fmt.Errorf("get upcoming streams by org: %w", err)
	}

	return out, nil
}

func (h *Service) GetUpcomingStreamsByOrg(ctx context.Context, hours int, org string) ([]*domain.Stream, error) {
	resolvedOrg, err := resolveStreamOrg(org)
	if err != nil {
		return nil, fmt.Errorf("resolve stream org: %w", err)
	}

	filterUpcoming := func(streams []*domain.Stream) []*domain.Stream {
		now := time.Now()

		var until time.Time

		if hours > 0 {
			until = now.Add(time.Duration(hours) * time.Hour)
		}

		return h.filter.FilterUpcomingStreamsInWindow(filterStreamsByStatus(streams, domain.StreamStatusUpcoming), now, until)
	}

	out, err := h.getStreamsByOrgWithFallback(ctx, &streamFetchPlan{
		resolvedOrg: resolvedOrg,
		status:      constants.HolodexAPIParams.StatusUpcoming,
		hours:       hours,
		operation:   "upcoming_streams",
		cacheGet: func(cacheCtx context.Context, org string, hours int) ([]*domain.Stream, bool) {
			streams, found := h.cacheManager.GetUpcomingStreamsByOrg(cacheCtx, org, hours)
			if !found {
				return streams, false
			}

			return filterUpcoming(streams), true
		},
		cacheSet: func(cacheCtx context.Context, org string, hours int, streams []*domain.Stream) {
			h.cacheManager.SetUpcomingStreamsByOrg(cacheCtx, org, hours, streams)
		},
		primaryFilter:  filterUpcoming,
		fallbackFilter: filterUpcoming,
		retryKey:       fmt.Sprintf("upcoming_%s_%d", strings.ToLower(resolvedOrg), hours),
		retry: func(retryCtx context.Context, org string, hours int) {
			if _, getUpcomingErr := h.GetUpcomingStreamsByOrg(retryCtx, hours, org); getUpcomingErr != nil && h.logger != nil {
				h.logger.Warn("holodex upcoming streams retry failed", slog.String("org", org), slog.Int("hours", hours), slog.Any("error", getUpcomingErr))
			}
		},
		fallbackLogMessage: "Holodex upcoming stream source failed; using official schedule API",
	})
	if err != nil {
		return out, fmt.Errorf("get streams by org with fallback: %w", err)
	}

	return out, nil
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
	fallbackFilter     func(streams []*domain.Stream) []*domain.Stream
	retry              func(ctx context.Context, org string, hours int)
}

func (p *streamFetchPlan) cacheKey() string {
	if p.status == constants.HolodexAPIParams.StatusLive {
		return buildLiveStreamsCacheKey(p.resolvedOrg)
	}

	return buildUpcomingStreamsCacheKey(p.resolvedOrg, p.hours)
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
	params.Set("limit", fmt.Sprintf("%d", constants.HolodexAPIParams.StreamListLimit))

	if status == constants.HolodexAPIParams.StatusUpcoming {
		params.Set("max_upcoming_hours", fmt.Sprintf("%d", min(hours, constants.HolodexAPIParams.MaxUpcomingHours)))
		params.Set("order", "asc")
		params.Set("sort", "start_scheduled")
	}

	body, err := h.requester.DoRequest(ctx, http.MethodGet, "/live", params)
	if err != nil {
		return nil, fmt.Errorf("get streams by org (%s): %w", org, err)
	}

	var rawStreams []streammapping.StreamRaw

	if err := jsonv2.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal streams by org (%s): %w", org, err)
	}

	return limitStreamList(h.mapper.MapStreamsResponse(rawStreams)), nil
}

func resolveStreamOrg(org string) (string, error) {
	if resolvedOrg, ok := streamOrgAliases[normalizeStreamOrg(org)]; ok {
		return resolvedOrg, nil
	}

	return "", fmt.Errorf("%w: %s", ErrInvalidStreamOrg, strings.TrimSpace(org))
}

var streamOrgAliases = map[string]string{
	"":      constants.HolodexAPIParams.OrgHololive,
	"holo":  constants.HolodexAPIParams.OrgHololive,
	"indie": constants.HolodexAPIParams.OrgIndie,
	strings.ToLower(constants.HolodexAPIParams.OrgHololive): constants.HolodexAPIParams.OrgHololive,
	strings.ToLower(constants.HolodexAPIParams.OrgVSpo):     constants.HolodexAPIParams.OrgVSpo,
	strings.ToLower(constants.HolodexAPIParams.OrgStellive): constants.HolodexAPIParams.OrgStellive,
	strings.ToLower(constants.HolodexAPIParams.OrgIndie):    constants.HolodexAPIParams.OrgIndie,
	constants.HolodexAPIParams.OrgAll:                       constants.HolodexAPIParams.OrgAll,
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

func holodexOrgFetchParallelism(org string, orgAllParallelism int) int {
	if org != constants.HolodexAPIParams.OrgAll {
		return 1
	}

	if orgAllParallelism > 1 {
		return orgAllParallelism
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

func limitStreamList(streams []*domain.Stream) []*domain.Stream {
	limit := constants.HolodexAPIParams.StreamListLimit
	if limit < 1 || len(streams) <= limit {
		return streams
	}

	return streams[:limit]
}

func normalizeStreamOrg(org string) string {
	normalized := strings.ToLower(strings.TrimSpace(org))
	return strings.TrimSuffix(normalized, "!")
}

func (h *Service) fetchIndieStreams(ctx context.Context) ([]*domain.Stream, error) {
	if len(constants.IndieChannelIDs) == 0 {
		return nil, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(constants.IndieChannelIDs, ","))

	body, err := h.requester.DoRequest(ctx, http.MethodGet, usersLivePath, params)
	if err != nil {
		return nil, fmt.Errorf("fetch indie streams: %w", err)
	}

	var rawStreams []streammapping.StreamRaw

	if err := jsonv2.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal indie streams: %w", err)
	}

	streams := limitStreamList(h.mapper.MapStreamsResponse(rawStreams))
	h.hydrateIndieStreamChannels(streams, constants.IndieChannelIDs)

	return streams, nil
}
