package app

import (
	"reflect"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func TestBuildStreamIngesterYouTubeComponents_RegistersCommunityAndShortsForEveryActiveChannel(t *testing.T) {
	t.Parallel()

	operationalChannels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: " UC_ACTIVE_A ", Name: "A"},
			{ChannelID: "UC_ACTIVE_B", Name: "B"},
			{ChannelID: "   ", Name: "Missing"},
			{ChannelID: "UC_GRADUATED", Name: "G", IsGraduated: true},
		},
	})

	scraperScheduler, outboxDispatcher := buildStreamIngesterYouTubeComponents(
		config.ScraperConfig{
			WorkerCount: 2,
			Poll: config.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		&databasemocks.Client{},
		communityShortsEnabledChannelIDs(operationalChannels),
		nil,
		nil,
		nil,
		nil,
		nil,
		testLogger(),
	)

	require.NotNil(t, scraperScheduler)
	require.NotNil(t, outboxDispatcher)
	require.ElementsMatch(t,
		[]string{
			"UC_ACTIVE_A:community",
			"UC_ACTIVE_A:shorts",
			"UC_ACTIVE_B:community",
			"UC_ACTIVE_B:shorts",
		},
		contentPollerJobKeys(t, scraperScheduler),
	)
}

func contentPollerJobKeys(t *testing.T, scheduler *poller.Scheduler) []string {
	t.Helper()

	require.NotNil(t, scheduler)
	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")

	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
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
