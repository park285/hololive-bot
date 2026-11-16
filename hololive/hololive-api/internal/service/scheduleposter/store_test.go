package scheduleposter_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	dbtest "github.com/kapu/hololive-dbtest"
)

const sophiaChannel = "UCROQtXcp2loQEmvpe5rhJzQ"

func date(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.DateOnly, value)
	require.NoError(t, err)

	return parsed
}

func newStore(t *testing.T) *scheduleposter.Store {
	t.Helper()

	store, err := scheduleposter.NewStore(dbtest.NewPool(t))
	require.NoError(t, err)

	return store
}

func candidate(id int64, postedAt time.Time) scheduleposter.Candidate {
	return scheduleposter.Candidate{
		ID: id, ChannelID: sophiaChannel, XUserID: "2054825907916054528",
		PostURL: "https://x.com/i/status/1", ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig",
		TextExcerpt: "週間スケジュール", PostedAt: postedAt,
	}
}

var testPostedAt = time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC)

func thumbnailImage(data string) scheduleposter.Image {
	return scheduleposter.Image{MIME: "image/jpeg", Data: []byte(data)}
}

func confirmPoster101(t *testing.T, store *scheduleposter.Store) int64 {
	t.Helper()

	ctx := t.Context()

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt), candidate(102, testPostedAt.AddDate(0, 0, -3))}, false)
	require.NoError(t, err)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	revision, err := store.Confirm(ctx, 101, poster.Revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	return revision
}

func TestStoreInsertCandidatesIgnoresDuplicates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	inserted, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt), candidate(102, testPostedAt)}, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, inserted)

	inserted, err = store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt)}, false)
	require.NoError(t, err)
	require.Zero(t, inserted)

	summary, err := store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Summary{PendingCount: 2, LatestCandidateID: 102}, summary)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewPending, poster.ReviewStatus)
}

