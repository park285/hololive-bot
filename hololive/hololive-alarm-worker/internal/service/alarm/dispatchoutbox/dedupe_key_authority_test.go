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
	startScheduled := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	tests := append(youtubeAuthorityKeyCases(), nonYouTubeAuthorityKeyCases(startScheduled)...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAuthorityKeyPair(t, &tt.input, tt.wantEvent)
		})
	}
}

func youtubeAuthorityKeyCases() []authorityKeyCase {
	canonical := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	return []authorityKeyCase{
		{
			name:      "canonical youtube identity",
			input:     youtubeAuthorityInput(canonical),
			wantEvent: "youtube-outbox:NEW_VIDEO:" + canonical,
		},
	}
}

func nonYouTubeAuthorityKeyCases(startScheduled time.Time) []authorityKeyCase {
	return append(overLengthAuthorityKeyCases(), fixedFormatAuthorityKeyCases(startScheduled)...)
}

func overLengthAuthorityKeyCases() []authorityKeyCase {
	return []authorityKeyCase{{
		name: "over length live key",
		input: DedupeInput{
			RoomID:       testRoomID,
			ChannelID:    strings.Repeat("c", 600),
			AlarmType:    domain.AlarmTypeLive,
			StreamID:     strings.Repeat("s", 600),
			Category:     testClaimEventCategory,
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
				RoomID:         testRoomID,
				ChannelID:      testChannelID,
				AlarmType:      domain.AlarmTypeLive,
				StreamID:       testStreamID,
				StartScheduled: startScheduled,
				Category:       "live",
			},
			wantEvent: "live:channel-1:stream-1:1781265600:live:LIVE",
		},
		{
			name: "schedule key",
			input: DedupeInput{
				RoomID:                      testRoomID,
				ChannelID:                   testChannelID,
				AlarmType:                   domain.AlarmTypeLive,
				StreamID:                    testStreamID,
				StartScheduled:              startScheduled,
				ScheduleChangePreviousStart: "2026-06-12T11:00:00Z",
				Category:                    "live",
			},
			wantEvent: "schedule:channel-1:stream-1:2026-06-12T11:00:00Z:1781265600:live:LIVE",
		},
		{
			name: "celebration key",
			input: DedupeInput{
				RoomID:         testRoomID,
				SourceKind:     domain.AlarmDispatchSourceKindCelebration,
				SourceIdentity: "birthday:UC_test:2026-05-26",
			},
			wantEvent: "celebration:birthday:UC_test:2026-05-26",
		},
	}
}

func youtubeAuthorityInput(identity string) DedupeInput {
	return DedupeInput{
		RoomID:           testRoomID,
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

// 저장 경로의 YouTube 식별자는 envelope payload의 정규 식별자 그대로 key가 된다.
func TestEnvelopeYouTubeIdentityUsesCanonicalPayloadIdentity(t *testing.T) {
	envelope := authorityYouTubeEnvelope()
	input := EnvelopeDedupeInput(&envelope)
	wantEvent := "youtube-outbox:COMMUNITY_POST:" + envelope.YouTubeOutbox.Identity()

	if got := BuildEventKey(&input); got != wantEvent {
		t.Fatalf("envelope event key = %q, want %q", got, wantEvent)
	}

	if got, want := BuildDedupeKeyFromEnvelope(&envelope), "v2:room:"+input.RoomID+":event:"+wantEvent; got != want {
		t.Fatalf("envelope dedupe key = %q, want %q", got, want)
	}
}

func TestBuildLedgerRowsYouTubeOutboxPersistsLiteralKeys(t *testing.T) {
	envelope := authorityYouTubeEnvelope()

	event, delivery, err := buildLedgerRows(&envelope)
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

	if got := BuildDedupeKeyFromEnvelope(&envelope); got != wantDedupe {
		t.Fatalf("envelope dedupe key = %q, want %q", got, wantDedupe)
	}
}

// 비정규 YouTube source identity는 해시로 감싸 받지 않고 저장 전에 거절한다(stack-audit 2026-09-26 T11
// holo-dispatch-raw-youtube-identity-hash). 다른 source kind의 식별자 형식은 이 검사 대상이 아니다.
func TestValidateSourceIdentityRejectsNonCanonicalYouTubeIdentity(t *testing.T) {
	canonical := "sha256:" + strings.Repeat("0123456789abcdef", 4)

	canonicalInput := youtubeAuthorityInput(canonical)
	if err := validateSourceIdentity(&canonicalInput); err != nil {
		t.Fatalf("canonical identity rejected: %v", err)
	}

	for name, identity := range map[string]string{
		"legacy raw":          "post-a,post-b",
		"empty":               "",
		"padded canonical":    " " + canonical,
		"uppercase prefix":    "SHA256:" + strings.Repeat("a", 64),
		"uppercase hex":       "sha256:" + strings.Repeat("A", 64),
		"malformed suffix":    "sha256:" + strings.Repeat("a", 63) + "g",
		"short hash":          "sha256:abc",
		"multibyte in digest": "sha256:" + strings.Repeat("a", 61) + "가",
	} {
		t.Run(name, func(t *testing.T) {
			input := youtubeAuthorityInput(identity)
			if err := validateSourceIdentity(&input); err == nil {
				t.Fatalf("validateSourceIdentity(%q) = nil, want error", identity)
			}
		})
	}

	celebration := &DedupeInput{SourceKind: domain.AlarmDispatchSourceKindCelebration, SourceIdentity: "birthday:42:2026-09-27"}
	if err := validateSourceIdentity(celebration); err != nil {
		t.Fatalf("non-youtube identity rejected: %v", err)
	}
}

func authorityYouTubeEnvelope() domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeCommunity,
			RoomID:    testRoomID,
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs: []int64{10, 11},
			Kind:      domain.OutboxKindCommunityPost,
			AlarmType: domain.AlarmTypeCommunity,
			ChannelID: testYouTubeChannelID,
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
