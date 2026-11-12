package scheduleposter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
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

func confirmedPending(t *testing.T, store *scheduleposter.Store) scheduleposter.PendingTranslation {
	t.Helper()

	ctx := t.Context()

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt), candidate(102, testPostedAt.AddDate(0, 0, -3))})
	require.NoError(t, err)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	revision, err := store.Confirm(ctx, 101, poster.Revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	pending, err := store.PendingTranslations(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []scheduleposter.PendingTranslation{{ID: 101, ChannelID: sophiaChannel, ImageURL: candidate(101, testPostedAt).ImageURL, Revision: revision}}, pending)

	return pending[0]
}

func TestStoreInsertCandidatesIgnoresDuplicates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	inserted, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt), candidate(102, testPostedAt)})
	require.NoError(t, err)
	require.EqualValues(t, 2, inserted)

	inserted, err = store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt)})
	require.NoError(t, err)
	require.Zero(t, inserted)

	summary, err := store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Summary{PendingCount: 2, LatestCandidateID: 102}, summary)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewPending, poster.ReviewStatus)
	require.Equal(t, scheduleposter.TranslationNotRequested, poster.TranslationStatus)
}

func TestStoreConfirmChecksRevisionWeekAndState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(101, testPostedAt)})
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

	_, err = store.Confirm(ctx, 101, revision, weekStart, weekEnd)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState)

	_, err = store.RequestTranslation(ctx, 101, revision)
	require.ErrorIs(t, err, scheduleposter.ErrInvalidState, "번역 대기 중에는 재번역을 예약하지 않는다")
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
	})
	require.NoError(t, err)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	_, err = store.Confirm(ctx, 101, poster.Revision, date(t, "2026-11-09"), date(t, "2026-11-15"))
	require.NoError(t, err)

	inserted, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(105, at(12)), candidate(106, at(15))})
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
		pattern     = "週間スケジュール|スケジュール|schedule|予定|weekly"
		thumbnail   = "https://yt3.ggpht.com/poster=s640-c"
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

	inserted, err := store.InsertCommunityCandidates(ctx, []string{melaChannel}, since, pattern)
	require.NoError(t, err)
	require.EqualValues(t, 1, inserted)

	inserted, err = store.InsertCommunityCandidates(ctx, []string{melaChannel}, since, pattern)
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
}

func TestStoreRecordTranslationPublishesOnlyCurrentRevision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	pending := confirmedPending(t, store)
	image := scheduleposter.Image{MIME: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff}}

	stale := pending
	stale.Revision--

	recorded, err := store.RecordTranslation(ctx, stale, image)
	require.NoError(t, err)
	require.False(t, recorded)

	recorded, err = store.RecordTranslation(ctx, pending, image)
	require.NoError(t, err)
	require.True(t, recorded)

	published, found, err := store.PublishedImage(ctx, sophiaChannel, date(t, "2026-11-11"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, image, published)

	_, found, err = store.PublishedImage(ctx, sophiaChannel, date(t, "2026-11-16"))
	require.NoError(t, err)
	require.False(t, found)
}

func TestStoreRetranslationFailureAndReject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	pending := confirmedPending(t, store)

	image := scheduleposter.Image{MIME: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff}}
	day := date(t, "2026-11-11")

	recorded, err := store.RecordTranslation(ctx, pending, image)
	require.NoError(t, err)
	require.True(t, recorded)

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	revision, err := store.RequestTranslation(ctx, 101, poster.Revision)
	require.NoError(t, err)

	_, err = store.TranslatedImage(ctx, 101)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound, "Console 번역본 조회는 번역 성공 상태만 본다")

	poster, err = store.Get(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, poster.TranslatedAt, "번역 대기 행의 translatedAt은 비워 Console 응답 형식을 유지한다")

	published, found, err := store.PublishedImage(ctx, sophiaChannel, day)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, image, published, "재번역 중에도 이전 번역본을 게시한다")

	recorded, err = store.RecordTranslationFailure(ctx, scheduleposter.PendingTranslation{ID: 101, Revision: revision}, "translation_failed")
	require.NoError(t, err)
	require.True(t, recorded)

	poster, err = store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.TranslationFailed, poster.TranslationStatus)
	require.Equal(t, "translation_failed", poster.TranslationError)

	_, found, err = store.PublishedImage(ctx, sophiaChannel, day)
	require.NoError(t, err)
	require.True(t, found, "재번역이 실패해도 이전 번역본은 남는다")

	_, err = store.Reject(ctx, 101, poster.Revision)
	require.NoError(t, err)

	_, found, err = store.PublishedImage(ctx, sophiaChannel, day)
	require.NoError(t, err)
	require.False(t, found, "거절하면 게시를 내린다")

	poster, err = store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.ReviewRejected, poster.ReviewStatus)
	require.Equal(t, scheduleposter.TranslationNotRequested, poster.TranslationStatus)

	summary, err := store.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.Summary{PendingCount: 1, LatestCandidateID: 102}, summary)
}

func TestStoreRetention(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	postedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(201, postedAt), candidate(202, postedAt)})
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