func TestStoreConfirmChecksRevisionWeekAndState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	_, err = store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt)}, false)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET rows_status = 'failed', rows_error = 'extraction_failed' WHERE id = 101`)
	require.NoError(t, err)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	weekStart, weekEnd := date(t, "2026-11-09"), date(t, "2026-11-15")

	_, err = store.Confirm(ctx, 101, poster.Revision+1, weekStart, weekEnd)
	require.ErrorIs(t, err, scheduleposter.ErrRevisionConflict)

	_, err = store.Confirm(ctx, 101, poster.Revision, date(t, "2026-12-01"), date(t, "2026-12-07"))
	require.ErrorIs(t, err, scheduleposter.ErrInvalidWeek)

	_, err = store.Confirm(ctx, 101, poster.Revision, weekStart, date(t, "2026-11-16"))
	require.ErrorIs(t, err, scheduleposter.ErrInvalidWeek)

	_, err = store.Confirm(ctx, 999, 1, weekStart, weekEnd)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)

	revision, err := store.Confirm(ctx, 101, poster.Revision, weekStart, weekEnd)
	require.NoError(t, err)

	confirmed, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.RowsPending, confirmed.RowsStatus, "확정은 행 추출을 예약한다")
	require.Empty(t, confirmed.RowsError)

	pending, err := store.PendingRows(ctx, weekStart, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.EqualValues(t, 101, pending[0].ID)

	_, err = store.Confirm(ctx, 101, revision, weekStart, weekEnd)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState)

	_, err = store.Confirm(ctx, 101, revision, date(t, "2026-12-01"), date(t, "2026-12-07"))
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "검토 대기가 아니면 주가 멀어도 상태 오류다")
}

func TestStoreUpdateWeekMovesTheWeekAndReextractsRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	revision := confirmPoster101(t, store)

	summary, err := store.Summary(ctx)
	require.NoError(t, err)

	nextWeek, nextEnd := date(t, "2026-11-16"), date(t, "2026-11-22")

	_, err = store.UpdateWeek(ctx, 101, revision+1, nextWeek, nextEnd)
	require.ErrorIs(t, err, scheduleposter.ErrRevisionConflict)

	_, err = store.UpdateWeek(ctx, 101, revision, date(t, "2026-11-23"), date(t, "2026-11-29"))
	require.ErrorIs(t, err, scheduleposter.ErrInvalidWeek, "게시일과 7일 넘게 떨어진 주")

	_, err = store.UpdateWeek(ctx, 101, revision, nextWeek, nextEnd.AddDate(0, 0, 1))
	require.ErrorIs(t, err, scheduleposter.ErrInvalidWeek, "7일을 넘는 범위")

	_, err = store.UpdateWeek(ctx, 102, 1, nextWeek, nextEnd)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "확정본만 고친다")

	_, err = store.UpdateWeek(ctx, 999, 1, nextWeek, nextEnd)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)

	updatedRevision, err := store.UpdateWeek(ctx, 101, revision, nextWeek, nextEnd)
	require.NoError(t, err, "행 추출을 기다리는 확정본도 주를 고친다")
	require.Equal(t, revision+1, updatedRevision)

	updated, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewerAdmin, updated.ReviewedBy)
	require.Equal(t, scheduleposter.RowsPending, updated.RowsStatus)

	after, err := store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, summary, after, "주 수정은 Console 새 후보 알림을 바꾸지 않는다")

	moved := onlyPendingRows(t, store)
	require.Equal(t, nextWeek, moved.WeekStart, "고친 주로 행을 다시 뽑는다")
}

func TestStoreWritesLeaveEarlierImageTranslationsAlone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	postedAt := time.Date(2099, time.November, 9, 3, 0, 0, 0, time.UTC)

	type translation struct {
		Status, Error string
		ImageBytes    *int
		At            *time.Time
	}

	read := func(id int64) translation {
		var got translation

		require.NoError(t, pool.QueryRow(ctx, `SELECT translation_status, translation_error, octet_length(translated_image), translated_at
			FROM schedule_posters WHERE id = $1`, id).Scan(&got.Status, &got.Error, &got.ImageBytes, &got.At))

		return got
	}

	for _, poster := range []struct {
		id            int64
		channel, seed string
	}{
		{401, sophiaChannel, `UPDATE schedule_posters SET translation_status = 'pending' WHERE id = $1`},
		{402, "UCy9mgxB8pn2C4aNK_MPthDQ", `UPDATE schedule_posters SET translation_status = 'succeeded', translated_image = '\xffd8ffd9'::bytea,
			translated_mime = 'image/jpeg', translated_at = now() WHERE id = $1`},
	} {
		id := poster.id
		confirmedPosterWithRows(t, store, id, poster.channel, postedAt, "2099-11-09", "2099-11-15")

		_, err := pool.Exec(ctx, poster.seed, id)
		require.NoError(t, err)

		before := read(id)

		current, err := store.Get(ctx, id)
		require.NoError(t, err)

		_, err = store.RequestRows(ctx, id, current.Revision)
		require.NoError(t, err, "poster %d", id)
		require.Equal(t, before, read(id), "poster %d", id)

		revision, err := store.UpdateWeek(ctx, id, current.Revision, date(t, "2099-11-10"), date(t, "2099-11-16"))
		require.NoError(t, err, "poster %d", id)
		require.Equal(t, before, read(id), "poster %d", id)

		_, err = store.Reject(ctx, id, revision)
		require.NoError(t, err, "poster %d", id)
		require.Equal(t, before, read(id), "poster %d", id)
	}
}

func TestStoreConfirmedWeekClosesSameMemberCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	jst := time.FixedZone("JST", 9*60*60)
	at := func(day int) time.Time { return time.Date(2026, time.November, day, 12, 0, 0, 0, jst) }

	other := candidate(301, at(10))

	other.ChannelID = "UCy9mgxB8pn2C4aNK_MPthDQ"

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{
		candidate(101, at(10)), candidate(102, at(9)), candidate(103, at(15)), candidate(104, at(8)), other,
	}, false)
	require.NoError(t, err)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	_, err = store.Confirm(ctx, 101, poster.Revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	inserted, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(105, at(12)), candidate(106, at(15))}, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, inserted, "확정한 주 안의 새 글은 받지 않고, 다음 주 포스터를 올리는 마지막 날 글은 받는다")

	pending, err := store.List(ctx, scheduleposter.ReviewPending, 10)
	require.NoError(t, err)

	ids := make([]int64, 0, len(pending))
	for i := range pending {
		ids = append(ids, pending[i].ID)
	}

	require.ElementsMatch(t, []int64{103, 104, 106, 301}, ids, "같은 멤버의 그 주 대기 후보만 닫는다")

	closed, err := store.Get(ctx, 102)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewExpired, closed.ReviewStatus)
}

func TestStoreInsertCommunityCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	const (
		melaChannel = "UC8eitCE9Z6EwUCs-VUi1blg"
		pattern     = "週間|今週|来週|スケジュール|schedule|weekly"
		thumbnail   = "https://yt3.ggpht.com/poster=s640-c-fcrop64=1,00000000ffffffff-rw-nd-v1"
	)

	jst := time.FixedZone("JST", 9*60*60)
	seen := func(day int) time.Time { return time.Date(2026, time.November, day, 18, 0, 0, 0, jst) }

	for _, post := range []struct {
		id, channel, text, images string
		at                        time.Time
	}{
		{"community:UgkxSchedule", melaChannel, "📅スケジュールのお知らせ📅 11/2(月)-11/8(日)までの予定", `[{"url":"` + thumbnail + `"}]`, seen(3)},
		{"community:UgkxNoImage", melaChannel, "スケジュール", `[]`, seen(3)},
		{"community:UgkxOtherText", melaChannel, "グッズ販売のお知らせ", `[{"url":"` + thumbnail + `"}]`, seen(3)},
		{"community:UgkxOtherHost", melaChannel, "スケジュール", `[{"url":"https://example.com/a.jpg"}]`, seen(3)},
		{"community:UgkxOld", melaChannel, "スケジュール", `[{"url":"` + thumbnail + `"}]`, seen(1).AddDate(0, 0, -20)},
		{"community:UgkxOtherMember", sophiaChannel, "スケジュール", `[{"url":"` + thumbnail + `"}]`, seen(3)},
	} {
		_, insertErr := pool.Exec(ctx, `INSERT INTO youtube_community_posts (post_id, channel_id, content_text, images, first_seen_at)
			VALUES ($1, $2, $3, $4::jsonb, $5)`, post.id, post.channel, post.text, post.images, post.at)
		require.NoError(t, insertErr)
	}

	since := seen(3).AddDate(0, 0, -8)

	inserted, err := store.InsertCommunityCandidates(ctx, []string{melaChannel}, since, pattern, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, inserted)

	inserted, err = store.InsertCommunityCandidates(ctx, []string{melaChannel}, since, pattern, false)
	require.NoError(t, err)
	require.Zero(t, inserted, "같은 글은 같은 id라 다시 넣지 않는다")

	listed, err := store.List(ctx, scheduleposter.ReviewPending, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, melaChannel, listed[0].ChannelID)
	require.Equal(t, "https://www.youtube.com/post/UgkxSchedule", listed[0].PostURL)
	require.Equal(t, "https://yt3.ggpht.com/poster=s0", listed[0].ImageURL)
	require.Less(t, listed[0].ID, int64(1)<<60+1, "X 게시물 번호와 겹치지 않는 범위다")
	require.Positive(t, listed[0].ID)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_community_posts (post_id, channel_id, content_text, images, first_seen_at)
		VALUES ('community:UgkxNoText', $1, NULL, $2::jsonb, $3)`, melaChannel, `[{"url":"`+thumbnail+`"}]`, seen(3))
	require.NoError(t, err)

	inserted, err = store.InsertCommunityCandidates(ctx, []string{melaChannel}, since, "", true)
	require.NoError(t, err)
	require.EqualValues(t, 2, inserted, "빈 문구 패턴은 본문과 상관없이 이미지 글을 모두 넣는다")

	unreviewed, err := store.UnreviewedCandidates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, unreviewed, 2)

	for _, item := range unreviewed {
		poster, getErr := store.Get(ctx, item.ID)
		require.NoError(t, getErr)
		require.Contains(t, []string{"グッズ販売のお知らせ", ""}, poster.TextExcerpt)
	}
}

