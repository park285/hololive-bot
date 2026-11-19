package broadcasttypedecision

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/service/geminidecision"
)

var testNow = time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)

const (
	unknown0001 = "unknown0001"
	unknown0002 = "unknown0002"
	unknown0003 = "unknown0003"
	unknown0004 = "unknown0004"
	unknown0005 = "unknown0005"
	unknown0006 = "unknown0006"
)

type fakeStore struct {
	rows   []candidate
	saved  map[string]decision
	sinces []time.Time
	ended  []time.Time
}

func (s *fakeStore) candidates(_ context.Context, since, endedBefore time.Time, cursorAt *time.Time, cursorID string, limit int) ([]candidate, error) {
	s.sinces = append(s.sinces, since)
	s.ended = append(s.ended, endedBefore)

	page := make([]candidate, 0, limit)

	for _, row := range s.rows {
		if row.SortAt.Before(since) {
			continue
		}

		if cursorAt != nil && cmp.Or(row.SortAt.Compare(*cursorAt), cmp.Compare(row.VideoID, cursorID)) >= 0 {
			continue
		}

		if saved, ok := s.saved[row.VideoID]; ok && saved.Title == row.Title {
			continue
		}

		if page = append(page, row); len(page) == limit {
			break
		}
	}

	return page, nil
}

func (s *fakeStore) save(_ context.Context, d decision) error {
	s.saved[d.VideoID] = d

	return nil
}

type fakeJudge struct {
	answers map[string]string
	errs    map[string]error
	asked   []string
	texts   []string
}

func (j *fakeJudge) Decide(_ context.Context, request geminidecision.Request) (geminidecision.Response, error) {
	contents := request.Input.ArrayOfDecisionInputContent

	thumbnail, err := base64.StdEncoding.DecodeString(*contents[1].ImageContent.Data)
	if err != nil {
		return geminidecision.Response{}, fmt.Errorf("decode thumbnail: %w", err)
	}

	videoID := string(thumbnail)

	j.asked = append(j.asked, videoID)
	j.texts = append(j.texts, contents[0].TextContent.Text)

	if err := j.errs[videoID]; err != nil {
		return geminidecision.Response{}, err
	}

	choice := cmp.Or(j.answers[videoID], string(broadcasttype.Talk))

	return geminidecision.Response{Answers: map[string]geminidecision.Answer{
		questionKey: {Type: "choice", Choice: &choice, Confidence: new(0.77)},
	}}, nil
}

type fakeThumbnails struct {
	failing   map[string]bool
	missing   map[string]bool
	downloads map[string]int
}

func (f fakeThumbnails) Download(_ context.Context, entry *handlercore.BroadcastHistoryEntry) ([]byte, string, error) {
	if f.downloads != nil {
		f.downloads[entry.VideoID]++
	}

	if f.failing[entry.VideoID] {
		return nil, "", errors.New("thumbnail status 503")
	}

	if f.missing[entry.VideoID] {
		return nil, "", fmt.Errorf("%w: thumbnail status 404", handlercore.ErrBroadcastThumbnailNotFound)
	}

	return []byte(entry.VideoID), "image/jpeg", nil
}

func unknownRows(count int) []candidate {
	rows := make([]candidate, 0, count)
	for i := range count {
		rows = append(rows, candidate{
			VideoID: fmt.Sprintf("unknown%04d", count-i), ChannelID: "UCtestChannel", Title: "にょ",
			SortAt: testNow.Add(-time.Duration(i) * time.Minute),
		})
	}

	return rows
}

func testWorker(store *fakeStore, model judge, thumbs fakeThumbnails) *Worker {
	if store.saved == nil {
		store.saved = map[string]decision{}
	}

	return &Worker{
		store: store, judge: model, thumbnails: thumbs, logger: slog.New(slog.DiscardHandler),
		now: func() time.Time { return testNow }, failed: map[string]struct{}{},
	}
}

