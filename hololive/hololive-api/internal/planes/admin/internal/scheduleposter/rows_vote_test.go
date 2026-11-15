package scheduleposter

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

var (
	errBadRequest = genai.APIError{Code: http.StatusBadRequest}
	offRow        = extractedRow{Date: slotDay, TZ: new("JST"), Status: posterstore.RowOff, Text: "お休み"}
	songRow       = extractedRow{Date: "2026-11-12", Start: new("22:00"), TZ: new("JST"), Status: posterstore.RowPlanned, Text: "歌枠"}
)

func withStatus(row extractedRow, status posterstore.RowStatus) extractedRow {
	row.Status = status

	return row
}

func koreanRows(titles ...string) []translatedRow {
	rows := make([]translatedRow, len(titles))
	for i, title := range titles {
		rows[i] = translatedRow{Index: i + 1, TitleKo: title}
	}

	return rows
}

func recordVoted(t *testing.T, model *fakeRowModel) *fakeRowStore {
	t.Helper()

	store := &fakeRowStore{pending: []posterstore.PendingRows{week}}

	require.NoError(t, rowsService(store, model, sophia).extractPendingRows(t.Context()))

	return store
}

func TestExtractVotesHoldAStatusTheRunsSplitOnAiring(t *testing.T) {
	t.Parallel()

	model := &fakeRowModel{
		extracted:  [][]extractedRow{{plannedRow}, {plannedRow}, {withStatus(plannedRow, posterstore.RowOff)}},
		translated: [][]translatedRow{{chatRowKo}},
	}
	store := recordVoted(t, model)

	require.Empty(t, store.failures)
	require.Len(t, store.recorded, 1)
	require.Equal(t, posterstore.RowTentative, store.recorded[0][0].Status, "다수결(planned)도 소수(off)도 카드에 단정하지 않는다")
	require.Equal(t, []string{"E6 실행 간 방송 유무 불일치: off/planned/planned"}, store.recorded[0][0].Warnings)
}

func TestExtractVotesHoldAStatusTwoRunsSplitOnAiring(t *testing.T) {
	t.Parallel()

	model := &fakeRowModel{
		extracted:   [][]extractedRow{{plannedRow}, {withStatus(plannedRow, posterstore.RowCancelled)}, {plannedRow}},
		extractErrs: []error{nil, nil, errBadRequest},
		translated:  [][]translatedRow{{chatRowKo}},
	}
	store := recordVoted(t, model)

	require.Len(t, store.recorded, 1)
	require.Equal(t, posterstore.RowTentative, store.recorded[0][0].Status)
	require.Equal(t, []string{"E6 실행 간 방송 유무 불일치: " + string(posterstore.RowCancelled) + "/planned"}, store.recorded[0][0].Warnings)
}

func TestExtractFailsWhenOnlyOneRunSucceeds(t *testing.T) {
	t.Parallel()

	model := &fakeRowModel{extracted: [][]extractedRow{{plannedRow}, {plannedRow}}, extractErrs: []error{nil, errBadRequest}}
	store := recordVoted(t, model)

	require.Equal(t, []string{reasonExtractionFailed}, store.failures, "성공한 한 실행만으로 행을 쓰지 않는다")
	require.Empty(t, store.recorded)
	require.Empty(t, model.prompts)
	require.Equal(t, extractRuns, model.extracts)
}

func TestExtractVotesDropARowOnlyOneRunSaw(t *testing.T) {
	t.Parallel()

	model := &fakeRowModel{
		extracted:  [][]extractedRow{{plannedRow, songRow}, {plannedRow}, {plannedRow}},
		translated: [][]translatedRow{{chatRowKo}},
	}
	store := recordVoted(t, model)

	require.Len(t, store.recorded, 1)
	require.Len(t, store.recorded[0], 1)
	require.Equal(t, "雑談", store.recorded[0][0].SourceText)
	require.Equal(t, []string{"E9 일부 실행에만 있어 뺀 행: 2026-11-12 22:00 歌枠"}, store.recorded[0][0].Warnings)
}

func TestExtractVotesKeepARowTwoRunsSaw(t *testing.T) {
	t.Parallel()

	model := &fakeRowModel{
		extracted:  [][]extractedRow{{plannedRow, songRow}, {plannedRow}, {plannedRow, songRow}},
		translated: [][]translatedRow{koreanRows("잡담", "노래")},
	}
	store := recordVoted(t, model)

	require.Len(t, store.recorded, 1)
	require.Len(t, store.recorded[0], 2)
	require.Empty(t, store.recorded[0][0].Warnings)
	require.Equal(t, "歌枠", store.recorded[0][1].SourceText)
	require.Equal(t, []string{"E8 일부 실행에만 있는 행: 2/3"}, store.recorded[0][1].Warnings)
}

