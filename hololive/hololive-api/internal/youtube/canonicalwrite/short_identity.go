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

package canonicalwrite

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// canonicalizeShortContentIDs는 NEW_SHORT outbox·tracking 행의 content_id를 'short:' canonical 형식으로 맞춘다.
// 이전에는 같은 영상의 raw 형식(접두사 없는) 기존 행을 두 테이블에서 찾아 그 content_id를 재사용했다. T18(2026-09-26)에서
// youtube_notification_outbox와 youtube_content_alarm_tracking의 raw 형식 NEW_SHORT 행이 0건임을 확인해 그 조회를
// 지웠다(stack-audit T11 holo-batchrepo-short-raw-identity-alias). 정규화할 수 없는 값은 이전처럼 공백만 걷어 두고,
// 판정은 저장 전 검증과 DB 제약이 맡는다.
func canonicalizeShortContentIDs(
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) {
	for i := range notifications {
		if notifications[i] == nil || notifications[i].Kind != domain.OutboxKindNewShort {
			continue
		}

		notifications[i].ContentID = canonicalShortContentID(notifications[i].ContentID)
	}

	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindNewShort {
			continue
		}

		trackingRows[i].ContentID = canonicalShortContentID(trackingRows[i].ContentID)
	}
}

func canonicalShortContentID(contentID string) string {
	if canonicalID := normalizeContentID(domain.OutboxKindNewShort, contentID); canonicalID != "" {
		return canonicalID
	}

	return strings.TrimSpace(contentID)
}
