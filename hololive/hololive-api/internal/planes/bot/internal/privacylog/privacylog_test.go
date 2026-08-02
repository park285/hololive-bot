package privacylog

import (
	"strings"
	"testing"
)

func TestRoomIDAttrKeepsCanonicalIdentifiers(t *testing.T) {
	t.Parallel()

	for _, room := range []string{"0", "123456789", " 18446744073709551615 "} {
		attr := RoomIDAttr(room)
		if attr.Key != KeyRoomID {
			t.Fatalf("key = %q, want %q", attr.Key, KeyRoomID)
		}
		if got, want := attr.Value.String(), strings.TrimSpace(room); got != want {
			t.Fatalf("RoomIDAttr(%q) = %q, want %q", room, got, want)
		}
	}
}

func TestRoomIDAttrPseudonymizesNonCanonicalIdentifiers(t *testing.T) {
	t.Parallel()

	title := "룸제목 - 상대방 닉네임"
	got := RoomIDAttr(title).Value.String()

	if strings.Contains(got, title) {
		t.Fatalf("room_id = %q, want the room title to be absent", got)
	}
	if !strings.HasPrefix(got, PseudonymPrefix) {
		t.Fatalf("room_id = %q, want the %q prefix", got, PseudonymPrefix)
	}
	if got != RoomIDAttr(title).Value.String() {
		t.Fatal("pseudonym must be stable for the same input")
	}
	if got == RoomIDAttr(title+"2").Value.String() {
		t.Fatal("distinct rooms must not share a pseudonym")
	}
}

func TestChatIDAttrSharesTheRoomIDTreatment(t *testing.T) {
	t.Parallel()

	title := "룸제목"
	attr := ChatIDAttr(title)

	if attr.Key != KeyChatID {
		t.Fatalf("key = %q, want %q", attr.Key, KeyChatID)
	}
	if attr.Value.String() != RoomIDAttr(title).Value.String() {
		t.Fatalf("chat_id token = %q, want the room_id token %q", attr.Value.String(), RoomIDAttr(title).Value.String())
	}
}

func TestBlankIdentifiersBecomeUnknown(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "   "} {
		if got := RoomIDAttr(value).Value.String(); got != UnknownToken {
			t.Fatalf("RoomIDAttr(%q) = %q, want %q", value, got, UnknownToken)
		}
		if got := Pseudonym(value); got != UnknownToken {
			t.Fatalf("Pseudonym(%q) = %q, want %q", value, got, UnknownToken)
		}
	}
}

func TestPseudonymNeverEchoesItsInput(t *testing.T) {
	t.Parallel()

	// 숫자 ID는 16자 hex digest보다 길게 잡는다 — "123" 같은 짧은 숫자열은 키가 프로세스마다
	// 랜덤이라 digest에 우연히 포함될 수 있어(≈0.3%/run) 부분 문자열 단언이 flaky해진다.
	for _, value := range []string{"검색어", "미코", "1234567890123456789", "user@example.com"} {
		got := Pseudonym(value)
		if strings.Contains(got, value) {
			t.Fatalf("Pseudonym(%q) = %q, want the input to be absent", value, got)
		}
		if got != Pseudonym(value) {
			t.Fatalf("Pseudonym(%q) is not deterministic", value)
		}
	}
}

func TestIsCanonicalRoomID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", true},
		{"18446744073709551615", true},
		{"12a", false},
		{" 12", false},
		{"12\n", false},
		{"１２３", false},
		{"룸제목", false},
	}

	for _, tc := range cases {
		if got := IsCanonicalRoomID(tc.value); got != tc.want {
			t.Fatalf("IsCanonicalRoomID(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
