package dispatchoutbox

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmQueueEnvelope_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	envelope := AlarmQueueEnvelope{
		DispatchOutboxID: 123,
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       "room1",
			MinutesUntil: 5,
			Users:        []string{"user1"},
		},
		ClaimKeys:  []string{"notified:claim:room1:vid:123:LIVE"},
		EnqueuedAt: "2026-02-25T13:00:00Z",
		Version:    1,
		Retry: &AlarmQueueRetryMetadata{
			Attempt:      2,
			RetryAfterMS: 1500,
			LastError:    "temporary upstream timeout",
		},
	}

	data, err := jsonv2.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	var decoded AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 실패: %v", err)
	}

	if decoded.Version != 1 {
		t.Errorf("Version = %d, want 1", decoded.Version)
	}

	if decoded.DispatchOutboxID != 123 {
		t.Errorf("DispatchOutboxID = %d, want 123", decoded.DispatchOutboxID)
	}

	if decoded.Notification.RoomID != "room1" {
		t.Errorf("RoomID = %q, want %q", decoded.Notification.RoomID, "room1")
	}

	if len(decoded.ClaimKeys) != 1 {
		t.Errorf("ClaimKeys len = %d, want 1", len(decoded.ClaimKeys))
	}

	if decoded.Retry == nil {
		t.Fatal("Retry = nil, want metadata")
	}

	if decoded.Retry.Attempt != 2 {
		t.Errorf("Retry.Attempt = %d, want 2", decoded.Retry.Attempt)
	}

	if decoded.Retry.RetryAfterMS != 1500 {
		t.Errorf("Retry.RetryAfterMS = %d, want 1500", decoded.Retry.RetryAfterMS)
	}

	if decoded.Retry.LastError != "temporary upstream timeout" {
		t.Errorf("Retry.LastError = %q, want %q", decoded.Retry.LastError, "temporary upstream timeout")
	}
}

func TestAlarmQueueEnvelope_WireFormatDecode(t *testing.T) {
	t.Parallel()

	// 저장 payload의 snake_case wire 형식이며 alarm_type이 없으면 decode가 거절한다.
	wireJSON := `{
		"notification": {
			"alarm_type": "LIVE",
			"room_id": "room42",
			"channel": null,
			"stream": null,
			"minutes_until": 3,
			"users": []
		},
		"claim_keys": ["notified:claim:room42:stream1:1740492000:LIVE", "notified:claim:event:room42:UC_ch:1740492000:abc123:LIVE"],
		"enqueued_at": "2026-02-25T13:00:00+00:00",
		"version": 1
	}`

	var env AlarmQueueEnvelope

	if err := jsonv2.Unmarshal([]byte(wireJSON), &env); err != nil {
		t.Fatalf("wire JSON 역직렬화 실패: %v", err)
	}

	if env.Notification.RoomID != "room42" {
		t.Errorf("RoomID = %q, want %q", env.Notification.RoomID, "room42")
	}

	if env.Notification.MinutesUntil != 3 {
		t.Errorf("MinutesUntil = %d, want 3", env.Notification.MinutesUntil)
	}

	if len(env.ClaimKeys) != 2 {
		t.Errorf("ClaimKeys len = %d, want 2", len(env.ClaimKeys))
	}

	if env.Version != 1 {
		t.Errorf("Version = %d, want 1", env.Version)
	}

	if env.EnqueuedAt != "2026-02-25T13:00:00+00:00" {
		t.Errorf("EnqueuedAt = %q, want %q", env.EnqueuedAt, "2026-02-25T13:00:00+00:00")
	}
}

func TestAlarmQueueEnvelope_OmitsScheduleChangeMessage(t *testing.T) {
	t.Parallel()

	envelope := AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:       "room99",
			MinutesUntil: 0,
			Users:        []string{},
		},
		ClaimKeys:  []string{},
		EnqueuedAt: "2026-02-25T14:00:00Z",
		Version:    1,
	}

	data, err := jsonv2.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	// schedule_change_message는 빈 문자열이면 직렬화에 포함되지 않아야 함
	var raw map[string]any

	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw Unmarshal 실패: %v", err)
	}

	notif, ok := raw["notification"].(map[string]any)
	if !ok {
		t.Fatal("notification 필드 없음")
	}

	if _, exists := notif["schedule_change_message"]; exists {
		t.Error("schedule_change_message는 빈 값일 때 직렬화에 포함되면 안 됨")
	}

	if _, exists := raw["retry"]; exists {
		t.Error("retry는 nil일 때 직렬화에 포함되면 안 됨")
	}

	if _, exists := raw["source_payload"]; exists {
		t.Error("source_payload는 기본 envelope 직렬화에 포함되면 안 됨")
	}
}