func TestExtractVotesSettleSplitsWithinOneSideWithoutWarnings(t *testing.T) {
	t.Parallel()

	tentative := withStatus(plannedRow, posterstore.RowTentative)

	for name, tc := range map[string]struct {
		runs  [][]extractedRow
		errs  []error
		title string
		want  posterstore.RowStatus
	}{
		"majority":       {runs: [][]extractedRow{{plannedRow}, {tentative}, {plannedRow}}, title: "잡담", want: posterstore.RowPlanned},
		"airing tie":     {runs: [][]extractedRow{{plannedRow}, {tentative}, {plannedRow}}, errs: []error{nil, nil, errBadRequest}, title: "잡담", want: posterstore.RowTentative},
		"not airing tie": {runs: [][]extractedRow{{offRow}, {withStatus(offRow, posterstore.RowCancelled)}, {offRow}}, errs: []error{nil, nil, errBadRequest}, title: "휴방", want: posterstore.RowOff},
		"stopped twice":  {runs: [][]extractedRow{{offRow}, {withStatus(offRow, posterstore.RowCancelled)}, {withStatus(offRow, posterstore.RowCancelled)}}, title: "휴방", want: posterstore.RowCancelled},
	} {
		model := &fakeRowModel{extracted: tc.runs, extractErrs: tc.errs, translated: [][]translatedRow{koreanRows(tc.title)}}
		store := recordVoted(t, model)

		require.Len(t, store.recorded, 1, name)
		require.Equal(t, tc.want, store.recorded[0][0].Status, name)

		for _, warning := range store.recorded[0][0].Warnings {
			require.NotContains(t, warning, "E6", name)
		}
	}
}

func TestExtractVotesWarnWhenNoTwoRunsAgreeOnATime(t *testing.T) {
	t.Parallel()

	late, later := plannedRow, plannedRow

	late.Start, later.Start = new("22:00"), new("23:00")

	model := &fakeRowModel{extracted: [][]extractedRow{{plannedRow}, {late}, {later}}, translated: [][]translatedRow{{chatRowKo}}}
	store := recordVoted(t, model)

	require.Len(t, store.recorded, 1)
	require.Equal(t, []string{"E7 실행 간 값 불일치: 시작 21:00/22:00/23:00"}, store.recorded[0][0].Warnings)

	model = &fakeRowModel{extracted: [][]extractedRow{{plannedRow}, {late}, {plannedRow}}, translated: [][]translatedRow{{chatRowKo}}}
	store = recordVoted(t, model)

	require.Equal(t, "21:00", store.recorded[0][0].Start)
	require.Empty(t, store.recorded[0][0].Warnings)
}

func TestExtractVotesWarnWhenNoTwoRunsAgreeOnTextAndBadges(t *testing.T) {
	t.Parallel()

	members, spoken := plannedRow, plannedRow

	members.Badges, spoken.Text = []string{"メン限"}, "雑談 メン限"

	for name, tc := range map[string]struct {
		runs [][]extractedRow
		errs []error
		want string
	}{
		"two runs":   {runs: [][]extractedRow{{plannedRow}, {members}, {plannedRow}}, errs: []error{nil, nil, errBadRequest}, want: "雑談/雑談 [メン限]"},
		"three runs": {runs: [][]extractedRow{{plannedRow}, {members}, {spoken}}, want: "雑談/雑談 [メン限]/雑談 メン限"},
	} {
		model := &fakeRowModel{extracted: tc.runs, extractErrs: tc.errs, translated: [][]translatedRow{{chatRowKo}}}
		store := recordVoted(t, model)

		require.Len(t, store.recorded, 1, name)
		require.Contains(t, store.recorded[0][0].Warnings, "E7 실행 간 값 불일치: 원문·배지 "+tc.want, name)
	}
}

