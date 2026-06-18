package polling

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/stretchr/testify/require"
)

func goldenScraperConfig(tiering, backfill bool) config.ScraperConfig {
	cfg := config.ScraperConfig{
		Poll: config.ScraperPoll{
			Videos:    15 * time.Minute,
			Shorts:    6 * time.Minute,
			Community: 15 * time.Minute,
			Stats:     6 * time.Hour,
			Live:      2 * time.Minute,
		},
	}
	cfg.PollTiering.Enabled = tiering
	if backfill {
		cfg.Backfill = config.ScraperBackfillConfig{
			Enabled:           true,
			ShortsEnabled:     true,
			ShortsInterval:    5 * time.Minute,
			CommunityEnabled:  true,
			CommunityInterval: 10 * time.Minute,
			LiveEnabled:       true,
			LiveInterval:      3 * time.Minute,
			TargetGroup:       "notification",
		}
	}
	return cfg
}

func goldenBuildRegistrations(tiering, backfill bool, liveStatusProvider poller.LiveStatusProvider) []providers.ChannelPollerRegistration {
	postgres := &databasemocks.Client{}
	scraperConfig := goldenScraperConfig(tiering, backfill)
	return buildYouTubeProducerChannelPollerRegistrationsWithClient(
		context.Background(),
		postgres,
		&scraperConfig,
		nil,
		liveStatusProvider,
		nil,
		[]string{"UC_A", "UC_B", "UC_C"},
		[]string{"UC_STATS_1", "UC_STATS_2"},
	)
}