func TestWorkerSendsBroadcastDecisionRequest(t *testing.T) {
	t.Parallel()

	bodies := make(chan jsontext.Value, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1beta/decisions", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		bodies <- raw

		w.Header().Set("Content-Type", "application/json")

		if _, err := io.WriteString(w, `{"answers":{"broadcast_type":{"type":"choice","choice":"singing","confidence":0.9}}}`); err != nil {
			t.Error(err)
		}
	})

	server := httptest.NewTestServer(t, mux)
	worker := testWorker(&fakeStore{}, geminidecision.NewWithHTTPClient("test-key", server.Client()), fakeThumbnails{})
	require.NoError(t, worker.decide(t.Context(), candidate{VideoID: "talk0000001", Title: "星の歌 <live> & 雑談", TopicID: "Music"}))

	raw, err := os.ReadFile("testdata/broadcast_decision_request.json")
	require.NoError(t, err)

	want, got := jsontext.Value(raw), <-bodies
	require.NoError(t, want.Compact())
	require.NoError(t, got.Compact())
	require.Equal(t, want, got)
}

func TestPassDecidesOnlyBroadcastsTheRulesLeaveUnknown(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: []candidate{
		{VideoID: "game0000001", Title: "【Minecraft】建築", SortAt: testNow},
		{VideoID: "talk0000001", Title: "a", TopicID: "Rubinite", SortAt: testNow.Add(-time.Minute)},
		{VideoID: "sing0000001", Title: "【歌枠】", SortAt: testNow.Add(-2 * time.Minute)},
		{VideoID: "other000001", Title: "休憩", SortAt: testNow.Add(-3 * time.Minute)},
	}}
	judge := &fakeJudge{answers: map[string]string{"other000001": string(broadcasttype.Other)}}

	require.NoError(t, testWorker(store, judge, fakeThumbnails{}).pass(t.Context()))

	require.Equal(t, []string{"talk0000001", "other000001"}, judge.asked)
	require.Equal(t, []string{`{"title":"a","topic":"Rubinite"}` + "\n", `{"title":"休憩"}` + "\n"}, judge.texts)

	talk := store.saved["talk0000001"]
	require.Equal(t, outcomeDecided, talk.Outcome)
	require.Equal(t, "a", talk.Title)
	require.Equal(t, string(broadcasttype.Talk), *talk.Type)
	require.InDelta(t, 0.77, *talk.Confidence, 1e-9)
	require.Equal(t, geminidecision.Model, talk.Model)
	require.Equal(t, string(broadcasttype.Other), *store.saved["other000001"].Type)
}

func TestPassRecordsBlockedInputAndSkipsFailuresWithoutSaving(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: unknownRows(6)}
	judge := &fakeJudge{
		answers: map[string]string{unknown0004: "none_of_the_above"},
		errs: map[string]error{
			unknown0005: fmt.Errorf("%w: safety", geminidecision.ErrBlocked),
			unknown0002: fmt.Errorf("%w: invalid_request", geminidecision.ErrRejected),
		},
	}

	require.NoError(t, testWorker(store, judge, fakeThumbnails{failing: map[string]bool{unknown0006: true}}).pass(t.Context()))

	require.Equal(t, []string{unknown0005, unknown0004, unknown0003, unknown0002, unknown0001}, judge.asked,
		"썸네일이 없으면 묻지 않는다")
	require.Equal(t, decision{VideoID: unknown0005, Title: "にょ", Outcome: outcomeBlocked, Model: geminidecision.Model}, store.saved[unknown0005])
	require.Equal(t, []string{unknown0001, unknown0003, unknown0005}, slices.Sorted(maps.Keys(store.saved)),
		"썸네일 실패, 범위 밖 답, 400 거절은 저장하지 않는다")
}