func TestStoreAutoReviewCandidatesWaitUntilHandedToAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt)}, false)
	require.NoError(t, err)

	revision, err := store.Reject(ctx, 101, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, revision)

	inserted, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{
		candidate(102, testPostedAt.Add(-time.Hour)), candidate(103, testPostedAt), candidate(105, testPostedAt.Add(-3*time.Hour)),
	}, true)
	require.NoError(t, err)
	require.EqualValues(t, 3, inserted)

	summary, err := store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Summary{PendingCount: 0, LatestCandidateID: 101}, summary,
		"자동 검토를 기다리는 후보는 대기 수도 최신 후보도 바꾸지 않아 Console 알림이 가지 않는다")
	require.Empty(t, pendingIDs(t, store), "검토 대기 목록도 넘긴 후보만 보여 준다")

	unreviewed, err := store.UnreviewedCandidates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, unreviewed, 3)
	require.Equal(t, []int64{103, 102, 105}, []int64{unreviewed[0].ID, unreviewed[1].ID, unreviewed[2].ID}, "최근 게시물부터 판정한다")

	unreviewed[0].PostedAt = unreviewed[0].PostedAt.UTC()
	require.Equal(t, scheduleposter.ReviewCandidate{
		ID: 103, ChannelID: sophiaChannel, ImageURL: candidate(103, testPostedAt).ImageURL, PostedAt: testPostedAt, Revision: 1,
	}, unreviewed[0])

	for _, id := range []int64{103, 102} {
		requested, requestErr := store.RequestReview(ctx, id, scheduleposter.ReviewReasonUncertain)
		require.NoError(t, requestErr)
		require.True(t, requested)
	}

	requested, err := store.RequestReview(ctx, 103, scheduleposter.ReviewReasonStale)
	require.NoError(t, err)
	require.False(t, requested, "이미 넘긴 후보는 다시 넘기지 않는다")

	handed, err := store.Get(ctx, 103)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewReasonUncertain, handed.ReviewRequestReason, "처음 넘긴 이유를 남긴다")

	summary, err = store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Summary{PendingCount: 2, LatestCandidateID: 102}, summary, "id가 작아도 나중에 넘긴 후보가 최신이다")
	require.ElementsMatch(t, []int64{102, 103}, pendingIDs(t, store))

	_, err = store.Reject(ctx, 105, 1)
	require.NoError(t, err)

	requested, err = store.RequestReview(ctx, 105, scheduleposter.ReviewReasonUncertain)
	require.NoError(t, err)
	require.False(t, requested, "검토 대기가 아닌 후보는 넘기지 않는다")
}

