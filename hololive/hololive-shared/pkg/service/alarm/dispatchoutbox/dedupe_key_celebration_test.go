package dispatchoutbox

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildEventKeyCelebrationUsesIdentity(t *testing.T) {
	input := DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: "birthday:UC_test:2026-05-26",
		ChannelID:      "UC_test",
		AlarmType:      domain.AlarmTypeBirthday,
		Category:       string(domain.AlarmDispatchSourceKindCelebration),
	}

	got := BuildEventKey(&input)
	want := "celebration:birthday:UC_test:2026-05-26"
	if got != want {
		t.Fatalf("BuildEventKey() = %q, want %q", got, want)
	}
}

func TestBuildEventKeyCelebrationIncludesDate(t *testing.T) {
	day1 := DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: "birthday:UC_test:2026-05-26",
	}
	day2 := DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: "birthday:UC_test:2027-05-26",
	}

	key1 := BuildEventKey(&day1)
	key2 := BuildEventKey(&day2)
	if key1 == key2 {
		t.Fatalf("same event key across years: %q", key1)
	}
}

func TestEnvelopeDedupeInputCelebration(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "UC_test"},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:      domain.CelebrationKindBirthday,
			ChannelID: "UC_test",
			Date:      "2026-05-26",
		},
	}

	input := EnvelopeDedupeInput(&envelope)

	if input.SourceKind != domain.AlarmDispatchSourceKindCelebration {
		t.Fatalf("SourceKind = %q, want %q", input.SourceKind, domain.AlarmDispatchSourceKindCelebration)
	}
	if input.SourceIdentity != "birthday:UC_test:2026-05-26" {
		t.Fatalf("SourceIdentity = %q, want %q", input.SourceIdentity, "birthday:UC_test:2026-05-26")
	}
	if input.ChannelID != "UC_test" {
		t.Fatalf("ChannelID = %q, want UC_test", input.ChannelID)
	}
	if input.AlarmType != domain.AlarmTypeBirthday {
		t.Fatalf("AlarmType = %q, want %q", input.AlarmType, domain.AlarmTypeBirthday)
	}
}

func TestBuildLedgerRowsCelebrationEventKey(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "UC_test"},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "Test",
			ChannelID:  "UC_test",
			Date:       "2026-05-26",
		},
		Version: 1,
	}

	event, delivery, err := buildLedgerRows(&envelope, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows() error = %v", err)
	}

	wantEventKey := "celebration:birthday:UC_test:2026-05-26"
	if event.EventKey != wantEventKey {
		t.Fatalf("EventKey = %q, want %q", event.EventKey, wantEventKey)
	}
	if event.AlarmType != domain.AlarmTypeBirthday {
		t.Fatalf("AlarmType = %q, want BIRTHDAY", event.AlarmType)
	}
	if event.ChannelID != "UC_test" {
		t.Fatalf("ChannelID = %q, want UC_test", event.ChannelID)
	}
	if !strings.Contains(delivery.DedupeKey, "room-1") {
		t.Fatalf("DedupeKey = %q, want room-specific key", delivery.DedupeKey)
	}

	payload := string(event.Payload)
	if !strings.Contains(payload, `"celebration"`) {
		t.Fatal("event payload missing celebration field")
	}
	if err := validateEventPayloadRoomAgnostic(event.Payload); err != nil {
		t.Fatalf("validateEventPayloadRoomAgnostic() = %v", err)
	}
}

func TestBuildLedgerRowsCelebrationSameEventKeyAcrossRooms(t *testing.T) {
	makeEnvelope := func(roomID string) domain.AlarmQueueEnvelope {
		return domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeBirthday,
				RoomID:    roomID,
				Channel:   &domain.Channel{ID: "UC_test"},
			},
			SourceKind: domain.AlarmDispatchSourceKindCelebration,
			Celebration: &domain.CelebrationDispatchPayload{
				Kind:       domain.CelebrationKindBirthday,
				MemberName: "Test",
				ChannelID:  "UC_test",
				Date:       "2026-05-26",
			},
			Version: 1,
		}
	}

	room1 := makeEnvelope("room-1")
	event1, delivery1, err := buildLedgerRows(&room1, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows room1: %v", err)
	}
	room2 := makeEnvelope("room-2")
	event2, delivery2, err := buildLedgerRows(&room2, StatusPending)
	if err != nil {
		t.Fatalf("buildLedgerRows room2: %v", err)
	}

	if event1.EventKey != event2.EventKey {
		t.Fatalf("event keys differ by room: %q != %q", event1.EventKey, event2.EventKey)
	}
	if delivery1.DedupeKey == delivery2.DedupeKey {
		t.Fatalf("delivery dedupe keys should differ by room, got %q", delivery1.DedupeKey)
	}
}
