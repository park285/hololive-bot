// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package privacylog

import (
	"log/slog"
	"strings"

	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

const (
	KeyRoomID     = sharedprivacylog.KeyRoomID
	KeyChatID     = sharedprivacylog.KeyChatID
	KeyCacheKey   = sharedprivacylog.KeyCacheKey
	KeyCacheField = sharedprivacylog.KeyCacheField

	UnknownToken    = sharedprivacylog.UnknownToken
	PseudonymPrefix = sharedprivacylog.PseudonymPrefix
)

func RoomIDAttr(room string) slog.Attr {
	return sharedprivacylog.RoomIDAttr(room)
}

func ChatIDAttr(chatID string) slog.Attr {
	return sharedprivacylog.ChatIDAttr(chatID)
}

func IsCanonicalRoomID(value string) bool {
	return sharedprivacylog.IsCanonicalRoomID(value)
}

func Pseudonym(value string) string {
	return sharedprivacylog.Pseudonym(value)
}

func RoomAttr(chatID, roomName string) slog.Attr {
	return sharedprivacylog.RoomIDAttr(correlationSource(chatID, roomName))
}

func ChatAttr(chatID, roomName string) slog.Attr {
	return sharedprivacylog.ChatIDAttr(correlationSource(chatID, roomName))
}

// Iris webhook의 chat_id는 JSON 봉투가 없거나 비어서 도달할 수 있고, 그때 ingress가 방 제목을
// chatID로 승격시킨다. 로그 상관 키가 경로마다 갈리지 않도록 폴백 판정은 여기 한 곳에만 둔다.
func correlationSource(chatID, roomName string) string {
	if strings.TrimSpace(chatID) != "" {
		return chatID
	}

	return roomName
}
