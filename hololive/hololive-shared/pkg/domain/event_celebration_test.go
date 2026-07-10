package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/shared-go/pkg/json"
)

func TestCelebrationDispatchPayload_Identity(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		ChannelID: "UC_test",
		Date:      "2026-05-26",
	}
	want := "birthday:UC_test:2026-05-26"
	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityAnniversary(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindAnniversary,
		ChannelID: "UC_ch",
		Date:      "2026-09-01",
	}
	want := "anniversary:UC_ch:2026-09-01"
	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayStream(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		ChannelID: "UC_test",
		Date:      "2026-07-10",
		VideoID:   "vid123",
	}
	want := "birthday_stream:UC_test:2026-07-10:vid123"
	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayIgnoresVideoID(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		ChannelID: "UC_test",
		Date:      "2026-05-26",
		VideoID:   "vid123",
	}
	want := "birthday:UC_test:2026-05-26"
	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayStreamTrimsVideoID(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		ChannelID: "UC_test",
		Date:      "2026-07-10",
		VideoID:   " vid123 ",
	}
	want := "birthday_stream:UC_test:2026-07-10:vid123"
	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayStreamEmptyVideoID(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		ChannelID: "UC_test",
		Date:      "2026-07-10",
	}
	want := "birthday_stream:UC_test:2026-07-10"
	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestAlarmTypeBirthday_IsValid(t *testing.T) {
	t.Parallel()

	if !domain.AlarmTypeBirthday.IsValid() {
		t.Fatal("AlarmTypeBirthday.IsValid() = false, want true")
	}
}

func TestAlarmTypeAnniversary_IsValid(t *testing.T) {
	t.Parallel()

	if !domain.AlarmTypeAnniversary.IsValid() {
		t.Fatal("AlarmTypeAnniversary.IsValid() = false, want true")
	}
}

func TestAlarmTypeBirthday_DisplayName(t *testing.T) {
	t.Parallel()

	if got := domain.AlarmTypeBirthday.DisplayName(); got != "생일" {
		t.Fatalf("DisplayName() = %q, want %q", got, "생일")
	}
}

func TestAlarmTypeAnniversary_DisplayName(t *testing.T) {
	t.Parallel()

	if got := domain.AlarmTypeAnniversary.DisplayName(); got != "주년" {
		t.Fatalf("DisplayName() = %q, want %q", got, "주년")
	}
}

func TestAlarmTypeBirthdayNotInAllAlarmTypes(t *testing.T) {
	t.Parallel()

	for _, at := range domain.AllAlarmTypes {
		if at == domain.AlarmTypeBirthday || at == domain.AlarmTypeAnniversary {
			t.Fatalf("AllAlarmTypes should not contain broadcast-only type %q", at)
		}
	}
}

func TestAlarmQueueEnvelope_JSONRoundtripCelebrationSource(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "UC_test", Name: "Test Member"},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "Test Member",
			ChannelID:  "UC_test",
			Photo:      "https://example.com/photo.jpg",
			Ordinal:    2,
			Date:       "2026-05-26",
		},
		ClaimKeys:  []string{},
		EnqueuedAt: "2026-05-26T00:00:00Z",
		Version:    1,
	}

	data, err := json.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw Unmarshal: %v", err)
	}
	if raw["source_kind"] != string(domain.AlarmDispatchSourceKindCelebration) {
		t.Fatalf("source_kind = %v, want %q", raw["source_kind"], domain.AlarmDispatchSourceKindCelebration)
	}
	if _, ok := raw["celebration"]; !ok {
		t.Fatal("celebration field missing from JSON")
	}

	var decoded domain.AlarmQueueEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.SourceKind != domain.AlarmDispatchSourceKindCelebration {
		t.Fatalf("SourceKind = %q, want %q", decoded.SourceKind, domain.AlarmDispatchSourceKindCelebration)
	}
	if decoded.Celebration == nil {
		t.Fatal("Celebration = nil")
	}
	if decoded.Celebration.Kind != domain.CelebrationKindBirthday {
		t.Fatalf("Kind = %q, want %q", decoded.Celebration.Kind, domain.CelebrationKindBirthday)
	}
	if decoded.Celebration.Date != "2026-05-26" {
		t.Fatalf("Date = %q, want %q", decoded.Celebration.Date, "2026-05-26")
	}
	if decoded.Celebration.MemberName != "Test Member" {
		t.Fatalf("MemberName = %q, want %q", decoded.Celebration.MemberName, "Test Member")
	}
	if decoded.Celebration.Ordinal != 2 {
		t.Fatalf("Ordinal = %d, want 2", decoded.Celebration.Ordinal)
	}
}

func TestAlarmQueueEnvelope_ValidateCanonicalDispatch_Celebration(t *testing.T) {
	t.Parallel()

	valid := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "Test",
			ChannelID:  "UC_test",
			Date:       "2026-05-26",
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
		ChannelID: "UC_test",
	}
	if err := noDate.ValidateCanonicalDispatch(); err == nil {
		t.Fatal("ValidateCanonicalDispatch() = nil, want error for empty date")
	}
}

func TestAlarmQueueEnvelope_ValidateCanonicalDispatch_BirthdayStream(t *testing.T) {
	t.Parallel()

	valid := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthdayStream,
			MemberName: "Test",
			ChannelID:  "UC_test",
			Date:       "2026-07-10",
			VideoID:    "vid123",
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
