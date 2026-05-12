package keys

import (
	"strings"
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestIsRoomAlarmKeySeparatesRoomKeysFromReservedNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "room", key: "alarm:room-1", want: true},
		{name: "registry", key: AlarmRegistryKey, want: false},
		{name: "dispatch queue", key: contractsalarm.DispatchQueueKey, want: false},
		{name: "channel subscriber", key: ChannelSubscribersKeyPrefix + "UC_TEST", want: false},
		{name: "empty suffix", key: AlarmKeyPrefix, want: false},
		{name: "other namespace", key: "notified:stream-1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsRoomAlarmKey(tt.key); got != tt.want {
				t.Fatalf("IsRoomAlarmKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestBuildChannelSubscriberKey(t *testing.T) {
	t.Parallel()

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
			name:      "unknown falls back to default prefix",
			channelID: "UC_unknown",
			alarmType: domain.AlarmType("UNKNOWN"),
			want:      ChannelSubscribersKeyPrefix + "UC_unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := BuildChannelSubscriberKey(tt.channelID, tt.alarmType); got != tt.want {
				t.Fatalf("BuildChannelSubscriberKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildChannelContentAlarmTargetKeys(t *testing.T) {
	t.Parallel()

	targets := BuildChannelContentAlarmTargetKeys("UCtarget")

	if targets.ChannelID != "UCtarget" {
		t.Fatalf("ChannelID = %q, want %q", targets.ChannelID, "UCtarget")
	}
	if targets.CommunitySubscribersKey != ChannelSubscribersCommunityPrefix+"UCtarget" {
		t.Fatalf("CommunitySubscribersKey = %q", targets.CommunitySubscribersKey)
	}
	if targets.ShortsSubscribersKey != ChannelSubscribersShortsPrefix+"UCtarget" {
		t.Fatalf("ShortsSubscribersKey = %q", targets.ShortsSubscribersKey)
	}
	if got := targets.KeyFor(domain.AlarmTypeCommunity); got != targets.CommunitySubscribersKey {
		t.Fatalf("KeyFor(COMMUNITY) = %q, want %q", got, targets.CommunitySubscribersKey)
	}
	if got := targets.KeyFor(domain.AlarmTypeShorts); got != targets.ShortsSubscribersKey {
		t.Fatalf("KeyFor(SHORTS) = %q, want %q", got, targets.ShortsSubscribersKey)
	}
	if got := targets.KeyFor(domain.AlarmTypeLive); got != "" {
		t.Fatalf("KeyFor(LIVE) = %q, want empty", got)
	}
}

func TestValidateChannelContentAlarmTargetDefinitions(t *testing.T) {
	t.Parallel()

	t.Run("valid definitions", func(t *testing.T) {
		t.Parallel()

		err := ValidateChannelContentAlarmTargetDefinitions([]ChannelContentAlarmTargetDefinition{
			{OwnerLabel: "Pekora", ChannelID: "UCpekora"},
			{OwnerLabel: "Miko", ChannelID: "UCmiko"},
		})
		if err != nil {
			t.Fatalf("ValidateChannelContentAlarmTargetDefinitions() error = %v", err)
		}
	})

	t.Run("missing channel id is reported", func(t *testing.T) {
		t.Parallel()

		err := ValidateChannelContentAlarmTargetDefinitions([]ChannelContentAlarmTargetDefinition{
			{OwnerLabel: "Pekora", ChannelID: ""},
		})
		if err == nil {
			t.Fatal("expected error for missing channel id")
		}
		if !strings.Contains(err.Error(), "missing operating channel targets") {
			t.Fatalf("error = %q, want missing operating channel targets", err)
		}
		if !strings.Contains(err.Error(), "Pekora") {
			t.Fatalf("error = %q, want owner label", err)
		}
	})

	t.Run("duplicate channel target keys are reported", func(t *testing.T) {
		t.Parallel()

		err := ValidateChannelContentAlarmTargetDefinitions([]ChannelContentAlarmTargetDefinition{
			{OwnerLabel: "Pekora", ChannelID: "UCdup"},
			{OwnerLabel: "Miko", ChannelID: "UCdup"},
		})
		if err == nil {
			t.Fatal("expected error for duplicate channel targets")
		}
		if !strings.Contains(err.Error(), "duplicate deployment targets") {
			t.Fatalf("error = %q, want duplicate deployment targets", err)
		}
		if !strings.Contains(err.Error(), "Miko:community duplicates Pekora:community") {
			t.Fatalf("error = %q, want duplicate owner details", err)
		}
		if !strings.Contains(err.Error(), "Miko:shorts duplicates Pekora:shorts") {
			t.Fatalf("error = %q, want duplicate shorts details", err)
		}
	})
}

func TestBuildTitleFingerprint_FullWidthPunctuationEquivalence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		titleA string
		titleB string
	}{
		{
			name:   "half-width vs full-width exclamation",
			titleA: "クリアする!そして",
			titleB: "クリアする！そして",
		},
		{
			name:   "half-width vs full-width question",
			titleA: "本当に?マジで",
			titleB: "本当に？マジで",
		},
		{
			name:   "mixed full-width punctuation",
			titleA: "テスト!方送(開始)",
			titleB: "テスト！方送（開始）",
		},
		{
			name:   "brackets lenticular",
			titleA: "【ホロライブ】配信",
			titleB: "ホロライブ配信",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fpA := BuildTitleFingerprint(tt.titleA, "stream-1")
			fpB := BuildTitleFingerprint(tt.titleB, "stream-1")
			if fpA != fpB {
				t.Errorf("fingerprints differ: %q != %q (titleA=%q, titleB=%q)", fpA, fpB, tt.titleA, tt.titleB)
			}
		})
	}
}

func TestBuildTitleFingerprint_DifferentTitles(t *testing.T) {
	t.Parallel()

	fpA := BuildTitleFingerprint("Minecraft配信", "s1")
	fpB := BuildTitleFingerprint("Pokemon配信", "s1")
	if fpA == fpB {
		t.Error("different titles should produce different fingerprints")
	}
}
