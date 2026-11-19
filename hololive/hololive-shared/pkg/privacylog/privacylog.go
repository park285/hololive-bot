package privacylog

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
)

const (
	KeyRoomID = "room_id"
	KeyChatID = "chat_id"

	UnknownToken    = "unknown"
	PseudonymPrefix = "anon:"

	pseudonymBytes = 8
)

var pseudonymKey = []byte(rand.Text())

func RoomIDAttr(room string) slog.Attr {
	return slog.String(KeyRoomID, IdentifierToken(room))
}

func ChatIDAttr(chatID string) slog.Attr {
	return slog.String(KeyChatID, IdentifierToken(chatID))
}

func IsCanonicalRoomID(value string) bool {
	if value == "" {
		return false
	}

	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}

	return true
}

func Pseudonym(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return UnknownToken
	}

	mac := hmac.New(sha256.New, pseudonymKey)

	_, _ = mac.Write([]byte(trimmed))

	return PseudonymPrefix + hex.EncodeToString(mac.Sum(nil)[:pseudonymBytes])
}

func IdentifierToken(value string) string {
	trimmed := strings.TrimSpace(value)
	if IsCanonicalRoomID(trimmed) {
		return trimmed
	}

	return Pseudonym(trimmed)
}

func LoggerPrivacyKeys() []string {
	return []string{"room", "room_name", KeyChatID, "user_name", "thread_id"}
}