func TestExtractVotesCompareTimesAndZonesAsTheCardReadsThem(t *testing.T) {
	t.Parallel()

	early, padded, lower, bare := plannedRow, plannedRow, plannedRow, plannedRow
	pacific, daylight, standard := plannedRow, plannedRow, plannedRow

	early.Start, padded.Start, lower.Start, bare.Start = new("9:00"), new("09:00"), new("9:00"), new("9:00")
	lower.TZ, bare.TZ = new("jst"), nil
	pacific.TZ, daylight.TZ, standard.TZ = new("PT"), new("PDT"), new("pst")

	for name, tc := range map[string]struct {
		runs [][]extractedRow
		errs []error
	}{
		"padded hour": {runs: [][]extractedRow{{early}, {padded}, {early}}, errs: []error{nil, nil, errBadRequest}},
		"zone forms":  {runs: [][]extractedRow{{early}, {lower}, {bare}}},
		"same place":  {runs: [][]extractedRow{{pacific}, {daylight}, {standard}}},
	} {
		model := &fakeRowModel{extracted: tc.runs, extractErrs: tc.errs, translated: [][]translatedRow{{chatRowKo}}}
		store := recordVoted(t, model)

		require.Len(t, store.recorded, 1, name)
		require.Empty(t, store.recorded[0][0].Warnings, name)
	}
}

func TestExtractVotesHoldARowWhoseRunsSplitOnTheZone(t *testing.T) {
	t.Parallel()

	pacific := plannedRow

	pacific.TZ = new("PT")

	store := recordVoted(t, &fakeRowModel{extracted: [][]extractedRow{{pacific}, {pacific}, {plannedRow}}, translated: [][]translatedRow{{chatRowKo}}})

	require.Len(t, store.recorded, 1)
	require.Equal(t, posterstore.RowTentative, store.recorded[0][0].Status)
	require.Equal(t, "PT", store.recorded[0][0].SourceZone)
	require.Equal(t, []string{"E10 실행 간 시간대 불일치: JST/PT/PT"}, store.recorded[0][0].Warnings)

	store = recordVoted(t, &fakeRowModel{extracted: [][]extractedRow{{pacific}}, translated: [][]translatedRow{{chatRowKo}}})

	require.Equal(t, posterstore.RowPlanned, store.recorded[0][0].Status)
	require.Empty(t, store.recorded[0][0].Warnings)

	untimed := plannedRow

	untimed.Start = nil

	untimedPacific := untimed

	untimedPacific.TZ = new("PT")

	store = recordVoted(t, &fakeRowModel{extracted: [][]extractedRow{{untimedPacific}, {untimedPacific}, {untimed}}, translated: [][]translatedRow{{chatRowKo}}})

	require.Len(t, store.recorded, 1)
	require.Equal(t, posterstore.RowPlanned, store.recorded[0][0].Status, "시각 없는 칸의 시간대는 저장 때 다른 칸을 따르므로 갈려도 단정을 풀지 않는다")
	require.False(t, slices.ContainsFunc(store.recorded[0][0].Warnings, func(w string) bool { return strings.HasPrefix(w, "E10") }))
}

func TestExtractVotesKeepTheMostCommonZoneLabelOfTheAgreeingRuns(t *testing.T) {
	t.Parallel()

	korean, offset, bare := plannedRow, plannedRow, plannedRow

	korean.TZ, offset.TZ, bare.TZ = new(koreanZoneLabel), new("UTC+9"), nil

	for name, tc := range map[string]struct {
		runs [][]extractedRow
		want string
	}{
		"one korean":   {runs: [][]extractedRow{{korean}, {bare}, {plannedRow}}},
		"one offset":   {runs: [][]extractedRow{{offset}, {bare}, {bare}}},
		"tied":         {runs: [][]extractedRow{{korean}, {offset}, {bare}}},
		"most korean":  {runs: [][]extractedRow{{bare}, {korean}, {korean}}, want: koreanZoneLabel},
		"most offset":  {runs: [][]extractedRow{{offset}, {offset}, {plannedRow}}, want: "UTC+9"},
		"only same in": {runs: [][]extractedRow{{korean}, {korean}, {korean}}, want: koreanZoneLabel},
	} {
		model := &fakeRowModel{extracted: tc.runs, translated: [][]translatedRow{{chatRowKo}}}
		store := recordVoted(t, model)

		require.Len(t, store.recorded, 1, name)
		require.Equal(t, tc.want, store.recorded[0][0].SourceZone, name)
		require.Equal(t, slotDay, store.recorded[0][0].Day.Format(time.DateOnly), name)
	}
}

func TestExtractRecordsNoRowsWhenMostRunsSeeNoRow(t *testing.T) {
	t.Parallel()

	for _, runs := range [][][]extractedRow{{{}, {}, {plannedRow}}, {{plannedRow}, {}, {}}} {
		model := &fakeRowModel{extracted: runs}
		store := recordVoted(t, model)

		require.Equal(t, []string{reasonNoRows}, store.failures)
	}
}

type slowRunRowModel struct {
	*fakeRowModel

	arrived atomic.Int32
}

