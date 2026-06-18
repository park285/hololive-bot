package polling

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

const defaultLiveBatchChannelChunkSize = 40

type livePollerRegistrationSpec struct {
	Name           string
	Base           poller.Poller
	BatchBase      *poller.LivePoller
	BatchEnabled   bool
	Priority       poller.Priority
	Interval       time.Duration
	ChannelIDs     []string
	TargetGroup    providers.ChannelTargetGroup
	BurstClass     poller.BudgetBurstClass
	BudgetPriority poller.BudgetPriority
}

type liveBatchPoller struct {
	name       string
	base       *poller.LivePoller
	channelIDs []string
}

func appendLivePollerRegistrations(
	registrations []providers.ChannelPollerRegistration,
	spec *livePollerRegistrationSpec,
) []providers.ChannelPollerRegistration {
	channelIDs := uniqueLiveRegistrationChannelIDs(spec.ChannelIDs)
	if spec.Base == nil || spec.Interval <= 0 {
		return registrations
	}
	if spec.TargetGroup == "" {
		spec.TargetGroup = providers.ChannelTargetGroupNotification
	}
	if spec.BatchEnabled && spec.BatchBase != nil && len(channelIDs) > 0 {
		return appendLiveBatchPollerRegistrations(registrations, spec, channelIDs)
	}
	return append(registrations, providers.NewChannelPollerRegistration(spec.Base, spec.Priority, spec.Interval).
		WithChannelIDs(channelIDs).
		WithTargetGroup(spec.TargetGroup).
		WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
		WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)).
		WithBudgetProfile(youtubeScraperBudgetProfile(float64(scraper.FetchPageMaxAttempts), spec.BurstClass, spec.BudgetPriority)))
}

func appendLiveBatchPollerRegistrations(
	registrations []providers.ChannelPollerRegistration,
	spec *livePollerRegistrationSpec,
	channelIDs []string,
) []providers.ChannelPollerRegistration {
	chunks := chunkLiveRegistrationChannelIDs(channelIDs, defaultLiveBatchChannelChunkSize)
	for idx, chunk := range chunks {
		name := liveBatchRegistrationName(spec.Name, idx, len(chunks))
		batchPoller := newLiveBatchPoller(name, spec.BatchBase, chunk)
		fallbackUnits := liveBatchYouTubeScraperFallbackUnits(len(chunk))
		registrations = append(registrations, providers.NewChannelPollerRegistration(batchPoller, spec.Priority, spec.Interval).
			WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
			WithTargetGroup(spec.TargetGroup).
			WithWorstCaseAttempts(1).
			WithWorstCaseRequestUnitsPerRun(fallbackUnits).
			WithBudgetProfile(holodexLiveBatchBudgetProfile(len(chunk), spec.BurstClass, spec.BudgetPriority)))
	}
	return registrations
}

func newLiveBatchPoller(name string, base *poller.LivePoller, channelIDs []string) poller.Poller {
	return &liveBatchPoller{
		name:       strings.TrimSpace(name),
		base:       base,
		channelIDs: append([]string(nil), channelIDs...),
	}
}

func (p *liveBatchPoller) Poll(ctx context.Context, _ string) error {
	if p == nil || p.base == nil {
		return fmt.Errorf("live batch poller %s has no base poller", p.Name())
	}
	errs := p.base.PollBatch(ctx, p.channelIDs)
	return joinLiveBatchErrors(errs)
}

func (p *liveBatchPoller) Name() string {
	if p == nil || strings.TrimSpace(p.name) == "" {
		return "live_batch"
	}
	return p.name
}

func joinLiveBatchErrors(errs map[string]error) error {
	if len(errs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(errs))
	for channelID, err := range errs {
		if err != nil {
			keys = append(keys, channelID)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)

	joined := make([]error, 0, len(keys))
	for _, channelID := range keys {
		joined = append(joined, fmt.Errorf("%s: %w", channelID, errs[channelID]))
	}
	return errors.Join(joined...)
}

func uniqueLiveRegistrationChannelIDs(channelIDs []string) []string {
	seen := make(map[string]struct{}, len(channelIDs))
	unique := make([]string, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		trimmed := strings.TrimSpace(channelID)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	return unique
}

func chunkLiveRegistrationChannelIDs(channelIDs []string, chunkSize int) [][]string {
	if chunkSize <= 0 {
		chunkSize = defaultLiveBatchChannelChunkSize
	}
	chunks := make([][]string, 0, (len(channelIDs)+chunkSize-1)/chunkSize)
	for start := 0; start < len(channelIDs); start += chunkSize {
		end := min(start+chunkSize, len(channelIDs))
		chunks = append(chunks, append([]string(nil), channelIDs[start:end]...))
	}
	return chunks
}

func liveBatchRegistrationName(baseName string, index, total int) string {
	trimmed := strings.TrimSpace(baseName)
	if trimmed == "" {
		trimmed = "live"
	}
	if total <= 1 {
		return trimmed + "_batch"
	}
	return fmt.Sprintf("%s_batch_%02d", trimmed, index+1)
}

func holodexLiveBatchBudgetProfile(channelCount int, class poller.BudgetBurstClass, priority poller.BudgetPriority) poller.BudgetProfile {
	if channelCount < 1 {
		channelCount = 1
	}
	return poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{
			poller.BudgetSourceHolodexLive:   1,
			poller.BudgetSourcePostgresWrite: float64(channelCount),
		},
		FallbackSourceUnits: map[poller.BudgetSource]float64{
			poller.BudgetSourceYouTubeScraper: liveBatchYouTubeScraperFallbackUnits(channelCount),
		},
		BurstClass: class,
		Priority:   priority,
	}
}

func liveBatchYouTubeScraperFallbackUnits(channelCount int) float64 {
	if channelCount < 1 {
		channelCount = 1
	}
	return float64(channelCount * scraper.FetchPageMaxAttempts)
}