func pendingIDs(t *testing.T, store *scheduleposter.Store) []int64 {
	t.Helper()

	listed, err := store.List(t.Context(), scheduleposter.ReviewPending, 10)
	require.NoError(t, err)

	ids := make([]int64, 0, len(listed))
	for i := range listed {
		ids = append(ids, listed[i].ID)
	}

	return ids
}

func TestStoreHandOverUnreviewed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(104, testPostedAt), candidate(105, testPostedAt)}, true)
	require.NoError(t, err)

	_, err = store.Reject(ctx, 105, 1)
	require.NoError(t, err)

	handed, err := store.HandOverUnreviewed(ctx, time.Now().Add(-time.Hour), scheduleposter.ReviewReasonStale)
	require.NoError(t, err)
	require.Zero(t, handed, "기준 시각 뒤에 들어온 후보는 넘기지 않는다")

	handed, err = store.HandOverUnreviewed(ctx, time.Now().Add(time.Hour), scheduleposter.ReviewReasonStale)
	require.NoError(t, err)
	require.EqualValues(t, 1, handed, "거절한 105는 그대로 둔다")

	poster, err := store.Get(ctx, 104)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewReasonStale, poster.ReviewRequestReason)

	summary, err := store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Summary{PendingCount: 1, LatestCandidateID: 104}, summary)
	require.Equal(t, []int64{104}, pendingIDs(t, store))
}

const otherChannel = "UCy9mgxB8pn2C4aNK_MPthDQ"