// 저장 payload의 notification.alarm_type 누락은 Live로 채우지 않고 decode 오류다.
func TestAlarmQueueEnvelope_UnmarshalRejectsMissingAlarmType(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]string{
		"missing": `{"dispatch_outbox_id":1,"notification":{"room_id":"room-1"},"version":1}`,
		"empty":   `{"dispatch_outbox_id":1,"notification":{"alarm_type":"","room_id":"room-1"},"version":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var envelope AlarmQueueEnvelope

			err := jsonv2.Unmarshal([]byte(payload), &envelope)
			if err == nil {
				t.Fatalf("Unmarshal() error = nil, alarm type = %q; want missing alarm_type rejected", envelope.Notification.AlarmType)
			}

			if !strings.Contains(err.Error(), "alarm_type") {
				t.Fatalf("Unmarshal() error = %v, want alarm_type in message", err)
			}
		})
	}
}

func TestAlarmQueueEnvelope_JSONRoundtripCelebrationSource(t *testing.T) {
	t.Parallel()

	envelope := AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    testRoomID,
			Channel:   &domain.Channel{ID: testYouTubeChannelID, Name: "Test Member"},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberID:   101,
			MemberName: "Test Member",
			ChannelID:  testYouTubeChannelID,
			Photo:      "https://example.com/photo.jpg",
			Ordinal:    2,
			Date:       fixtureCelebrationDate,
		},
		ClaimKeys:  []string{},
		EnqueuedAt: "2026-05-26T00:00:00Z",
		Version:    1,
	}

	data, err := jsonv2.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any

	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw Unmarshal: %v", err)
	}

	if raw["source_kind"] != string(domain.AlarmDispatchSourceKindCelebration) {
		t.Fatalf("source_kind = %v, want %q", raw["source_kind"], domain.AlarmDispatchSourceKindCelebration)
	}

	assertCelebrationMemberIDPresent(t, raw["celebration"])

	var decoded AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.SourceKind != domain.AlarmDispatchSourceKindCelebration {
		t.Fatalf("SourceKind = %q, want %q", decoded.SourceKind, domain.AlarmDispatchSourceKindCelebration)
	}

	if decoded.Celebration == nil {
		t.Fatal("Celebration = nil")
	}

	if *decoded.Celebration != *envelope.Celebration {
		t.Fatalf("Celebration = %+v, want %+v", *decoded.Celebration, *envelope.Celebration)
	}
}

func assertCelebrationMemberIDPresent(t *testing.T, value any) {
	t.Helper()

	celebration, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("celebration = %T, want object", value)
	}

	if _, ok := celebration["member_id"]; !ok {
		t.Fatal("member_id must be serialized")
	}
}

func TestAlarmQueueEnvelope_ValidateCanonicalDispatch_Celebration(t *testing.T) {
	t.Parallel()

	valid := AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    testRoomID,
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:      domain.CelebrationKindBirthday,
			MemberID:  101,
			ChannelID: testYouTubeChannelID,
			Date:      fixtureCelebrationDate,
		},
	}
	if err := valid.ValidateCanonicalDispatch(); err != nil {
		t.Fatalf("ValidateCanonicalDispatch() = %v, want nil", err)
	}

	noRoom := valid

	noRoom.Notification.RoomID = ""

	if err := noRoom.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for empty room_id")
	}

	wrongType := valid

	wrongType.Notification.AlarmType = domain.AlarmTypeLive

	if err := wrongType.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for non-celebration alarm type")
	}

	nilPayload := valid

	nilPayload.Celebration = nil

	if err := nilPayload.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for nil celebration payload")
	}

	noDate := valid

	noDate.Celebration = &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		MemberID:  101,
		ChannelID: testYouTubeChannelID,
	}

	if err := noDate.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for empty date")
	}

	noMember := valid
	noMemberPayload := *valid.Celebration

	noMemberPayload.MemberID = 0
	noMember.Celebration = &noMemberPayload

	if err := noMember.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for missing member id")
	}
}

func TestAlarmQueueEnvelope_ValidateCanonicalDispatch_BirthdayStream(t *testing.T) {
	t.Parallel()

	valid := AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    testRoomID,
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:      domain.CelebrationKindBirthdayStream,
			MemberID:  101,
			ChannelID: testYouTubeChannelID,
			Date:      "2026-07-10",
			VideoID:   "vid123",
		},
	}
	if err := valid.ValidateCanonicalDispatch(); err != nil {
		t.Fatalf("ValidateCanonicalDispatch() = %v, want nil", err)
	}

	noVideoID := valid
	payload := *valid.Celebration

	payload.VideoID = ""
	noVideoID.Celebration = &payload

	if err := noVideoID.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for empty birthday stream video id")
	}

	blankVideoID := valid
	blankPayload := *valid.Celebration

	blankPayload.VideoID = "   "
	blankVideoID.Celebration = &blankPayload

	if err := blankVideoID.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for whitespace birthday stream video id")
	}
}

func decodeAlarmQueueEnvelopeFixture(t *testing.T) AlarmQueueEnvelope {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "envelope_v1.json"))
	if err != nil {
		t.Fatalf("read fixture envelope_v1.json: %v", err)
	}

	var envelope AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	return envelope
}

func TestAlarmQueueEnvelopeV1FixtureFields(t *testing.T) {
	t.Parallel()

	envelope := decodeAlarmQueueEnvelopeFixture(t)

	if envelope.Version != contractsalarm.QueueEnvelopeVersionV1 {
		t.Fatalf("version = %d, want %d", envelope.Version, contractsalarm.QueueEnvelopeVersionV1)
	}

	if envelope.Notification.RoomID != "room-1" {
		t.Fatalf("room_id = %q, want room-1", envelope.Notification.RoomID)
	}

	if envelope.Retry == nil || envelope.Retry.Attempt != 2 {
		t.Fatalf("retry metadata = %+v, want attempt 2", envelope.Retry)
	}

	if envelope.DispatchOutboxID != 42 {
		t.Fatalf("dispatch_outbox_id = %d, want 42", envelope.DispatchOutboxID)
	}

	if envelope.SourceKind != domain.AlarmDispatchSourceKindXSpace {
		t.Fatalf("source_kind = %q, want %q", envelope.SourceKind, domain.AlarmDispatchSourceKindXSpace)
	}

	if envelope.XSpace == nil || envelope.XSpace.SpaceID != "1fixture" {
		t.Fatalf("x_space = %+v, want space_id 1fixture", envelope.XSpace)
	}

	if envelope.SourcePayload() != `{"version":1}` {
		t.Fatalf("source_payload = %q, want {\"version\":1}", envelope.SourcePayload())
	}

	if err := envelope.ValidateCanonicalDispatch(); err != nil {
		t.Fatalf("ValidateCanonicalDispatch: %v", err)
	}
}

func TestAlarmQueueEnvelopeV1FixtureRoundTrip(t *testing.T) {
	t.Parallel()

	envelope := decodeAlarmQueueEnvelopeFixture(t)

	encoded, err := jsonv2.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	var roundTrip AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}

	if roundTrip.Notification.RoomID != envelope.Notification.RoomID {
		t.Fatalf("roundTrip room_id = %q, want %q", roundTrip.Notification.RoomID, envelope.Notification.RoomID)
	}

	if roundTrip.DispatchOutboxID != envelope.DispatchOutboxID {
		t.Fatalf("roundTrip dispatch_outbox_id = %d, want %d", roundTrip.DispatchOutboxID, envelope.DispatchOutboxID)
	}

	if roundTrip.SourceKind != envelope.SourceKind {
		t.Fatalf("roundTrip source_kind = %q, want %q", roundTrip.SourceKind, envelope.SourceKind)
	}

	if roundTrip.XSpace == nil || *roundTrip.XSpace != *envelope.XSpace {
		t.Fatal("roundTrip x_space mismatch")
	}

	if roundTrip.SourcePayload() != envelope.SourcePayload() {
		t.Fatalf("roundTrip source_payload = %q, want %q", roundTrip.SourcePayload(), envelope.SourcePayload())
	}
}

func TestAlarmQueueRetryMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	envelope := decodeAlarmQueueEnvelopeFixture(t)

	encoded, err := jsonv2.Marshal(envelope.Retry)
	if err != nil {
		t.Fatalf("marshal retry metadata: %v", err)
	}

	var retry AlarmQueueRetryMetadata

	if err := jsonv2.Unmarshal(encoded, &retry); err != nil {
		t.Fatalf("unmarshal retry metadata: %v", err)
	}

	if retry.RetryAfterMS != 30000 {
		t.Fatalf("retry_after_ms = %d, want 30000", retry.RetryAfterMS)
	}

	if retry.LastError != "temporary upstream error" {
		t.Fatalf("last_error = %q, want temporary upstream error", retry.LastError)
	}

	if retry.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", retry.Attempt)
	}
}
