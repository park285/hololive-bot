package apiservice

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

// blockingStatsScraper는 호출 context가 끝날 때까지 기다렸다가 그 오류를 돌려준다.
type blockingStatsScraper struct{ *stubScraper }

func (*blockingStatsScraper) GetChannelStats(ctx context.Context, _ string) (*parser.ChannelStats, error) {
	<-ctx.Done()

	return nil, context.Cause(ctx)
}

// scraper 단계는 호출자 취소와 분리된 자체 예산(scraperPhaseTimeout)으로 돈다. 예산 소진으로 못 끝낸 채널은 호출자
// 취소가 아니라 원천 실패이므로 hololive_fallback_primary_total은 outcome="failed"로 센다(stack audit D1).
func TestScrapeChannelStatisticsRecordsPhaseTimeoutAsFailedPrimary(t *testing.T) {
	ys := &serviceImpl{
		scraper:             &blockingStatsScraper{stubScraper: &stubScraper{}},
		logger:              slog.New(slog.DiscardHandler),
		channelToName:       make(map[string]string),
		scraperPhaseTimeout: 20 * time.Millisecond,
	}

	canceledBefore := channelStatisticsPrimaryCount(t, "canceled")
	failedBefore := channelStatisticsPrimaryCount(t, "failed")

	result := ys.scrapeChannelStatistics(t.Context(), []string{testChannelID1})

	if !slices.Equal(result.failedIDs, []string{testChannelID1}) || result.scraped != 0 {
		t.Fatalf("scrape result = (failed %v, scraped %d), want phase-timeout channel as failed", result.failedIDs, result.scraped)
	}

	if got := channelStatisticsPrimaryCount(t, "canceled") - canceledBefore; got != 0 {
		t.Fatalf("channel_statistics outcome=canceled delta = %v, want 0 for phase budget exhaustion", got)
	}

	if got := channelStatisticsPrimaryCount(t, "failed") - failedBefore; got != 1 {
		t.Fatalf("channel_statistics outcome=failed delta = %v, want 1", got)
	}
}

func channelStatisticsPrimaryCount(t *testing.T, outcome string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	var total float64

	for _, family := range families {
		if family.GetName() != "hololive_fallback_primary_total" {
			continue
		}

		for _, metric := range family.GetMetric() {
			labels := map[string]string{}

			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}

			if labels["service"] == "youtube" && labels["operation"] == "channel_statistics" && labels["outcome"] == outcome {
				total += metric.GetCounter().GetValue()
			}
		}
	}

	return total
}
