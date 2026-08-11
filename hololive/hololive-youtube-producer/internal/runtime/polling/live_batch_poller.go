package polling

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
)

const defaultLiveBatchChannelChunkSize = 40

type livePollerRegistrationSpec struct {
	Name           string
	Base           scheduler.Poller
	BatchBase      *pollers.LivePoller
	BatchEnabled   bool
	Priority       scheduler.Priority
	Interval       time.Duration
	ChannelIDs     []string
	TargetGroup    providers.ChannelTargetGroup
	BurstClass     polling.BudgetBurstClass
	BudgetPriority polling.BudgetPriority
}

type liveBatchPoller struct {
	name           string
	base           *pollers.LivePoller
	channelIDs     []string
	burstClass     polling.BudgetBurstClass
	budgetPriority polling.BudgetPriority
}

var _ providers.ChannelTargetSnapshotPoller = (*liveBatchPoller)(nil)

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
	if spec.BatchEnabled && spec.BatchBase != nil {
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
	batchPoller := newLiveBatchPoller(
		liveBatchRegistrationName(spec.Name),
		spec.BatchBase,
		channelIDs,
		spec.BurstClass,
		spec.BudgetPriority,
	)
	fallbackUnits := liveBatchYouTubeScraperFallbackUnits(len(channelIDs))
	return append(registrations, providers.NewChannelPollerRegistration(batchPoller, spec.Priority, spec.Interval).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithTargetGroup(spec.TargetGroup).
		WithWorstCaseAttempts(1).
		WithWorstCaseRequestUnitsPerRun(fallbackUnits).
		WithBudgetProfile(batchPoller.budgetProfile()))
}

func newLiveBatchPoller(
	name string,
	base *pollers.LivePoller,
	channelIDs []string,
	burstClass polling.BudgetBurstClass,
	budgetPriority polling.BudgetPriority,
) *liveBatchPoller {
	return &liveBatchPoller{
		name:           strings.TrimSpace(name),
		base:           base,
		channelIDs:     uniqueLiveRegistrationChannelIDs(channelIDs),
		burstClass:     burstClass,
		budgetPriority: budgetPriority,
	}
}

func (p *liveBatchPoller) Poll(ctx context.Context, _ string) error {
	if p == nil || p.base == nil {
		return fmt.Errorf("live batch poller %s has no base poller", p.Name())
	}
	var batchErrs []error
	for _, chunk := range chunkLiveRegistrationChannelIDs(p.channelIDs, defaultLiveBatchChannelChunkSize) {
		if err := ctx.Err(); err != nil {
			batchErrs = append(batchErrs, err)
			break
		}
		if err := joinLiveBatchErrors(p.base.PollBatch(ctx, chunk)); err != nil {
			batchErrs = append(batchErrs, err)
		}
	}
	return errors.Join(batchErrs...)
}

func (p *liveBatchPoller) Name() string {
	if p == nil || strings.TrimSpace(p.name) == "" {
		return "live_batch"
	}
	return p.name
}

func (p *liveBatchPoller) ChannelTargets() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.channelIDs...)
}

func (p *liveBatchPoller) WithChannelTargets(channelIDs []string) (scheduler.Poller, polling.BudgetProfile) {
	if p == nil {
		return nil, holodexLiveBatchBudgetProfile(0, "", "")
	}
	updated := newLiveBatchPoller(p.name, p.base, channelIDs, p.burstClass, p.budgetPriority)
	profile := updated.budgetProfile()
	return updated, profile
}

func (p *liveBatchPoller) budgetProfile() polling.BudgetProfile {
	if p == nil {
		return holodexLiveBatchBudgetProfile(0, "", "")
	}
	return holodexLiveBatchBudgetProfile(len(p.channelIDs), p.burstClass, p.budgetPriority)
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

func liveBatchRegistrationName(baseName string) string {
	trimmed := strings.TrimSpace(baseName)
	if trimmed == "" {
		trimmed = "live"
	}
	return trimmed + "_batch"
}

func holodexLiveBatchBudgetProfile(channelCount int, class polling.BudgetBurstClass, priority polling.BudgetPriority) polling.BudgetProfile {
	channelCount = max(channelCount, 0)
	holodexUnits := float64((channelCount + defaultLiveBatchChannelChunkSize - 1) / defaultLiveBatchChannelChunkSize)
	sourceUnits := make(map[polling.BudgetSource]float64)
	fallbackSourceUnits := make(map[polling.BudgetSource]float64)
	if channelCount > 0 {
		sourceUnits[polling.BudgetSourceHolodexLive] = holodexUnits
		sourceUnits[polling.BudgetSourcePostgresWrite] = float64(channelCount)
		fallbackSourceUnits[polling.BudgetSourceYouTubeScraper] = liveBatchYouTubeScraperFallbackUnits(channelCount)
	}
	return polling.BudgetProfile{
		SourceUnits:         sourceUnits,
		FallbackSourceUnits: fallbackSourceUnits,
		BurstClass:          class,
		Priority:            priority,
	}
}

func liveBatchYouTubeScraperFallbackUnits(channelCount int) float64 {
	channelCount = max(channelCount, 0)
	attempts := scraper.LiveStatusFallbackFetchPolicy.MaxAttempts
	if attempts <= 0 {
		attempts = scraper.FetchPageMaxAttempts
	}
	return float64(channelCount * attempts)
}
