package dispatchoutbox

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type authorityKeyCase struct {
	name      string
	input     DedupeInput
	wantEvent string
}

func TestBuildEventKeyAuthorityHeadGoldens(t *testing.T) {
	startScheduled := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tests := append(youtubeAuthorityKeyCases(), nonYouTubeAuthorityKeyCases(startScheduled)...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAuthorityKeyPair(t, &tt.input, tt.wantEvent)
		})
	}
}

func youtubeAuthorityKeyCases() []authorityKeyCase {
	return append(youtubeCanonicalAuthorityKeyCases(), youtubeFallbackAuthorityKeyCases()...)
}

func youtubeCanonicalAuthorityKeyCases() []authorityKeyCase {
	canonical := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return []authorityKeyCase{
		{
			name:      "canonical youtube identity",
			input:     youtubeAuthorityInput(canonical),
			wantEvent: "youtube-outbox:NEW_VIDEO:" + canonical,
		},
		{
			name:      "canonical youtube identity with whitespace",
			input:     youtubeAuthorityInput(" \t" + canonical + "\n "),
			wantEvent: "youtube-outbox:NEW_VIDEO:" + canonical,
		},
	}
}

func youtubeFallbackAuthorityKeyCases() []authorityKeyCase {
	return []authorityKeyCase{
		fallbackAuthorityKeyCase("legacy youtube identity", "post-a,post-b", "141aa311b7bd2dc73e43d1486ae02876e9c714ffaedbf157ee9ad00578eb9ae8"),
		fallbackAuthorityKeyCase("uppercase youtube prefix", "SHA256:"+strings.Repeat("a", 64), "05c25fcf33981a4c3d965a84ae356d1b094cc20fffd5e80b1f1bbaf68b1b3299"),
		fallbackAuthorityKeyCase("uppercase youtube hex", "sha256:"+strings.Repeat("A", 64), "f3d38b0e0b53b4c2312ba4a21b12d57a08c67beb17c84c02b1d3e9d97a8b93c2"),
		fallbackAuthorityKeyCase("malformed youtube suffix", "sha256:"+strings.Repeat("a", 63)+"g", "863f15abedf3b72109214de7dde8a851beba2aace53804c0435b9afc03c52d2f"),
		fallbackAuthorityKeyCase("short youtube hash", "sha256:abc", "67e9bc3cfd2163c2978358dfe00d2f912cd4ee0c99f077c3583b39b48aebb124"),
		fallbackAuthorityKeyCase("whitespace only youtube identity", " \t\n", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
		fallbackAuthorityKeyCase("large legacy youtube identity", strings.Repeat("legacy,", 1000), "7c317fe45e7dd1262f3061ae2d663a19b6840e725e9ef1c444d95929af53b755"),
	}
}

func fallbackAuthorityKeyCase(name, identity, digest string) authorityKeyCase {
	return authorityKeyCase{
		name:      name,
		input:     youtubeAuthorityInput(identity),
		wantEvent: "youtube-outbox:NEW_VIDEO:sha256:" + digest,
	}
}

func nonYouTubeAuthorityKeyCases(startScheduled time.Time) []authorityKeyCase {
	return append(overLengthAuthorityKeyCases(), fixedFormatAuthorityKeyCases(startScheduled)...)
}

func overLengthAuthorityKeyCases() []authorityKeyCase {
	return []authorityKeyCase{{
		name: "over length live key",
		input: DedupeInput{
			RoomID:       "room-1",
			ChannelID:    strings.Repeat("c", 600),
			AlarmType:    domain.AlarmTypeLive,
			StreamID:     strings.Repeat("s", 600),
			Category:     "claim:event",
			MinutesUntil: 10,
		},
		wantEvent: "event_sha:b9d7a14ee0e3277ce8304b4e84a77440cde6d6dc5bbb97e8e4d9975677c1f5d8",
	}}
}

func fixedFormatAuthorityKeyCases(startScheduled time.Time) []authorityKeyCase {
	return []authorityKeyCase{
		{
			name: "live key",
			input: DedupeInput{
				RoomID:         "room-1",
				ChannelID:      "channel-1",
				AlarmType:      domain.AlarmTypeLive,
				StreamID:       "stream-1",
				StartScheduled: startScheduled,
				Category:       "live",
			},
			wantEvent: "live:channel-1:stream-1:1781265600:live:LIVE",
		},
		{
			name: "schedule key",
			input: DedupeInput{
				RoomID:                      "room-1",
				ChannelID:                   "channel-1",
				AlarmType:                   domain.AlarmTypeLive,
				StreamID:                    "stream-1",
				StartScheduled:              startScheduled,
				ScheduleChangePreviousStart: "2026-06-12T11:00:00Z",
				Category:                    "live",
			},
			wantEvent: "schedule:channel-1:stream-1:2026-06-12T11:00:00Z:1781265600:live:LIVE",
		},
		{
			name: "celebration key",
			input: DedupeInput{
				RoomID:         "room-1",
				SourceKind:     domain.AlarmDispatchSourceKindCelebration,
				SourceIdentity: "birthday:UC_test:2026-05-26",
			},
			wantEvent: "celebration:birthday:UC_test:2026-05-26",
		},
	}
}

func youtubeAuthorityInput(identity string) DedupeInput {
	return DedupeInput{
		RoomID:           "room-1",
		SourceKind:       domain.AlarmDispatchSourceKindYouTubeOutbox,
		SourceIdentity:   identity,
		SourceOutboxKind: domain.OutboxKindNewVideo,
	}
}

func assertAuthorityKeyPair(t *testing.T, input *DedupeInput, wantEvent string) {
	t.Helper()
	if got := BuildEventKey(input); got != wantEvent {
		t.Fatalf("BuildEventKey() = %q, want %q", got, wantEvent)
	}
	wantDedupe := "v2:room:" + input.RoomID + ":event:" + wantEvent
	if got := BuildDedupeKey(input); got != wantDedupe {
		t.Fatalf("BuildDedupeKey() = %q, want %q", got, wantDedupe)
	}
}

func TestEnvelopePreparedYouTubeIdentityMatchesUntrustedFallback(t *testing.T) {
	envelope := authorityYouTubeEnvelope()
	prepared := EnvelopeDedupeInput(&envelope)
	untrusted := prepared
	untrusted.preparedYouTubeIdentity = ""

	if got, want := BuildEventKey(&prepared), BuildEventKey(&untrusted); got != want {
		t.Fatalf("prepared event key = %q, untrusted event key = %q", got, want)
	}
	if got, want := BuildDedupeKey(&prepared), BuildDedupeKey(&untrusted); got != want {
		t.Fatalf("prepared dedupe key = %q, untrusted dedupe key = %q", got, want)
	}
}

func TestEnvelopePreparedYouTubeIdentityMutationFallsBack(t *testing.T) {
	tests := []struct {
		name         string
		identity     string
		wantIdentity string
	}{
		{
			name:         "uppercase prefix",
			identity:     "SHA256:" + strings.Repeat("a", 64),
			wantIdentity: "sha256:05c25fcf33981a4c3d965a84ae356d1b094cc20fffd5e80b1f1bbaf68b1b3299",
		},
		{
			name:         "malformed suffix",
			identity:     "sha256:" + strings.Repeat("a", 63) + "g",
			wantIdentity: "sha256:863f15abedf3b72109214de7dde8a851beba2aace53804c0435b9afc03c52d2f",
		},
		{
			name:         "empty after trim",
			identity:     " \t\n",
			wantIdentity: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope := authorityYouTubeEnvelope()
			prepared := EnvelopeDedupeInput(&envelope)
			prepared.SourceIdentity = tt.identity
			wantEvent := "youtube-outbox:COMMUNITY_POST:" + tt.wantIdentity
			assertAuthorityKeyPair(t, &prepared, wantEvent)
		})
	}
}

