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

// Kakao 방 제목과 검색어는 엔트로피가 낮아 unsalted digest면 사전 대입으로 원문이 복원된다.
// 프로세스 밖으로 나가지 않는 key로 HMAC을 걸어, 한 프로세스 수명 안에서만 상관관계가
// 성립하고 로그 수집기 쪽에서는 원문을 되돌릴 수 없게 한다.
var pseudonymKey = []byte(rand.Text())

func RoomIDAttr(room string) slog.Attr {
	return slog.String(KeyRoomID, identifierToken(room))
}

func ChatIDAttr(chatID string) slog.Attr {
	return slog.String(KeyChatID, identifierToken(chatID))
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

func identifierToken(value string) string {
	trimmed := strings.TrimSpace(value)
	if IsCanonicalRoomID(trimmed) {
		return trimmed
	}

	return Pseudonym(trimmed)
}