func autoConfirmCandidates(t *testing.T, store *scheduleposter.Store) map[string]int64 {
	t.Helper()

	ctx := t.Context()
	other := candidate(104, testPostedAt.AddDate(0, 0, -3))

	other.ChannelID = otherChannel

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt.AddDate(0, 0, -2))}, false)
	require.NoError(t, err)

	_, err = store.InsertCandidates(ctx, []scheduleposter.Candidate{
		candidate(102, testPostedAt.AddDate(0, 0, -3)), candidate(103, testPostedAt.AddDate(0, 0, -1)), other,
	}, true)
	require.NoError(t, err)

	_, err = store.InsertThumbnailCandidates(ctx, []scheduleposter.ThumbnailCandidate{
		{ChannelID: sophiaChannel, VideoID: "4uuYjSf0rpU", Image: thumbnailImage("a"), PostedAt: testPostedAt.AddDate(0, 0, -1)},
		{ChannelID: otherChannel, VideoID: "4uuYjSf0rpU", Image: thumbnailImage("b"), PostedAt: testPostedAt.AddDate(0, 0, -1)},
	}, true)
	require.NoError(t, err)

	unreviewed, err := store.UnreviewedCandidates(ctx, 10)
	require.NoError(t, err)

	thumbnails := map[string]int64{}

	for _, item := range unreviewed {
		if strings.HasPrefix(item.ImageURL, "https://i.ytimg.com/") {
			thumbnails[item.ChannelID] = item.ID
		}
	}

	require.Len(t, thumbnails, 2)

	return thumbnails
}

func TestStoreAutoConfirm(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	thumbnails := autoConfirmCandidates(t, store)
	start, end := date(t, "2026-11-09"), date(t, "2026-11-15")

	_, err := store.Confirm(ctx, 101, 1, start, end)
	require.NoError(t, err)

	for _, tc := range []struct {
		name       string
		id         int64
		start, end time.Time
		coverage   scheduleposter.WeekCoverage
		status     scheduleposter.ReviewStatus
	}{
		{"같은 출처에서 늦게 올린 수정본은 닫지 않는다", 103, start, end, scheduleposter.WeekOverlapping, scheduleposter.ReviewPending},
		{"겹치는 다른 주는 닫지 않는다", 103, end, end.AddDate(0, 0, 6), scheduleposter.WeekOverlapping, scheduleposter.ReviewPending},
		{"다른 출처의 같은 일정은 닫는다", thumbnails[sophiaChannel], start, end, scheduleposter.WeekSame, scheduleposter.ReviewExpired},
		{"먼저 올린 같은 출처 글은 닫는다", 102, start, end, scheduleposter.WeekSame, scheduleposter.ReviewExpired},
		{"덮는 확정본이 없으면 확정한다", 104, start, end, scheduleposter.WeekUncovered, scheduleposter.ReviewConfirmed},
		{"자동 확정본은 다른 출처라도 닫지 않는다", thumbnails[otherChannel], start, end, scheduleposter.WeekOverlapping, scheduleposter.ReviewPending},
	} {
		coverage, _, autoErr := store.AutoConfirm(ctx, tc.id, 1, tc.start, tc.end)
		require.NoError(t, autoErr, tc.name)
		require.Equal(t, tc.coverage, coverage, tc.name)

		poster, getErr := store.Get(ctx, tc.id)
		require.NoError(t, getErr)
		require.Equal(t, tc.status, poster.ReviewStatus, tc.name)
	}

	_, _, err = store.AutoConfirm(ctx, 999, 1, start, end)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)

	_, _, err = store.AutoConfirm(ctx, 102, 1, start, end)
	require.ErrorIs(t, err, scheduleposter.ErrRevisionConflict, "닫은 후보는 revision이 올라 다시 바꾸지 않는다")

	requested, err := store.RequestReview(ctx, 103, scheduleposter.ReviewReasonWeekOverlapping)
	require.NoError(t, err)
	require.True(t, requested)

	_, _, err = store.AutoConfirm(ctx, 103, 1, start.AddDate(0, 0, 7), end.AddDate(0, 0, 7))
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "관리자 검토로 넘어간 후보는 자동 확정하지 않는다")

	_, err = store.AutoReject(ctx, 103, 1)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "관리자 검토로 넘어간 후보는 자동 거절하지 않는다")

	revision, err := store.AutoReject(ctx, thumbnails[otherChannel], 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, revision)

	_, err = store.AutoReject(ctx, 999, 1)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)
}

