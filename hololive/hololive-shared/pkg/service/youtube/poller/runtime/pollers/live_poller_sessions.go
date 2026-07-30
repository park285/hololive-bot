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

package pollers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 과거 tx 내 pre-read(read-then-upsert)는 active-active producer 간 stale 판단 경합이
// 있었다. 단조 가드(ENDED 불변·LIVE→UPCOMING 회귀 금지)와 COALESCE/GREATEST를 upsert
// SQL로 내려 단일 문장으로 원자화했다 — 여기서 기존 행을 읽어 병합하지 말 것.
func (p *LivePoller) saveLiveSession(ctx context.Context, channelID string, stream *domain.Stream, status domain.LiveStatus, now time.Time) error {
	session := buildLiveSession(channelID, stream, status, now)
	session.LastSeenAt = now.UTC().Truncate(time.Microsecond)
	if _, err := p.db.Exec(ctx, mustSQL("live_poller_sessions_0044_01.sql"),
		session.VideoID,
		session.ChannelID,
		session.Status,
		session.Title,
		session.ScheduledStartTime,
		session.StartedAt,
		session.EndedAt,
		session.LiveFirstSeenAt,
		session.TopicID,
		session.ThumbnailURL,
		session.LastSeenAt,
		liveSessionLastSeenMinAdvance.Microseconds(),
	); err != nil {
		return fmt.Errorf("save live session: %w", err)
	}
	return nil
}

// last_seen_at 전진이 이 간격 미만이고 다른 필드가 전부 동일하면 upsert가 행 갱신을
// 건너뛴다(쓰기 증폭 완화). 이 때문에 "다른 producer가 살아있는 스트림을 봤지만 아직
// 기록하지 않았을" 수 있는 폭이 이 간격만큼 생기고, markSessionEnded의 ENDED 판정
// fence도 정확히 이 간격만큼 과거로 물러나야 조기 종료가 성립하지 않는다. 소비자
// 신선도 계약(alarm checker 15m, birthday runner 30m)이 이 간격의 상한을 규정한다.
const liveSessionLastSeenMinAdvance = 2 * time.Minute

func loadExistingLiveSession(ctx context.Context, tx dbx.Querier, videoID string) (domain.YouTubeLiveSession, bool, error) {
	var existing domain.YouTubeLiveSession
	err := pgxscan.Get(ctx, tx, &existing, liveSessionSelectSQL+`
		WHERE video_id = $1`,
		videoID,
	)
	if err == nil {
		normalizeLiveSessionTimes(&existing)
		return existing, true, nil
	}
	if pgxscan.NotFound(err) {
		return domain.YouTubeLiveSession{}, false, nil
	}
	return domain.YouTubeLiveSession{}, false, fmt.Errorf("load existing live session: %w", err)
}

var liveSessionSelectSQL = mustSQL("live_poller_sessions_0093_02.sql")

func normalizeLiveSessionTimes(session *domain.YouTubeLiveSession) {
	if session == nil {
		return
	}
	session.LastSeenAt = session.LastSeenAt.UTC()
	if session.ScheduledStartTime != nil {
		value := session.ScheduledStartTime.UTC()
		session.ScheduledStartTime = &value
	}
	if session.StartedAt != nil {
		value := session.StartedAt.UTC()
		session.StartedAt = &value
	}
	if session.EndedAt != nil {
		value := session.EndedAt.UTC()
		session.EndedAt = &value
	}
	if session.LiveFirstSeenAt != nil {
		value := session.LiveFirstSeenAt.UTC()
		session.LiveFirstSeenAt = &value
	}
}

func buildLiveSession(channelID string, stream *domain.Stream, status domain.LiveStatus, now time.Time) *domain.YouTubeLiveSession {
	session := &domain.YouTubeLiveSession{
		VideoID:            stream.ID,
		ChannelID:          firstNonEmpty(stream.ChannelID, channelID),
		Status:             status,
		Title:              stream.Title,
		ScheduledStartTime: stream.StartScheduled,
		LiveFirstSeenAt:    liveFirstSeenAt(status, now),
		TopicID:            streamStringValue(stream.TopicID),
		ThumbnailURL:       streamStringValue(stream.Thumbnail),
	}

	if status == domain.LiveStatusLive {
		session.StartedAt = liveStartedAt(stream, now)
	}

	return session
}

func streamStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func liveFirstSeenAt(status domain.LiveStatus, now time.Time) *time.Time {
	if status != domain.LiveStatusLive {
		return nil
	}
	value := now.UTC()
	return &value
}

func firstNonEmpty(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func liveStartedAt(stream *domain.Stream, now time.Time) *time.Time {
	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		startedAt := stream.StartActual.UTC()
		return &startedAt
	}
	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		startedAt := stream.StartScheduled.UTC()
		return &startedAt
	}
	startedAt := now.UTC()
	return &startedAt
}
