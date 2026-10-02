package privacylog

import (
	"testing"

	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

func TestRoomAttrFallsBackToTheRoomNameToken(t *testing.T) {
	t.Parallel()

	const roomName = "상대방닉네임 님과의 대화"

	fromIngress := RoomAttr("", roomName)
	if fromIngress.Value.String() == sharedprivacylog.UnknownToken {
		t.Fatal("chat_id가 비면 방 제목 token으로 상관관계를 이어야 한다")
	}

	if fromIngress.Value.String() != sharedprivacylog.RoomIDAttr(roomName).Value.String() {
		t.Fatalf("ingress token = %q, want the room-name token %q",
			fromIngress.Value.String(), sharedprivacylog.RoomIDAttr(roomName).Value.String())
	}

	if fromIngress.Value.String() == roomName {
		t.Fatal("방 제목이 평문으로 남으면 안 된다")
	}
}

func TestRoomAttrAndChatAttrShareOneToken(t *testing.T) {
	t.Parallel()

	const roomName = "상대방닉네임 님과의 대화"

	roomAttr := RoomAttr("", roomName)
	chatAttr := ChatAttr("", roomName)

	if roomAttr.Key != sharedprivacylog.KeyRoomID || chatAttr.Key != sharedprivacylog.KeyChatID {
		t.Fatalf("keys = %q/%q, want %q/%q", roomAttr.Key, chatAttr.Key, sharedprivacylog.KeyRoomID, sharedprivacylog.KeyChatID)
	}

	if roomAttr.Value.String() != chatAttr.Value.String() {
		t.Fatalf("room token %q and chat token %q diverged",
			roomAttr.Value.String(), chatAttr.Value.String())
	}
}

func TestRoomAttrPrefersTheChatIdentifier(t *testing.T) {
	t.Parallel()

	if got, want := RoomAttr("123456789", "방 제목").Value.String(), "123456789"; got != want {
		t.Fatalf("RoomAttr = %q, want %q", got, want)
	}

	if got := RoomAttr("   ", "방 제목").Value.String(); got != sharedprivacylog.RoomIDAttr("방 제목").Value.String() {
		t.Fatalf("공백 chat_id는 비어 있는 것으로 봐야 한다: %q", got)
	}
}

func TestBlankRoomAndNameStillCollapseToUnknown(t *testing.T) {
	t.Parallel()

	if got := RoomAttr("", "").Value.String(); got != sharedprivacylog.UnknownToken {
		t.Fatalf("RoomAttr(\"\", \"\") = %q, want %q", got, sharedprivacylog.UnknownToken)
	}
}