func TestPassStopsWhenTheServiceIsUnavailableOrFailuresRepeat(t *testing.T) {
	t.Parallel()

	unavailable := &fakeJudge{errs: map[string]error{unknown0004: fmt.Errorf("%w: 429", geminidecision.ErrUnavailable)}}
	store := &fakeStore{rows: unknownRows(4)}

	err := testWorker(store, unavailable, fakeThumbnails{}).pass(t.Context())
	require.ErrorIs(t, err, geminidecision.ErrUnavailable)
	require.Equal(t, []string{unknown0004}, unavailable.asked)
	require.Empty(t, store.saved)

	thumbs := fakeThumbnails{failing: map[string]bool{unknown0005: true, unknown0004: true, unknown0003: true}}
	judge := &fakeJudge{}

	store = &fakeStore{rows: unknownRows(5)}

	require.Error(t, testWorker(store, judge, thumbs).pass(t.Context()))
	require.Empty(t, judge.asked, "연속 실패 한도에서 회차를 멈춘다")
}

func TestPassSkipsMissingThumbnailsWithoutStopping(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: unknownRows(6)}
	judge := &fakeJudge{}
	worker := testWorker(store, judge, fakeThumbnails{missing: map[string]bool{
		unknown0006: true, unknown0005: true, unknown0004: true, unknown0003: true,
	}})

	require.NoError(t, worker.pass(t.Context()))
	require.Equal(t, []string{unknown0002, unknown0001}, judge.asked, "404 썸네일은 연속 실패로 세지 않는다")
	require.Equal(t, []string{unknown0003, unknown0004, unknown0005, unknown0006}, slices.Sorted(maps.Keys(worker.failed)))
	require.Equal(t, testNow, worker.lastFullScan)
}

func TestMissingThumbnailsDoNotResetConsecutiveFailures(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: unknownRows(6)}
	judge := &fakeJudge{}
	thumbs := fakeThumbnails{
		failing: map[string]bool{unknown0006: true, unknown0004: true, unknown0002: true},
		missing: map[string]bool{unknown0005: true, unknown0003: true},
	}

	err := testWorker(store, judge, thumbs).pass(t.Context())
	require.ErrorContains(t, err, "stop after 3 consecutive failures")
	require.NotErrorIs(t, err, handlercore.ErrBroadcastThumbnailNotFound)
	require.Empty(t, judge.asked, "404 사이의 일반 실패 셋은 회차를 멈춘다")
}

func TestPassScansFullHistoryUntilFinishedThenRecentWindow(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: unknownRows(maxDecisionsPerPass + pageSize + 5)}
	judge := &fakeJudge{}
	worker := testWorker(store, judge, fakeThumbnails{})

	require.NoError(t, worker.pass(t.Context()))
	require.Len(t, judge.asked, maxDecisionsPerPass)
	require.True(t, worker.lastFullScan.IsZero(), "회차 상한에 걸린 전체 훑기는 끝나지 않았다")

	for range 3 {
		require.NoError(t, worker.pass(t.Context()))
	}

	require.Len(t, store.saved, len(store.rows))
	require.Equal(t, testNow, worker.lastFullScan)

	full := testNow.Add(-handlers.MaxBroadcastHistoryDays * 24 * time.Hour)
	require.Equal(t, full, store.sinces[0])
	require.Equal(t, testNow.Add(-endGrace), store.ended[0])
	require.Equal(t, full, store.sinces[len(store.sinces)-1])

	require.NoError(t, worker.pass(t.Context()))
	require.Equal(t, testNow.Add(-recentWindow), store.sinces[len(store.sinces)-1], "전체 훑기를 마친 뒤 하루 동안은 최근만 훑는다")
}