func (m *slowRunRowModel) extractRows(ctx context.Context, src source, prompt string) ([]extractedRow, usage, error) {
	if m.arrived.Add(1) == extractRuns {
		<-ctx.Done()

		return nil, usage{}, ctx.Err()
	}

	return m.fakeRowModel.extractRows(ctx, src, prompt)
}

func (m *slowRunRowModel) translateRows(ctx context.Context, prompt string) ([]translatedRow, usage, error) {
	if err := ctx.Err(); err != nil {
		return nil, usage{}, err
	}

	return m.fakeRowModel.translateRows(ctx, prompt)
}

func TestExtractLeavesTheTranslationBudgetWhenOneRunHangs(t *testing.T) {
	t.Parallel()

	store := &fakeRowStore{pending: []posterstore.PendingRows{week}}
	fake := &fakeRowModel{extracted: [][]extractedRow{{plannedRow}}, translated: [][]translatedRow{{chatRowKo}}}
	service := rowsService(store, fake, sophia)

	service.rowModel = &slowRunRowModel{fakeRowModel: fake}
	service.rowsLoop.budget, service.rowsLoop.translateBudget = 2*time.Second, 1900*time.Millisecond

	require.NoError(t, service.extractPendingRows(t.Context()))
	require.Empty(t, store.failures, "성공한 두 실행으로 번역까지 마친다")
	require.Len(t, store.recorded, 1)
}

func TestExtractFailsWhenTheRunsShareNoRow(t *testing.T) {
	t.Parallel()

	other := extractedRow{Date: "2026-11-14", Status: posterstore.RowPlanned, Text: "マイクラ"}
	model := &fakeRowModel{extracted: [][]extractedRow{{plannedRow}, {songRow}, {other}}}
	store := recordVoted(t, model)

	require.Equal(t, []string{reasonExtractionFailed}, store.failures, "일정 칸이 없는 그림(no_rows)으로 기록하지 않는다")
}

type concurrentRowModel struct {
	*fakeRowModel

	arrived atomic.Int32
	all     chan struct{}
}

func (m *concurrentRowModel) extractRows(ctx context.Context, src source, prompt string) ([]extractedRow, usage, error) {
	if m.arrived.Add(1) == extractRuns {
		close(m.all)
	}

	select {
	case <-m.all:
	case <-time.After(5 * time.Second):
		return nil, usage{}, errors.New("extract runs were not in flight together")
	}

	return m.fakeRowModel.extractRows(ctx, src, prompt)
}

func TestExtractRunsAreConcurrentAndEachLogsUsage(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	store := &fakeRowStore{pending: []posterstore.PendingRows{week}}
	fake := &fakeRowModel{extracted: [][]extractedRow{{plannedRow}}, translated: [][]translatedRow{{chatRowKo}}}
	service := rowsService(store, fake, sophia)

	service.rowModel = &concurrentRowModel{fakeRowModel: fake, all: make(chan struct{})}
	service.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	require.NoError(t, service.extractPendingRows(t.Context()))
	require.Len(t, store.recorded, 1)
	require.Equal(t, extractRuns, fake.extracts)

	extractUsage := 0

	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, `"msg":"Schedule poster model usage"`) && strings.Contains(line, `"stage":"rows_extract"`) {
			extractUsage++
		}
	}

	require.Equal(t, extractRuns, extractUsage)
}

func TestVoteExtractedReturnsAgreeingRunsUnchanged(t *testing.T) {
	t.Parallel()

	for _, poster := range readFixture[[]harnessPoster](t, "posters.json") {
		voted, err := voteExtracted([][]extractedRow{poster.Rows, poster.Rows, poster.Rows})
		require.NoError(t, err, poster.Name)
		require.Equal(t, poster.Rows, voted, poster.Name)

		voted, err = voteExtracted([][]extractedRow{poster.Rows, poster.Rows})
		require.NoError(t, err, poster.Name)
		require.Equal(t, poster.Rows, voted, poster.Name)
	}
}

func TestRowsPosterBudgetCoversATimedOutExtractRetry(t *testing.T) {
	t.Parallel()

	rows := defaultRowsConfig()

	require.Greater(t, rows.extractBudget(), 2*rows.Extract.timeout(), "추출 요청 하나가 시간 제한을 다 쓰고 다시 보낼 수 있어야 한다")
	require.Greater(t, rows.translateBudget(), 2*rows.Translate.timeout(), "추출이 예산을 다 써도 번역 요청을 다시 보낼 수 있어야 한다")
}
