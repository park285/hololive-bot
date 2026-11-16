package adminruntime

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const revisionConflictResponse = `{"error":"schedule poster changed; refresh before retrying","code":"revision_conflict"}`

type polishedSlots struct {
	UpdatedAt string `json:"updatedAt"`
	Slots     []struct {
		Ordinal     *int    `json:"ordinal"`
		AddedPostID *string `json:"addedPostId"`
		AddedItem   *int    `json:"addedItem"`
		EditedAt    *string `json:"editedAt"`
		TitleKo     string  `json:"titleKo"`
		NoteKo      string  `json:"noteKo"`
		DoubtKo     string  `json:"doubtKo"`
	} `json:"slots"`
}

func readPolishedSlots(t *testing.T, serve adminTestServe, poster string) polishedSlots {
	t.Helper()

	rec := serve(http.MethodGet, poster+"/rows", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got polishedSlots

	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &got))

	return got
}

func TestSchedulePosterPolishMatchesAdminOpenAPI(t *testing.T) {
	spec := loadAdminOpenAPI(t)
	serve, _, pool := newAdminTestRouter(t, &recordingIrisRooms{})

	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	fx := seedPosterChanges(t, pool, store)
	fx.seedSlotDetails(t, pool)

	const (
		rowRoute   = schedulePosterRoute + "/rows/polish"
		addedRoute = schedulePosterRoute + "/changes/polish"
	)

	poster := schedulePostersBase + "/" + strconv.FormatInt(fx.posterID, 10)
	rowPath, addedPath := poster+"/rows/polish", poster+"/changes/polish"
	done := mustJSON(t, map[string]string{"id": strconv.FormatInt(fx.posterID, 10)})
	before := readPolishedSlots(t, serve, poster)
	seen := `"expectedUpdatedAt":"` + before.UpdatedAt + `"`
	row := `{` + seen + `,"ordinal":1,"titleKo":" 마크 ","withKo":["쿠로가네 노에루"],"placeKo":"","tagsKo":[],"noteKo":"첫 플레이"}`

	require.Nil(t, before.Slots[0].EditedAt)

	spec.runSteps(t, serve, pool, []schedulePosterStep{
		{http.MethodPost, rowPath, rowRoute, `{` + seen + `,"ordinal":1,"titleKo":" "}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, rowPath, rowRoute, `{` + seen + `,"ordinal":0,"titleKo":"마크"}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, rowPath, rowRoute, `{` + seen + `,"ordinal":1,"postId":"7","titleKo":"마크"}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, rowPath, rowRoute, `{"expectedUpdatedAt":"어제","ordinal":1,"titleKo":"마크"}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, schedulePosterNoSuch + "/rows/polish", rowRoute, row, http.StatusNotFound, "", ""},
		{http.MethodPost, rowPath, rowRoute, `{` + seen + `,"ordinal":9,"titleKo":"마크"}`, http.StatusConflict, "", revisionConflictResponse},
		{http.MethodPost, rowPath, rowRoute, row, http.StatusOK, "", done},
		{http.MethodPost, rowPath, rowRoute, row, http.StatusConflict, "UPDATE schedule_poster_rows SET edited_at = edited_at + interval '2 seconds'", revisionConflictResponse},
		{http.MethodGet, poster + "/rows", schedulePosterRoute + "/rows", "", http.StatusOK, "", ""},
	})

	after := readPolishedSlots(t, serve, poster)
	require.Equal(t, "마크", after.Slots[0].TitleKo)
	require.Equal(t, "첫 플레이", after.Slots[0].NoteKo)
	require.NotNil(t, after.Slots[0].EditedAt)

	added := after.Slots[len(after.Slots)-1]
	require.NotNil(t, added.AddedPostID)
	require.NotNil(t, added.AddedItem)

	seen = `"expectedUpdatedAt":"` + after.UpdatedAt + `","postId":"` + *added.AddedPostID + `"`

	body := `{` + seen + `,"item":` + strconv.Itoa(*added.AddedItem) + `,"titleKo":"하루 휴방","tagsKo":["휴방"]}`

	spec.runSteps(t, serve, pool, []schedulePosterStep{
		{http.MethodPost, addedPath, addedRoute, `{` + seen + `,"item":0,"titleKo":"하루 휴방"}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, addedPath, addedRoute, `{` + seen + `,"item":1,"titleKo":"하루 휴방","tagsKo":[""]}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, addedPath, addedRoute, `{` + seen + `,"item":20,"titleKo":"하루 휴방"}`, http.StatusConflict, "", revisionConflictResponse},
		{http.MethodPost, addedPath, addedRoute, body, http.StatusOK, "", done},
		{http.MethodGet, poster + "/rows", schedulePosterRoute + "/rows", "", http.StatusOK, "", ""},
		{http.MethodPost, poster + "/reject", schedulePosterRoute + "/reject", `{"expectedRevision":"` + strconv.FormatInt(fx.revision, 10) + `"}`, http.StatusOK, "", ""},
		{http.MethodPost, addedPath, addedRoute, body, http.StatusConflict, "", invalidStateResponse},
	})
}

func TestSchedulePosterMemoMatchesAdminOpenAPI(t *testing.T) {
	spec := loadAdminOpenAPI(t)
	serve, _, pool := newAdminTestRouter(t, &recordingIrisRooms{})

	const memo = schedulePostersBase + "/memo"

	spec.runSteps(t, serve, pool, []schedulePosterStep{
		{http.MethodGet, memo, memo, "", http.StatusOK, "", `{"body":"","revision":"1","updatedAt":null}`},
		{http.MethodPost, memo, memo, `{"expectedRevision":"x","body":"메모"}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, memo, memo, `{"expectedRevision":"1","body":"탭\t문자"}`, http.StatusBadRequest, "", ""},
		{http.MethodPost, memo, memo, `{"expectedRevision":"1","body":" 朝活은 아침 방송으로\r\n문장형 제목은 명사구로 \n"}`, http.StatusOK, "", `{"revision":"2"}`},
		{http.MethodPost, memo, memo, `{"expectedRevision":"1","body":"늦은 저장"}`, http.StatusConflict, "", revisionConflictResponse},
		{http.MethodGet, memo, memo, "", http.StatusOK, "", ""},
	})

	var body string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT body FROM schedule_poster_translation_memo`).Scan(&body))
	require.Equal(t, "朝活은 아침 방송으로\n문장형 제목은 명사구로", body)
}
