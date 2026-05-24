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

package alarmservice

import (
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/alarmcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSubscribersKeyByType(t *testing.T) {
	t.Parallel()

	as := &AlarmService{}
	tests := []struct {
		name      string
		channelID string
		alarmType domain.AlarmType
		want      string
	}{
		{
			name:      "live uses default prefix",
			channelID: "UC_live",
			alarmType: domain.AlarmTypeLive,
			want:      ChannelSubscribersKeyPrefix + "UC_live",
		},
		{
			name:      "community uses dedicated prefix",
			channelID: "UC_community",
			alarmType: domain.AlarmTypeCommunity,
			want:      ChannelSubscribersCommunityPrefix + "UC_community",
		},
		{
			name:      "shorts uses dedicated prefix",
			channelID: "UC_shorts",
			alarmType: domain.AlarmTypeShorts,
			want:      ChannelSubscribersShortsPrefix + "UC_shorts",
		},
		{
			name:      "unknown type falls back to default",
			channelID: "UC_unknown",
			alarmType: domain.AlarmType("UNKNOWN"),
			want:      ChannelSubscribersKeyPrefix + "UC_unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := as.channelSubscribersKeyByType(tt.channelID, tt.alarmType); got != tt.want {
				t.Fatalf("channelSubscribersKeyByType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannelContentAlarmTargetKeysMatchSharedSchema(t *testing.T) {
	t.Parallel()

	as := &AlarmService{}
	targets := sharedalarmkeys.BuildChannelContentAlarmTargetKeys("UC_bundle")

	assert.Equal(t, targets.CommunitySubscribersKey, as.channelSubscribersKeyByType("UC_bundle", domain.AlarmTypeCommunity))
	assert.Equal(t, targets.ShortsSubscribersKey, as.channelSubscribersKeyByType("UC_bundle", domain.AlarmTypeShorts))
	assert.Empty(t, targets.KeyFor(domain.AlarmTypeLive))
}

func TestBuildTitleFingerprint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		streamID string
		wantLen  int
	}{
		{name: "uses title", title: "페코라 방송", streamID: "vid1", wantLen: 16},
		{name: "falls back to stream id", title: "", streamID: "vid2", wantLen: 16},
		{name: "falls back to untitled", title: "", streamID: "", wantLen: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := buildTitleFingerprint(tt.title, tt.streamID)
			if len(got) != tt.wantLen {
				t.Fatalf("buildTitleFingerprint() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}

	if got1, got2 := buildTitleFingerprint("같은 제목", "a"), buildTitleFingerprint("같은 제목", "b"); got1 != got2 {
		t.Fatalf("expected same fingerprint for same normalized title, got %q and %q", got1, got2)
	}

	if got1, got2 := buildTitleFingerprint("", "stream-a"), buildTitleFingerprint("", "stream-b"); got1 == got2 {
		t.Fatalf("expected different fingerprints for different stream id fallback, got same %q", got1)
	}
}

func TestBuildTitleFingerprint_FullWidthEquivalence(t *testing.T) {
	t.Parallel()

	fpA := buildTitleFingerprint("クリアする!そして", "s1")
	fpB := buildTitleFingerprint("クリアする！そして", "s1")

	if fpA != fpB {
		t.Errorf("alarm_cache fingerprints differ for half/full-width: %q != %q", fpA, fpB)
	}
}

func TestResolveStreamChannelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stream    *domain.Stream
		fallback  string
		wantValue string
	}{
		{
			name:      "nil stream",
			stream:    nil,
			fallback:  "default",
			wantValue: "default",
		},
		{
			name:      "stream channel id has priority",
			stream:    &domain.Stream{ChannelID: "stream-id", Channel: &domain.Channel{ID: "channel-id"}},
			fallback:  "default",
			wantValue: "stream-id",
		},
		{
			name:      "falls back to stream channel object",
			stream:    &domain.Stream{Channel: &domain.Channel{ID: "channel-id"}},
			fallback:  "default",
			wantValue: "channel-id",
		},
		{
			name:      "falls back to default",
			stream:    &domain.Stream{},
			fallback:  "default",
			wantValue: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveStreamChannelID(tt.stream, tt.fallback); got != tt.wantValue {
				t.Fatalf("resolveStreamChannelID() = %q, want %q", got, tt.wantValue)
			}
		})
	}
}

func TestBuildUpcomingEventKey(t *testing.T) {
	t.Parallel()

	as := &AlarmService{}
	as.cacheState = alarmcache.NewState(nil, nil, nil)
	start := time.Date(2026, time.March, 2, 10, 30, 59, 0, time.UTC)
	key := as.buildUpcomingEventKey("room1", "channel1", "stream1", "Title", start)

	parts := strings.Split(key, ":")
	if len(parts) < 7 {
		t.Fatalf("unexpected key format: %q", key)
	}

	if !strings.HasPrefix(key, UpcomingEventKeyPrefix+"room1:channel1:") {
		t.Fatalf("unexpected prefix: %q", key)
	}

	wantUnix := normalizeScheduledMinute(start).Unix()
	if got := parts[len(parts)-2]; got != fmt.Sprintf("%d", wantUnix) {
		t.Fatalf("scheduled minute unix mismatch: got=%s want=%d", got, wantUnix)
	}

	if gotFingerprint := parts[len(parts)-1]; gotFingerprint != buildTitleFingerprint("Title", "stream1") {
		t.Fatalf("unexpected fingerprint: got=%s want=%s", gotFingerprint, buildTitleFingerprint("Title", "stream1"))
	}
}

func TestMarkUpcomingEventNotifiedAndWasRecently(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()
	start := time.Now().UTC().Add(30 * time.Minute)

	stream := &domain.Stream{
		ID:             "stream1",
		Title:          "테스트 방송",
		StartScheduled: &start,
	}

	if err := as.MarkUpcomingEventNotified(ctx, "room1", "channel1", stream); err != nil {
		t.Fatalf("MarkUpcomingEventNotified() error = %v", err)
	}

	if !as.WasUpcomingEventNotifiedRecently(ctx, "room1", "channel1", stream, time.Hour) {
		t.Fatal("expected upcoming event to be considered recently notified")
	}

	if as.WasUpcomingEventNotifiedRecently(ctx, "room1", "channel1", stream, 0) {
		t.Fatal("expected zero window to return false")
	}

	if as.WasUpcomingEventNotifiedRecently(ctx, "other-room", "channel1", stream, time.Hour) {
		t.Fatal("expected different room key to return false")
	}

	key := as.buildUpcomingEventKey("room1", "channel1", stream.ID, stream.Title, *stream.StartScheduled)
	if err := as.cache.Set(ctx, key, UpcomingEventNotifiedData{NotifiedAt: "invalid-time"}, constants.CacheTTL.NotificationSent); err != nil {
		t.Fatalf("cache.Set invalid payload failed: %v", err)
	}

	if as.WasUpcomingEventNotifiedRecently(ctx, "room1", "channel1", stream, time.Hour) {
		t.Fatal("expected invalid notified timestamp to return false")
	}

	if err := as.MarkUpcomingEventNotified(ctx, "room1", "channel1", nil); err != nil {
		t.Fatalf("MarkUpcomingEventNotified(nil) error = %v", err)
	}
}

type nextStreamInfoCase struct {
	name   string
	seed   map[string]any
	assert func(t *testing.T, got *domain.NextStreamInfo, err error)
}

func assertNoNextStreamInfo(t *testing.T, got *domain.NextStreamInfo, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Fatalf("expected nil info, got %#v", got)
	}
}

func assertUpcomingNextStreamInfo(t *testing.T, got *domain.NextStreamInfo, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got == nil {
		t.Fatal("expected info, got nil")
	}

	if got.Status != domain.NextStreamStatusUpcoming || got.VideoID != "v1" || got.Title != "테스트" {
		t.Fatalf("unexpected info: %#v", got)
	}

	if got.StartScheduled == nil || got.StartScheduled.Format(time.RFC3339) != "2026-03-02T00:00:00Z" {
		t.Fatalf("unexpected scheduled time: %#v", got.StartScheduled)
	}
}

func TestGetNextStreamInfo(t *testing.T) {
	t.Parallel()

	tests := []nextStreamInfoCase{
		{
			name:   "missing cache returns nil",
			seed:   nil,
			assert: assertNoNextStreamInfo,
		},
		{
			name:   "invalid status ignored",
			seed:   map[string]any{"status": "broken", "video_id": "v1", "title": "t1"},
			assert: assertNoNextStreamInfo,
		},
		{
			name:   "upcoming requires complete fields",
			seed:   map[string]any{"status": "upcoming", "video_id": "v1"},
			assert: assertNoNextStreamInfo,
		},
		{
			name: "upcoming complete fields",
			seed: map[string]any{
				"status":          "upcoming",
				"video_id":        "v1",
				"title":           "테스트",
				"start_scheduled": "2026-03-02T00:00:00Z",
			},
			assert: assertUpcomingNextStreamInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			as := newTestAlarmService(t)
			ctx := t.Context()
			channelID := "UC_test"
			key := NextStreamKeyPrefix + channelID

			if err := as.cache.Del(ctx, key); err != nil {
				t.Fatalf("cache delete failed: %v", err)
			}

			if len(tt.seed) > 0 {
				fields := make(map[string]any, len(tt.seed))
				maps.Copy(fields, tt.seed)

				if err := as.cache.HMSet(ctx, key, fields); err != nil {
					t.Fatalf("cache HMSet failed: %v", err)
				}
			}

			got, err := as.GetNextStreamInfo(ctx, channelID)
			tt.assert(t, got, err)
		})
	}
}

func TestGetNextStreamInfosBatch(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	require.NoError(t, as.cache.HMSet(ctx, NextStreamKeyPrefix+"UC_ok", map[string]any{
		"status":          "upcoming",
		"video_id":        "vid-ok",
		"title":           "배치 방송",
		"start_scheduled": "2026-03-06T00:00:00Z",
	}))
	require.NoError(t, as.cache.HMSet(ctx, NextStreamKeyPrefix+"UC_invalid", map[string]any{
		"status": "broken",
	}))
	require.NoError(t, as.cache.HSet(ctx, MemberNameKey, "UC_ok", "미코"))

	names, err := as.getMemberNamesBatch(ctx, []string{"UC_ok", "UC_missing"})
	require.NoError(t, err)
	assert.Equal(t, "미코", names["UC_ok"])
	assert.Empty(t, names["UC_missing"])

	infos, err := as.getNextStreamInfosBatch(ctx, []string{"UC_ok", "UC_invalid", "UC_missing"})
	require.NoError(t, err)
	require.NotNil(t, infos["UC_ok"])
	assert.Equal(t, "vid-ok", infos["UC_ok"].VideoID)
	assert.Nil(t, infos["UC_invalid"])
	assert.Nil(t, infos["UC_missing"])
}

func TestBuildAlarmListViews(t *testing.T) {
	t.Parallel()

	nextStream := &domain.NextStreamInfo{
		Status:  domain.NextStreamStatusUpcoming,
		Title:   "테스트 방송",
		VideoID: "vid1",
	}

	entries := buildAlarmListViews(
		[]*domain.Alarm{
			{
				ChannelID:  "ch-1",
				MemberName: "DB 이름",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
			},
			{
				ChannelID:  "ch-2",
				MemberName: "  ",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
			},
			{
				ChannelID:  "ch-3",
				MemberName: "",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
			},
		},
		map[string]string{
			"ch-1": "캐시 이름",
			"ch-2": " ",
		},
		map[string]*domain.NextStreamInfo{
			"ch-1": nextStream,
		},
	)

	require.Len(t, entries, 3)
	assert.Equal(t, "캐시 이름", entries[0].MemberName)
	assert.Equal(t, nextStream, entries[0].NextStream)
	assert.Equal(t, "ch-1", entries[0].ChannelID)

	assert.Equal(t, "ch-2", entries[1].MemberName)
	assert.Nil(t, entries[1].NextStream)

	assert.Equal(t, "ch-3", entries[2].MemberName)
	assert.Nil(t, entries[2].NextStream)
}

func TestNormalizeScheduledMinute(t *testing.T) {
	t.Parallel()

	input := time.Date(2026, time.March, 2, 5, 4, 59, 123, time.UTC)

	want := time.Date(2026, time.March, 2, 5, 4, 0, 0, time.UTC)
	if got := normalizeScheduledMinute(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeScheduledMinute() = %v, want %v", got, want)
	}
}
