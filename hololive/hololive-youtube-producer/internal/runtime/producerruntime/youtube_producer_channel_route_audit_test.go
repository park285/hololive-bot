package producerruntime

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"

	pollscheduler "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
)

func TestBuildYouTubeProducerYouTubeComponents_OmitsVideosAndShortsForEveryActiveChannel(t *testing.T) {
	t.Parallel()

	operationalChannels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: " UC_ACTIVE_A ", Name: "A"},
			{ChannelID: "UC_ACTIVE_B", Name: "B"},
			{ChannelID: "   ", Name: "Missing"},
			{ChannelID: "UC_GRADUATED", Name: "G", IsGraduated: true},
		},
	})

	scraperScheduler, registrations, err := polling.BuildComponents(
		&settings.ScraperConfig{
			WorkerCount: 2,
			Poll: settings.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		newPollerRegistrationTestDB(t),
		communityshorts.EnabledChannelIDs(operationalChannels),
		communityshorts.EnabledChannelIDs(operationalChannels),
		polling.BuildSharedClient(&settings.ScraperConfig{}, nil, nil),
		nil,
		testLogger(),
	)
	require.NoError(t, err)

	require.NotNil(t, scraperScheduler)
	require.Len(t, registrations, 2)
	require.Empty(t, contentPollerJobKeys(t, scraperScheduler))
}

func contentPollerJobKeys(t *testing.T, scheduler *pollscheduler.Scheduler) []string {
	t.Helper()

	require.NotNil(t, scheduler)
	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")

	keys := make([]string, 0, field.Len())
	iterator := field.MapRange()
	for iterator.Next() {
		key := iterator.Key().String()
		if key == "" {
			continue
		}
		if key == "UC_GRADUATED:community" || key == "UC_GRADUATED:shorts" {
			t.Fatalf("graduated content poller key registered: %s", key)
		}
		if len(key) >= len(":community") && key[len(key)-len(":community"):] == ":community" {
			keys = append(keys, key)
			continue
		}
		if len(key) >= len(":shorts") && key[len(key)-len(":shorts"):] == ":shorts" {
			keys = append(keys, key)
		}
	}

	slices.Sort(keys)
	return keys
}
