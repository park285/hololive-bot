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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
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
			want:      sharedalarmkeys.ChannelSubscribersKeyPrefix + "UC_live",
		},
		{
			name:      "community uses dedicated prefix",
			channelID: "UC_community",
			alarmType: domain.AlarmTypeCommunity,
			want:      sharedalarmkeys.ChannelSubscribersCommunityPrefix + "UC_community",
		},
		{
			name:      "shorts uses dedicated prefix",
			channelID: "UC_shorts",
			alarmType: domain.AlarmTypeShorts,
			want:      sharedalarmkeys.ChannelSubscribersShortsPrefix + "UC_shorts",
		},
		{
			name:      "unknown type falls back to default",
			channelID: "UC_unknown",
			alarmType: domain.AlarmType("UNKNOWN"),
			want:      sharedalarmkeys.ChannelSubscribersKeyPrefix + "UC_unknown",
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

			got := sharedalarmkeys.BuildTitleFingerprint(tt.title, tt.streamID)
			if len(got) != tt.wantLen {
				t.Fatalf("sharedalarmkeys.BuildTitleFingerprint() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}

	if got1, got2 := sharedalarmkeys.BuildTitleFingerprint("같은 제목", "a"), sharedalarmkeys.BuildTitleFingerprint("같은 제목", "b"); got1 != got2 {
		t.Fatalf("expected same fingerprint for same normalized title, got %q and %q", got1, got2)
	}

	if got1, got2 := sharedalarmkeys.BuildTitleFingerprint("", "stream-a"), sharedalarmkeys.BuildTitleFingerprint("", "stream-b"); got1 == got2 {
		t.Fatalf("expected different fingerprints for different stream id fallback, got same %q", got1)
	}
}

func TestBuildTitleFingerprint_FullWidthEquivalence(t *testing.T) {
	t.Parallel()

	fpA := sharedalarmkeys.BuildTitleFingerprint("クリアする!そして", "s1")
	fpB := sharedalarmkeys.BuildTitleFingerprint("クリアする！そして", "s1")

	if fpA != fpB {
		t.Errorf("alarm_cache fingerprints differ for half/full-width: %q != %q", fpA, fpB)
	}
}

func TestGetMemberNamesBatch(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	require.NoError(t, as.cache.HSet(ctx, sharedalarmkeys.MemberNameKey, "UC_ok", "미코"))

	names, err := as.getMemberNamesBatch(ctx, []string{"UC_ok", "UC_missing"})
	require.NoError(t, err)
	assert.Equal(t, "미코", names["UC_ok"])
	assert.Empty(t, names["UC_missing"])
}

func TestBuildAlarmListViews(t *testing.T) {
	t.Parallel()

	entries := buildAlarmListViews(
		[]*domain.Alarm{
			{
				ChannelID:  testChannelID,
				MemberName: "DB 이름",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
			},
			{
				ChannelID:  testOtherChannelID,
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
			testChannelID:      "캐시 이름",
			testOtherChannelID: " ",
		},
	)

	require.Len(t, entries, 3)
	assert.Equal(t, "캐시 이름", entries[0].MemberName)
	assert.Equal(t, testChannelID, entries[0].ChannelID)

	assert.Equal(t, testOtherChannelID, entries[1].MemberName)

	assert.Equal(t, "ch-3", entries[2].MemberName)
}
