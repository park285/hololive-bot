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

package ingress

import (
	"log/slog"
	"strings"

	"github.com/park285/iris-client-go/v3/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
)

// resolveRoom은 방 ID와 방 이름을 돌려준다. Iris webhook은 방 제목을 싣지 않으므로(iris-client-go webhook.Message) 이름은
// 모르는 값인 빈 문자열이다. 방 ID를 이름 자리에 넣으면 alarm·major event·member news 구독 upsert가 저장된 Kakao 방
// 이름을 방 ID로 덮어썼다. 표시가 필요한 읽기 경로는 빈 이름일 때 방 ID로 대체한다(관리 알림 목록 alarm_admin).
func resolveRoom(message *webhook.Message) (chatID, roomName string) {
	return message.Room, ""
}

// chatID가 canonical room id가 아니면 로그에서 방 제목으로 새어 나가므로, 로그 경계에는
// 원본 대신 이 attr만 전달한다. ACL과 응답 송신은 계속 raw chatID를 쓴다.
func roomLogAttr(chatID, roomName string) slog.Attr {
	return privacylog.RoomAttr(chatID, roomName)
}

func roomChatFromMessage(message *webhook.Message) (roomType, roomLinkID string) {
	if message == nil || message.JSON == nil {
		return "", ""
	}

	return strings.TrimSpace(message.JSON.RoomType), strings.TrimSpace(message.JSON.RoomLinkID)
}

func resolveUser(message *webhook.Message) (userID, userName string) {
	userID = "unknown"
	userName = userID

	if message.JSON != nil && message.JSON.UserID != "" {
		userID = message.JSON.UserID
		userName = userID
	}

	if message.Sender != nil {
		userName = *message.Sender
	}

	return userID, userName
}