func serializeBudgetSourceUnits(units map[poller.BudgetSource]float64) string {
	if len(units) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(units))
	for source := range units {
		keys = append(keys, string(source))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, source := range keys {
		parts = append(parts, fmt.Sprintf("%s=%g", source, units[poller.BudgetSource(source)]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func serializeRegistration(r *providers.ChannelPollerRegistration) string {
	name := "<nil>"
	if r.Poller != nil {
		name = r.Poller.Name()
	}
	return strings.Join([]string{
		fmt.Sprintf("name=%s", name),
		fmt.Sprintf("priority=%d", r.Priority),
		fmt.Sprintf("interval=%s", r.Interval),
		fmt.Sprintf("targetGroup=%s", r.TargetGroup),
		fmt.Sprintf("channelIDs=[%s]", strings.Join(r.ChannelIDs, " ")),
		fmt.Sprintf("hasExplicitChannelIDs=%t", r.HasExplicitChannelIDs),
		fmt.Sprintf("requestsPerRun=%d", r.RequestsPerRun),
		fmt.Sprintf("worstCaseAttempts=%d", r.WorstCaseAttempts),
		fmt.Sprintf("worstCaseRequestUnitsPerRun=%g", r.WorstCaseRequestUnitsPerRun),
		fmt.Sprintf("hasBudgetProfile=%t", r.HasBudgetProfile),
		fmt.Sprintf("budgetBurstClass=%s", r.BudgetProfile.BurstClass),
		fmt.Sprintf("budgetPriority=%s", r.BudgetProfile.Priority),
		fmt.Sprintf("budgetSourceUnits=%s", serializeBudgetSourceUnits(r.BudgetProfile.SourceUnits)),
		fmt.Sprintf("budgetFallbackSourceUnits=%s", serializeBudgetSourceUnits(r.BudgetProfile.FallbackSourceUnits)),
	}, " | ")
}

func serializeRegistrations(registrations []providers.ChannelPollerRegistration) []string {
	lines := make([]string, 0, len(registrations))
	for i := range registrations {
		lines = append(lines, serializeRegistration(&registrations[i]))
	}
	return lines
}

func TestBuildYouTubeProducerChannelPollerRegistrationsGolden(t *testing.T) {
	cases := []struct {
		name       string
		tiering    bool
		backfill   bool
		liveStatus poller.LiveStatusProvider
		want       []string
	}{
		{
			name:       "flat_no_backfill_batch",
			tiering:    false,
			backfill:   false,
			liveStatus: registrationTestLiveStatusProvider{},
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live_batch | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[__global__] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={holodex_live=1,postgres_write=3} | budgetFallbackSourceUnits={youtube_scraper=9}",
			},
		},
		{
			name:       "flat_backfill_batch",
			tiering:    false,
			backfill:   true,
			liveStatus: registrationTestLiveStatusProvider{},
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live_batch | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[__global__] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={holodex_live=1,postgres_write=3} | budgetFallbackSourceUnits={youtube_scraper=9}",
				"name=shorts_backfill | priority=0 | interval=5m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community_backfill | priority=0 | interval=10m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=live_backfill_batch | priority=0 | interval=3m0s | targetGroup=notification | channelIDs=[__global__] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={holodex_live=1,postgres_write=3} | budgetFallbackSourceUnits={youtube_scraper=9}",
			},
		},
		{
			name:       "tiered_no_backfill_batch",
			tiering:    true,
			backfill:   false,
			liveStatus: registrationTestLiveStatusProvider{},
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=1 | interval=30m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=0 | interval=1h30m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live_batch | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[__global__] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={holodex_live=1,postgres_write=3} | budgetFallbackSourceUnits={youtube_scraper=9}",
			},
		},
		{
			name:       "tiered_backfill_batch",
			tiering:    true,
			backfill:   true,
			liveStatus: registrationTestLiveStatusProvider{},
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=1 | interval=30m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=0 | interval=1h30m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live_batch | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[__global__] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={holodex_live=1,postgres_write=3} | budgetFallbackSourceUnits={youtube_scraper=9}",
				"name=shorts_backfill | priority=0 | interval=5m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community_backfill | priority=0 | interval=10m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=live_backfill_batch | priority=0 | interval=3m0s | targetGroup=notification | channelIDs=[__global__] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={holodex_live=1,postgres_write=3} | budgetFallbackSourceUnits={youtube_scraper=9}",
			},
		},
		{
			name:       "flat_no_backfill_single",
			tiering:    false,
			backfill:   false,
			liveStatus: nil,
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=3 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={postgres_write=1,youtube_scraper=3} | budgetFallbackSourceUnits={}",
			},
		},
		{
			name:       "flat_backfill_single",
			tiering:    false,
			backfill:   true,
			liveStatus: nil,
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=3 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={postgres_write=1,youtube_scraper=3} | budgetFallbackSourceUnits={}",
				"name=shorts_backfill | priority=0 | interval=5m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community_backfill | priority=0 | interval=10m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=live_backfill | priority=0 | interval=3m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=3 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=3} | budgetFallbackSourceUnits={}",
			},
		},
		{
			name:       "tiered_no_backfill_single",
			tiering:    true,
			backfill:   false,
			liveStatus: nil,
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=1 | interval=30m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=0 | interval=1h30m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=3 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={postgres_write=1,youtube_scraper=3} | budgetFallbackSourceUnits={}",
			},
		},
		{
			name:       "tiered_backfill_single",
			tiering:    true,
			backfill:   true,
			liveStatus: nil,
			want: []string{
				"name=videos | priority=1 | interval=15m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=1 | interval=30m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=videos | priority=0 | interval=1h30m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=9 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=normal | budgetSourceUnits={postgres_write=1,youtube_scraper=9} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=shorts | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=6m0s | targetGroup=notification_active | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=12m0s | targetGroup=notification_warm | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community | priority=0 | interval=36m0s | targetGroup=notification_cold | channelIDs=[] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=channel_stats | priority=0 | interval=6h0m0s | targetGroup=stats | channelIDs=[UC_STATS_1 UC_STATS_2] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=6 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=6} | budgetFallbackSourceUnits={}",
				"name=live | priority=2 | interval=2m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=3 | hasBudgetProfile=true | budgetBurstClass=primary | budgetPriority=high | budgetSourceUnits={postgres_write=1,youtube_scraper=3} | budgetFallbackSourceUnits={}",
				"name=shorts_backfill | priority=0 | interval=5m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=community_backfill | priority=0 | interval=10m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=1 | worstCaseRequestUnitsPerRun=1 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=1} | budgetFallbackSourceUnits={}",
				"name=live_backfill | priority=0 | interval=3m0s | targetGroup=notification | channelIDs=[UC_A UC_B UC_C] | hasExplicitChannelIDs=true | requestsPerRun=1 | worstCaseAttempts=3 | worstCaseRequestUnitsPerRun=3 | hasBudgetProfile=true | budgetBurstClass=backfill | budgetPriority=low | budgetSourceUnits={postgres_write=1,youtube_scraper=3} | budgetFallbackSourceUnits={}",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serializeRegistrations(goldenBuildRegistrations(tc.tiering, tc.backfill, tc.liveStatus))
			require.Equal(t, tc.want, got)
		})
	}
}