func TestBuildLedgerRowsYouTubeOutboxPersistsLiteralKeys(t *testing.T) {
	envelope := authorityYouTubeEnvelope()
	event, delivery, err := buildLedgerRows(&envelope, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows() error = %v", err)
	}

	wantEvent := "youtube-outbox:COMMUNITY_POST:sha256:c7c82486b9edf207d201c85f712ac0eebe66f126ced5e6ed3e5abf5eefab8a92"
	wantDedupe := "v2:room:room-1:event:youtube-outbox:COMMUNITY_POST:sha256:c7c82486b9edf207d201c85f712ac0eebe66f126ced5e6ed3e5abf5eefab8a92"
	if event.EventKey != wantEvent {
		t.Fatalf("event key = %q, want %q", event.EventKey, wantEvent)
	}
	if delivery.EventKey != wantEvent {
		t.Fatalf("delivery event key = %q, want %q", delivery.EventKey, wantEvent)
	}
	if delivery.DedupeKey != wantDedupe {
		t.Fatalf("delivery dedupe key = %q, want %q", delivery.DedupeKey, wantDedupe)
	}
}

func authorityYouTubeEnvelope() domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeCommunity,
			RoomID:    "room-1",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs:         []int64{10, 11},
			Kind:              domain.OutboxKindCommunityPost,
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_test",
			RenderTemplateKey: domain.TemplateKeyOutboxCommunityGroup,
			Items: []domain.YouTubeOutboxItem{
				{OutboxID: 11, ContentID: "post-b", Payload: `{"post_id":"post-b","content_text":"b"}`},
				{OutboxID: 10, ContentID: "post-a", Payload: `{"post_id":"post-a","content_text":"a"}`},
			},
		},
		ClaimKeys: []string{
			"youtube-notification:COMMUNITY_POST:post-a:room-1",
			"youtube-notification:COMMUNITY_POST:post-b:room-1",
		},
		Version: 1,
	}
}
