package api

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

type rowsResponse struct {
	Today          string  `json:"today"`
	RowsDayShift   int     `json:"rowsDayShift"`
	PublishStart   *string `json:"publishStart"`
	PublishEnd     *string `json:"publishEnd"`
	PublishedToday bool    `json:"publishedToday"`
	TodayPosterID  *string `json:"todayPosterId"`
}

func getSchedulePosterRows(t *testing.T, store *scheduleposter.Store, id string, now time.Time) rowsResponse {
	t.Helper()

	handler := &SchedulePosterHandler{store: store, logger: newDiscardLogger(), now: func() time.Time { return now }}
	ctx, recorder := newAPITestContext(http.MethodGet, "/api/holo/schedule-posters/"+id+"/rows", nil)

	ctx.Params = gin.Params{{Key: "id", Value: id}}
	handler.GetSchedulePosterRows(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response rowsResponse

	require.NoError(t, jsonv2.Unmarshal(recorder.Body.Bytes(), &response))

	return response
}

func TestGetSchedulePosterRowsJudgesTodayInJST(t *testing.T) {
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	postedAt := time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC)

	_, err = store.InsertCandidates(t.Context(), []scheduleposter.Candidate{{
		ID: 101, ChannelID: "UCROQtXcp2loQEmvpe5rhJzQ", XUserID: "2054825907916054528",
		PostURL: scheduleposter.PostURL(101), ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig",
		TextExcerpt: "週間スケジュール", PostedAt: postedAt,
	}}, false)
	require.NoError(t, err)

	weekStart := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)

	_, err = store.Confirm(t.Context(), 101, 1, weekStart, weekStart.AddDate(0, 0, 6))
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `UPDATE schedule_posters SET rows_status = 'succeeded', rows_extracted_at = $1 WHERE id = 101`, postedAt.Add(time.Hour))
	require.NoError(t, err)

	get := func(now time.Time) rowsResponse { return getSchedulePosterRows(t, store, "101", now) }

	lastMinute, nextDay := time.Date(2026, time.November, 15, 14, 59, 0, 0, time.UTC), time.Date(2026, time.November, 15, 15, 0, 0, 0, time.UTC)

	require.Equal(t, rowsResponse{
		Today: "2026-11-15", PublishStart: new("2026-11-09"), PublishEnd: new("2026-11-15"), PublishedToday: true, TodayPosterID: new("101"),
	}, get(lastMinute))
	require.Equal(t, rowsResponse{
		Today: "2026-11-16", PublishStart: new("2026-11-09"), PublishEnd: new("2026-11-15"),
	}, get(nextDay), "UTC 15:00은 JST로 다음 날이라 주가 끝났다")

	_, err = pool.Exec(t.Context(), `UPDATE schedule_posters SET rows_day_shift = 1 WHERE id = 101`)
	require.NoError(t, err)

	require.Equal(t, rowsResponse{
		Today: "2026-11-16", RowsDayShift: 1, PublishStart: new("2026-11-10"), PublishEnd: new("2026-11-16"), PublishedToday: true, TodayPosterID: new("101"),
	}, get(nextDay))
}

func TestGetSchedulePosterRowsPointsAtTheUpcomingWeekWhenNothingCoversToday(t *testing.T) {
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	postedAt := time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC)
	weekStart := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)

	for i, id := range []int64{101, 102} {
		_, err = store.InsertCandidates(t.Context(), []scheduleposter.Candidate{{
			ID: id, ChannelID: "UCROQtXcp2loQEmvpe5rhJzQ", XUserID: "2054825907916054528",
			PostURL: scheduleposter.PostURL(id), ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig",
			TextExcerpt: "週間スケジュール", PostedAt: postedAt.AddDate(0, 0, 6*i),
		}}, false)
		require.NoError(t, err)

		start := weekStart.AddDate(0, 0, 7*i)

		_, err = store.Confirm(t.Context(), id, 1, start, start.AddDate(0, 0, 6))
		require.NoError(t, err)
	}

	_, err = pool.Exec(t.Context(), `UPDATE schedule_posters SET rows_status = 'succeeded', rows_extracted_at = $1 WHERE id = 102`, postedAt.Add(time.Hour))
	require.NoError(t, err)

	sunday := time.Date(2026, time.November, 15, 14, 59, 0, 0, time.UTC)

	require.Equal(t, rowsResponse{
		Today: "2026-11-15", PublishStart: new("2026-11-16"), PublishEnd: new("2026-11-22"), PublishedToday: true, TodayPosterID: new("102"),
	}, getSchedulePosterRows(t, store, "102", sunday), "오늘을 덮는 포스터에 행이 없으면 다가오는 주 포스터가 게시된다")
	require.Equal(t, rowsResponse{
		Today: "2026-11-15", PublishStart: new("2026-11-09"), PublishEnd: new("2026-11-15"), TodayPosterID: new("102"),
	}, getSchedulePosterRows(t, store, "101", sunday))
}
