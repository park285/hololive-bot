package scheduleposter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

func TestStoreSavesThePosterImageOnceWithoutTouchingThePoster(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)

	confirmPoster101(t, store)

	_, found, err := store.Image(ctx, 101)
	require.NoError(t, err)
	require.False(t, found, "X 후보는 그림 없이 들어간다")

	poster, err := store.Get(ctx, 101)
	require.NoError(t, err)

	pending := onlyPendingRows(t, store)
	first := scheduleposter.Image{MIME: "image/png", Data: []byte("first")}

	require.NoError(t, store.SaveImage(ctx, 101, first))
	require.NoError(t, store.SaveImage(ctx, 101, scheduleposter.Image{MIME: "image/jpeg", Data: []byte("second")}))

	stored, found, err := store.Image(ctx, 101)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first, stored, "저장한 그림은 바꾸지 않는다")

	saved, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, poster, saved, "revision·updated_at·검토·행 상태를 바꾸지 않는다")
	require.Equal(t, pending, onlyPendingRows(t, store), "진행 중인 행 추출의 rows_revision을 바꾸지 않는다")

	_, _, err = store.Image(ctx, 999)
	require.ErrorIs(t, err, scheduleposter.ErrNotFound)
}

func TestStoreDropsThePosterImageWhenThePosterCloses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := newStore(t)
	thumbnails := autoConfirmCandidates(t, store)
	start, end := date(t, "2026-11-09"), date(t, "2026-11-15")

	_, err := store.InsertCandidates(ctx, []scheduleposter.Candidate{candidate(105, testPostedAt.AddDate(0, 0, 1))}, false)
	require.NoError(t, err)

	for _, id := range []int64{101, 104, 105} {
		require.NoError(t, store.SaveImage(ctx, id, thumbnailImage("x")))
	}

	revision, err := store.Confirm(ctx, 101, 1, start, end)
	require.NoError(t, err)

	coverage, _, err := store.AutoConfirm(ctx, thumbnails[sophiaChannel], 1, start, end)
	require.NoError(t, err)
	require.Equal(t, scheduleposter.WeekSame, coverage)

	_, err = store.AutoReject(ctx, thumbnails[otherChannel], 1)
	require.NoError(t, err)

	_, err = store.Reject(ctx, 101, revision)
	require.NoError(t, err)

	_, err = store.ExpirePending(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		id     int64
		status scheduleposter.ReviewStatus
	}{
		{"확정이 닫은 같은 주 후보", 105, scheduleposter.ReviewExpired},
		{"같은 주 확정본이 있어 닫은 후보", thumbnails[sophiaChannel], scheduleposter.ReviewExpired},
		{"자동 거절", thumbnails[otherChannel], scheduleposter.ReviewRejected},
		{"확정본 거절", 101, scheduleposter.ReviewRejected},
		{"기한 만료", 104, scheduleposter.ReviewExpired},
	} {
		poster, getErr := store.Get(ctx, tc.id)
		require.NoError(t, getErr, tc.name)
		require.Equal(t, tc.status, poster.ReviewStatus, tc.name)

		require.NoError(t, store.SaveImage(ctx, tc.id, thumbnailImage("late")), tc.name)

		_, found, imageErr := store.Image(ctx, tc.id)
		require.NoError(t, imageErr, tc.name)
		require.False(t, found, "%s: 닫은 포스터에는 그림이 남지 않고 다시 저장하지도 않는다", tc.name)
	}
}
