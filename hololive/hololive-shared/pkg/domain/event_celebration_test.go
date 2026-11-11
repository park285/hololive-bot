package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCelebrationDispatchPayload_Identity(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		MemberID:  testCelebrationMemberID,
		ChannelID: testChannelID,
		Date:      testCelebrationDate,
	}
	want := "birthday:member-101:2026-05-26"

	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityAnniversary(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindAnniversary,
		MemberID:  testCelebrationMemberID,
		ChannelID: "UC_ch",
		Date:      "2026-09-01",
	}
	want := "anniversary:member-101:2026-09-01"

	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayStream(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		MemberID:  testCelebrationMemberID,
		ChannelID: testChannelID,
		Date:      "2026-07-10",
		VideoID:   "vid123",
	}
	want := "birthday_stream:member-101:2026-07-10:vid123"

	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayIgnoresVideoID(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		MemberID:  testCelebrationMemberID,
		ChannelID: testChannelID,
		Date:      testCelebrationDate,
		VideoID:   "vid123",
	}
	want := "birthday:member-101:2026-05-26"

	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayStreamTrimsVideoID(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		MemberID:  testCelebrationMemberID,
		ChannelID: testChannelID,
		Date:      "2026-07-10",
		VideoID:   " vid123 ",
	}
	want := "birthday_stream:member-101:2026-07-10:vid123"

	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityBirthdayStreamEmptyVideoID(t *testing.T) {
	t.Parallel()

	p := &domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		MemberID:  testCelebrationMemberID,
		ChannelID: testChannelID,
		Date:      "2026-07-10",
	}
	want := "birthday_stream:member-101:2026-07-10"

	if got := p.Identity(); got != want {
		t.Fatalf("Identity() = %q, want %q", got, want)
	}
}

func TestCelebrationDispatchPayload_IdentityUsesStableMemberID(t *testing.T) {
	t.Parallel()

	first := &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, MemberID: 101, ChannelID: "UC_shared", Date: testCelebrationDate}
	second := &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, MemberID: 102, ChannelID: "UC_shared", Date: testCelebrationDate}

	if first.Identity() == second.Identity() {
		t.Fatalf("shared-channel member identities collide: %q", first.Identity())
	}
}

func TestCelebrationDispatchPayload_IdentityIgnoresChannelID(t *testing.T) {
	t.Parallel()

	first := &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, MemberID: testCelebrationMemberID, ChannelID: "UC_old", Date: testCelebrationDate}
	second := &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, MemberID: testCelebrationMemberID, ChannelID: "UC_new", Date: testCelebrationDate}

	if first.Identity() != second.Identity() {
		t.Fatalf("channel change altered member identity: %q != %q", first.Identity(), second.Identity())
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