func TestParseAnswerRejectsMissingOrOutOfRangeAnswers(t *testing.T) {
	t.Parallel()

	answer := func(choice string, confidence float64) geminidecision.Response {
		return geminidecision.Response{Answers: map[string]geminidecision.Answer{
			questionKey: {Choice: &choice, Confidence: &confidence},
		}}
	}

	got, err := parseAnswer(answer("horse_racing", 0.61))
	require.NoError(t, err)
	require.Equal(t, handlers.BroadcastTypeDecision{Type: broadcasttype.HorseRacing, Confidence: 0.61}, got)

	for name, response := range map[string]geminidecision.Response{
		"missing":      {},
		"unknown":      answer(string(broadcasttype.Unknown), 0.9),
		"outside set":  answer("cooking", 0.9),
		"echoed token": answer("api_key:synthetic-broadcast-test-key", 0.9),
		"confidence":   answer("talk", 1.2),
		"no choice":    {Answers: map[string]geminidecision.Answer{questionKey: {Confidence: new(0.9)}}},
	} {
		_, err := parseAnswer(response)
		require.ErrorIs(t, err, errBadAnswer, name)
		require.NotContains(t, err.Error(), "synthetic-broadcast-test-key", name)
	}
}

func TestQuestionOffersEveryClassifiedType(t *testing.T) {
	t.Parallel()

	encoded, err := jsonv2.Marshal(question)
	require.NoError(t, err)

	var decoded struct {
		Type     string            `json:"type"`
		Criteria map[string]string `json:"criteria"`
	}

	require.NoError(t, jsonv2.Unmarshal(encoded, &decoded))
	require.Equal(t, "choice", decoded.Type)
	require.ElementsMatch(t, []string{
		"game", "talk", "singing", "asmr", "membership", "event", "horse_racing", "watchalong", "news", "other",
	}, slices.Collect(maps.Keys(decoded.Criteria)))
	require.NotContains(t, slices.Collect(maps.Values(decoded.Criteria)), "")
}

func TestFailedBroadcastsWaitForTheNextFullScan(t *testing.T) {
	t.Parallel()

	now := testNow
	store := &fakeStore{rows: unknownRows(2)}
	judge := &fakeJudge{errs: map[string]error{unknown0002: errors.New("decision status 500")}}
	worker := testWorker(store, judge, fakeThumbnails{})

	worker.now = func() time.Time { return now }

	require.NoError(t, worker.pass(t.Context()))
	require.Equal(t, []string{unknown0002, unknown0001}, judge.asked)

	now = now.Add(passInterval)

	require.NoError(t, worker.pass(t.Context()))
	require.Len(t, judge.asked, 2, "최근 훑기는 실패한 방송을 다시 묻지 않는다")

	now = now.Add(fullScanInterval)
	judge.errs = nil

	require.NoError(t, worker.pass(t.Context()))
	require.Equal(t, []string{unknown0002, unknown0001, unknown0002}, judge.asked, "다음 전체 훑기에서 한 번 다시 묻는다")
	require.Contains(t, store.saved, unknown0002)
}

func TestUnfinishedFullScanKeepsFailuresAndMovesOn(t *testing.T) {
	t.Parallel()

	now := testNow
	failing := map[string]bool{unknown0006: true, unknown0004: true, unknown0002: true}
	downloads := map[string]int{}
	store := &fakeStore{rows: unknownRows(6)}
	worker := testWorker(store, &fakeJudge{}, fakeThumbnails{failing: failing, downloads: downloads})

	worker.now = func() time.Time { return now }

	require.NoError(t, worker.pass(t.Context()))
	require.Equal(t, now, worker.lastFullScan)

	store.rows[5].Title = "new title"
	now = now.Add(fullScanInterval)

	require.Error(t, worker.pass(t.Context()), "계속 실패하는 방송 셋이 이어지면 회차를 멈춘다")

	now = now.Add(passInterval)

	require.NoError(t, worker.pass(t.Context()))
	require.Equal(t, now, worker.lastFullScan, "이어 가는 회차는 실패한 방송을 건너뛰고 전체 훑기를 마친다")
	require.Equal(t, "new title", store.saved[unknown0001].Title)
	require.Equal(t, map[string]int{unknown0006: 2, unknown0005: 1, unknown0004: 2, unknown0003: 1, unknown0002: 2, unknown0001: 2}, downloads,
		"실패한 방송은 전체 훑기 주기마다 한 번만 다시 묻는다")
}