func TestStoreRecordsWhoReviewed(t *testing.T) {
	t.Parallel()

	start, end := date(t, "2026-11-09"), date(t, "2026-11-15")

	type adminChange func(ctx context.Context, store *scheduleposter.Store) (int64, error)

	for name, change := range map[string]adminChange{
		"주 수정": func(ctx context.Context, store *scheduleposter.Store) (int64, error) {
			poster, err := store.Get(ctx, 104)
			if err != nil {
				return 0, fmt.Errorf("get poster 104: %w", err)
			}

			return store.UpdateWeek(ctx, 104, poster.Revision, start, end)
		},
		"거절": func(ctx context.Context, store *scheduleposter.Store) (int64, error) {
			return store.Reject(ctx, 104, 2)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			store := newStore(t)
			thumbnails := autoConfirmCandidates(t, store)

			_, err := store.Confirm(ctx, 101, 1, start, end)
			require.NoError(t, err)

			_, _, err = store.AutoConfirm(ctx, 104, 1, start, end)
			require.NoError(t, err)

			_, err = store.AutoReject(ctx, thumbnails[otherChannel], 1)
			require.NoError(t, err)

			reviewers := func() []scheduleposter.Reviewer {
				ids := []int64{101, 104, thumbnails[otherChannel], 102}
				found := make([]scheduleposter.Reviewer, 0, len(ids))

				for _, id := range ids {
					poster, getErr := store.Get(ctx, id)
					require.NoError(t, getErr)

					found = append(found, poster.ReviewedBy)
				}

				return found
			}

			require.Equal(t, []scheduleposter.Reviewer{
				scheduleposter.ReviewerAdmin, scheduleposter.ReviewerAuto, scheduleposter.ReviewerAuto, "",
			}, reviewers(), "검토 대기 후보는 판정 주체가 없다")

			_, err = change(ctx, store)
			require.NoError(t, err)
			require.Equal(t, scheduleposter.ReviewerAdmin, reviewers()[1], "관리자가 자동 확정본을 바꾸면 관리자 판정이다")
		})
	}
}

func TestStoreInsertThumbnailCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	const (
		melaChannel = "UC8eitCE9Z6EwUCs-VUi1blg"
		freeChat    = "4uuYjSf0rpU"
		unknown     = "AAAAAAAAAAA"
	)

	thisWeek := thumbnailImage("this week")
	nextWeek := scheduleposter.Image{MIME: "image/webp", Data: []byte("next week")}

	_, err = pool.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id, channel_id, status, lifecycle_origin, title)
		VALUES ($1, $2, 'UPCOMING', 'observed', 'フリーチャット')`, freeChat, melaChannel)
	require.NoError(t, err)

	insert := func(video string, image scheduleposter.Image, at time.Time) int64 {
		t.Helper()

		inserted, insertErr := store.InsertThumbnailCandidates(ctx, []scheduleposter.ThumbnailCandidate{
			{ChannelID: melaChannel, VideoID: video, Image: image, PostedAt: at},
		}, false)
		require.NoError(t, insertErr)

		return inserted
	}

	require.EqualValues(t, 1, insert(freeChat, thisWeek, testPostedAt))
	require.Zero(t, insert(freeChat, thisWeek, testPostedAt.Add(time.Hour)), "같은 그림은 같은 id라 다시 넣지 않는다")
	require.EqualValues(t, 1, insert(unknown, thisWeek, testPostedAt), "생방송 기록이 없는 영상도 후보가 된다")

	listed, err := store.List(ctx, scheduleposter.ReviewPending, 10)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	posters := map[string]scheduleposter.Poster{}

	for _, poster := range listed {
		posters[poster.PostURL] = poster
	}

	poster := posters["https://www.youtube.com/watch?v="+freeChat]
	require.Equal(t, "https://i.ytimg.com/vi/"+freeChat+"/maxresdefault.jpg", poster.ImageURL)
	require.Equal(t, "フリーチャット", poster.TextExcerpt)
	require.Empty(t, posters["https://www.youtube.com/watch?v="+unknown].TextExcerpt)
	require.Less(t, poster.ID, int64(1)<<60+1, "X 게시물 번호와 겹치지 않는 범위다")
	require.Positive(t, poster.ID)

	stored, found, err := store.Image(ctx, poster.ID)
	require.NoError(t, err)
	require.True(t, found, "수집 때 받은 그림을 후보와 함께 저장한다")
	require.Equal(t, thisWeek, stored)

	_, err = store.Confirm(ctx, poster.ID, poster.Revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	sum := sha256.Sum256(thisWeek.Data)

	pending, err := store.PendingRows(ctx, date(t, "2026-11-09"), 10)
	require.NoError(t, err)
	require.Equal(t, []scheduleposter.PendingRows{{
		ID: poster.ID, ChannelID: melaChannel, ImageURL: poster.ImageURL, ImageSHA256: hex.EncodeToString(sum[:]),
		WeekStart: date(t, "2026-11-09"), WeekEnd: date(t, "2026-11-15"), RowsRevision: 1,
	}}, pending, "image_sha256은 저장한 그림의 sha256이다")

	require.Zero(t, insert(freeChat, nextWeek, testPostedAt.AddDate(0, 0, 3)), "확정된 주 안에 바뀐 그림은 후보가 되지 않는다")
	require.EqualValues(t, 1, insert(freeChat, nextWeek, testPostedAt.AddDate(0, 0, 6)), "주 종료일부터 바뀐 그림은 다음 주 후보가 된다")
}

func TestStoreInsertThumbnailCandidatesFillsTheImageOfTheSameThumbnail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	const (
		melaChannel = "UC8eitCE9Z6EwUCs-VUi1blg"
		confirmed   = "4uuYjSf0rpU"
		superseded  = "BBBBBBBBBBB"
	)

	thisWeek := thumbnailImage("this week")

	insert := func(video string, image scheduleposter.Image, at time.Time) int64 {
		t.Helper()

		inserted, insertErr := store.InsertThumbnailCandidates(ctx, []scheduleposter.ThumbnailCandidate{
			{ChannelID: melaChannel, VideoID: video, Image: image, PostedAt: at},
		}, false)
		require.NoError(t, insertErr)

		return inserted
	}

	posterOf := func(video string) scheduleposter.Poster {
		t.Helper()

		var id int64

		require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM schedule_posters WHERE post_url = $1`,
			"https://www.youtube.com/watch?v="+video).Scan(&id))

		poster, getErr := store.Get(ctx, id)
		require.NoError(t, getErr)

		return poster
	}

	require.EqualValues(t, 1, insert(confirmed, thisWeek, testPostedAt))
	require.EqualValues(t, 1, insert(superseded, thisWeek, testPostedAt))

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET image_data = NULL, image_mime = NULL`)
	require.NoError(t, err)

	before := posterOf(confirmed)

	_, err = store.Confirm(ctx, before.ID, before.Revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	before = posterOf(confirmed)
	require.Equal(t, scheduleposter.ReviewExpired, posterOf(superseded).ReviewStatus, "같은 주 확정이 다른 후보를 만료시킨다")

	require.Zero(t, insert(confirmed, thisWeek, testPostedAt.Add(time.Hour)), "확정된 주 안이라 새 후보는 넣지 않는다")
	require.Zero(t, insert(superseded, thisWeek, testPostedAt.Add(time.Hour)))

	stored, found, err := store.Image(ctx, before.ID)
	require.NoError(t, err)
	require.True(t, found, "그림이 없는 같은 썸네일 확정본은 수집한 그림으로 채운다")
	require.Equal(t, thisWeek, stored)
	require.Equal(t, before, posterOf(confirmed), "그림만 채우고 revision·상태는 바꾸지 않는다")

	_, found, err = store.Image(ctx, posterOf(superseded).ID)
	require.NoError(t, err)
	require.False(t, found, "만료한 포스터는 채우지 않는다")
}

func TestStorePublishesLatestPostedPosterOfTheWeek(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	revised, original := candidate(201, testPostedAt.AddDate(0, 0, -1)), candidate(202, testPostedAt.AddDate(0, 0, -2))

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{revised, original}, false)
	require.NoError(t, err)

	for _, id := range []int64{202, 201} {
		_, err = store.Confirm(ctx, id, 1, date(t, "2026-11-09"), date(t, "2026-11-15"))
		require.NoError(t, err)
	}

	pending := pendingRows(t, store)
	require.Len(t, pending, 2)

	for _, item := range pending {
		require.True(t, recordChat(t, store, item))
	}

	posterID, _ := publishedTitles(t, store, sophiaChannel, "2026-11-11")
	require.EqualValues(t, 201, posterID, "커뮤니티·썸네일 id는 게시 순서가 아니므로 늦게 올린 포스터를 고른다")
}

func TestStoreRetention(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	postedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(201, postedAt), candidate(202, postedAt)}, false)
	require.NoError(t, err)

	expired, err := store.ExpirePending(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 2, expired)

	purged, err := store.Purge(ctx, time.Now().Add(48*time.Hour), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)

	listed, err := store.List(ctx, scheduleposter.ReviewExpired, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}
