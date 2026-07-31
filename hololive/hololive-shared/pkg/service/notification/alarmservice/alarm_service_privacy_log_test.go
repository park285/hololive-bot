package alarmservice

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func TestAlarmAddedLogNeverCarriesRoomTitleOrNickname(t *testing.T) {
	t.Parallel()

	const roomTitle = "상대방닉네임 님과의 대화"
	const nickname = "상대방닉네임"

	var buffer bytes.Buffer
	service := &AlarmService{logger: slog.New(slog.NewJSONHandler(&buffer, nil))}

	service.logAlarmAdded(&domain.AddAlarmRequest{
		RoomID:     roomTitle,
		UserID:     "1234567890",
		ChannelID:  "UC-channel",
		MemberName: "미코",
		RoomName:   roomTitle,
		UserName:   nickname,
	}, domain.AlarmTypes{"live"})

	line := buffer.String()
	for _, plaintext := range []string{roomTitle, nickname} {
		if strings.Contains(line, plaintext) {
			t.Fatalf("alarm add log leaked %q: %s", plaintext, line)
		}
	}
	for _, bannedKey := range []string{`"room_name"`, `"user_name"`} {
		if strings.Contains(line, bannedKey) {
			t.Fatalf("alarm add log still carries %s: %s", bannedKey, line)
		}
	}
	if !strings.Contains(line, privacylog.PseudonymPrefix) {
		t.Fatalf("alarm add log lost its room correlation token: %s", line)
	}
}

func TestAlarmAddedLogKeepsCanonicalRoomIdentifiersReadable(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	service := &AlarmService{logger: slog.New(slog.NewJSONHandler(&buffer, nil))}

	service.logAlarmAdded(&domain.AddAlarmRequest{
		RoomID:    "18446744073709551615",
		UserID:    "1234567890",
		ChannelID: "UC-channel",
	}, domain.AlarmTypes{"live"})

	if !strings.Contains(buffer.String(), `"room_id":"18446744073709551615"`) {
		t.Fatalf("canonical room id must stay readable: %s", buffer.String())
	}
}
