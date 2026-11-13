package scheduleposter

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/kapu/hololive-shared/pkg/domain"
	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	sophiaUserID   = "2054825907916054528"
	sophiaChannel  = "UCROQtXcp2loQEmvpe5rhJzQ"
	mimePNG        = "image/png"
	mimeJPEG       = "image/jpeg"
	sampleImageURL = "https://pbs.twimg.com/media/a.jpg"
)

type fakeSources struct {
	pages     map[string]string
	cursors   []string
	imageURLs []string
	original  source
	err       error
}

func (f *fakeSources) statuses(_ context.Context, userID, cursor string) (statusPage, error) {
	if userID != sophiaUserID {
		return statusPage{}, errors.New("unexpected user")
	}

	f.cursors = append(f.cursors, cursor)

	name, ok := f.pages[cursor]
	if !ok {
		return statusPage{}, errors.New("unexpected cursor " + cursor)
	}

	raw, err := fs.ReadFile(os.DirFS("testdata"), name)
	if err != nil {
		return statusPage{}, fmt.Errorf("read status fixture: %w", err)
	}

	return decodeStatusPage(raw)
}

func (f *fakeSources) image(_ context.Context, rawURL string) (source, error) {
	f.imageURLs = append(f.imageURLs, rawURL)

	return f.original, f.err
}

type fakeStore struct {
	inserted          []posterstore.Candidate
	communityChannels []string
	communityPattern  string
	thumbnails        []posterstore.ThumbnailCandidate
	pending           []posterstore.PendingTranslation
	recorded          []posterstore.Image
	failures          []string
	inputs            []posterstore.TranslationInputs
}

func (f *fakeStore) InsertCandidates(_ context.Context, candidates []posterstore.Candidate) (int64, error) {
	f.inserted = append(f.inserted, candidates...)

	return int64(len(candidates)), nil
}

func (f *fakeStore) InsertCommunityCandidates(_ context.Context, channelIDs []string, _ time.Time, textPattern string) (int64, error) {
	f.communityChannels = append(f.communityChannels, channelIDs...)
	f.communityPattern = textPattern

	return 0, nil
}

func (f *fakeStore) InsertThumbnailCandidates(_ context.Context, candidates []posterstore.ThumbnailCandidate) (int64, error) {
	f.thumbnails = append(f.thumbnails, candidates...)

	return int64(len(candidates)), nil
}

func (f *fakeStore) ExpirePending(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeStore) Purge(context.Context, time.Time, int) (int64, error) { return 0, nil }

func (f *fakeStore) PendingTranslations(context.Context, int) ([]posterstore.PendingTranslation, error) {
	return f.pending, nil
}

func (f *fakeStore) RecordTranslation(_ context.Context, _ posterstore.PendingTranslation, translated posterstore.Image) (bool, error) {
	f.recorded = append(f.recorded, translated)

	return true, nil
}

func (f *fakeStore) RecordTranslationFailure(_ context.Context, _ posterstore.PendingTranslation, reason string, inputs posterstore.TranslationInputs) (bool, error) {
	f.failures = append(f.failures, reason)
	f.inputs = append(f.inputs, inputs)

	return true, nil
}

const sampleTable = "宙科そぴあ ⇒ 소라시나 소피아\nホロドリ配信 ⇒ 홀로도리 방송"

var sampleOriginalText = []string{"ホロドリ配信", "宙科そぴあ"}

type fakeTranslator struct {
	tables       []string
	textErr      error
	output       source
	outputs      []source
	imageErr     error
	originalText []string
	renders      [][]string
	textPrompts  []string
	imagePrompts []string
	imageInputs  []source
	originals    int
	checks       int
}

// fakeUsage는 fakeTranslator가 응답을 받은 호출마다 돌려주는 사용량이며, textErr는 응답을 받지 못한 호출로 본다.
var fakeUsage = usage{model: "fake-model", prompt: 1, output: 1}

// translateText는 tables를 차례로 내고 마지막 것을 되풀이한다.
func (f *fakeTranslator) translateText(_ context.Context, _ source, prompt string) (textTranslation, usage, error) {
	f.textPrompts = append(f.textPrompts, prompt)

	if f.textErr != nil {
		return textTranslation{}, usage{}, f.textErr
	}

	table, err := parseTranslationTable(f.tables[min(len(f.textPrompts), len(f.tables))-1])

	return textTranslation{table: table, webSearchQueries: 2}, fakeUsage, err
}

// transcribe는 "original"로 시작하는 그림을 원본으로 본다.
func (f *fakeTranslator) transcribe(_ context.Context, src source) ([]string, usage, error) {
	if strings.HasPrefix(string(src.Data), "original") {
		f.originals++

		return f.originalText, fakeUsage, nil
	}

	f.checks++

	return f.renders[min(f.checks, len(f.renders))-1], fakeUsage, nil
}

// renderImage는 outputs가 있으면 차례로 내고 마지막 것을 되풀이하며, imageErr는 이미지 없이 끝난 응답으로 본다.
func (f *fakeTranslator) renderImage(_ context.Context, src source, prompt string) (source, usage, error) {
	f.imagePrompts = append(f.imagePrompts, prompt)
	f.imageInputs = append(f.imageInputs, src)

	if len(f.outputs) > 0 {
		return f.outputs[min(len(f.imagePrompts), len(f.outputs))-1], fakeUsage, f.imageErr
	}

	return f.output, fakeUsage, f.imageErr
}

type fakeMembers struct {
	members []*domain.Member
	err     error
	calls   int
}

func (f *fakeMembers) GetAllMembers(context.Context) ([]*domain.Member, error) {
	f.calls++

	return f.members, f.err
}

var (
	sophia = &domain.Member{ChannelID: sophiaChannel, Name: "Sorashina Sopia", NameJa: "宙科そぴあ", NameKo: "소라시나 소피아"}
	mela   = &domain.Member{ChannelID: "UCmela", Name: "Achichi Mela", NameJa: "熱千めら", NameKo: "아치치 메라"}
)

func testService(store *fakeStore, sources sourceClient, translator posterTranslator, members ...*domain.Member) *Service {
	config := Config{PollSeconds: 3600, Targets: []Target{{XUserID: sophiaUserID, ChannelID: sophiaChannel}}}
	service := newService(config, store, store, sources, translator, &fakeMembers{members: members}, slog.New(slog.DiscardHandler))

	service.now = func() time.Time { return time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC) }
	service.retry = retry.RetryOptions{MaxAttempts: 3, Sleep: func(context.Context, time.Duration) bool { return true }}

	return service
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	canvas.Set(0, 0, color.RGBA{R: 255, A: 255})

	var buffer bytes.Buffer

	require.NoError(t, png.Encode(&buffer, canvas))

	return buffer.Bytes()
}

func TestCollectKeepsOnlyRecentOwnPosterOriginals(t *testing.T) {
	t.Parallel()

	sources := &fakeSources{pages: map[string]string{
		"":      "statuses_page1.json",
		"page2": "statuses_page2.json",
		"page3": "statuses_page3.json",
	}}
	store := &fakeStore{}

	require.NoError(t, testService(store, sources, &fakeTranslator{}).collect(t.Context()))

	require.Equal(t, []string{"", "page2", "page3"}, sources.cursors, "8일보다 오래된 본인 원글만 남은 페이지에서 멈춘다")
	require.Equal(t, []posterstore.Candidate{{
		ID:          2119784604752343207,
		ChannelID:   sophiaChannel,
		XUserID:     sophiaUserID,
		PostURL:     "https://x.com/i/status/2119784604752343207",
		ImageURL:    "https://pbs.twimg.com/media/HWr8oRIa0AAlqaZ.jpg?name=orig",
		TextExcerpt: "🌟週間スケジュール🌟\n　11/9(月) ～ 11/15(日)",
		PostedAt:    time.Unix(1794231008, 0),
	}}, store.inserted)
}

func TestTranslatePendingStoresJPEG(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{
		tables: []string{sampleTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: sampleOriginalText, renders: [][]string{{"소라시나 소피아", "홀로도리 방송"}},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator, mela, sophia).translatePending(t.Context()))

	require.Len(t, translator.textPrompts, 1)
	require.Contains(t, translator.textPrompts[0], "Poster owner: 宙科そぴあ (Sorashina Sopia) → 소라시나 소피아")
	require.Contains(t, translator.textPrompts[0], "熱千めら (Achichi Mela) → 아치치 메라")
	require.Equal(t, []string{buildImagePrompt(mustTable(t, sampleTable))}, translator.imagePrompts, "이미지 단계는 번역표대로만 그린다")
	require.Equal(t, 1, translator.checks, "저장할 JPEG를 판독해 검사한다")

	require.Len(t, store.recorded, 1)
	require.Equal(t, mimeJPEG, store.recorded[0].MIME)

	decoded, err := jpeg.DecodeConfig(bytes.NewReader(store.recorded[0].Data))
	require.NoError(t, err)
	require.Equal(t, 64, decoded.Width)
	require.Empty(t, store.failures)
}

func TestTranslatePendingRecordsFailureAfterRetries(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{textErr: errors.New("text model unavailable"), originalText: sampleOriginalText}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.textPrompts, 3)
	require.Empty(t, translator.imagePrompts, "번역표가 없으면 이미지를 그리지 않는다")
	require.Equal(t, []string{reasonTranslationFailed}, store.failures)
	require.Equal(t, []posterstore.TranslationInputs{{}}, store.inputs, "번역표를 만들지 못한 시도는 판독을 남기지 않는다")

	translator = &fakeTranslator{tables: []string{sampleTable}, imageErr: errors.New("image model unavailable"), originalText: sampleOriginalText}
	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Len(t, translator.textPrompts, 1, "이미지 단계 재시도는 번역표를 다시 만들지 않는다")
	require.Len(t, translator.imagePrompts, 3)
	require.Equal(t, []string{reasonTranslationFailed, reasonTranslationFailed}, store.failures)
	require.Empty(t, store.recorded)

	store.pending[0].Inputs = store.inputs[1]
	sources.err = errors.New("gone")
	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Equal(t, []string{reasonTranslationFailed, reasonTranslationFailed, reasonSourceUnavailable}, store.failures)
	require.Equal(t, store.inputs[1], store.inputs[2], "원본을 받지 못한 시도는 남긴 판독·번역표를 그대로 둔다")
}

func TestTranslatePendingUsesOnlyTheCollectedThumbnail(t *testing.T) {
	t.Parallel()

	const collected = "0682c5f2076f099c34cfdd15a9e063849ed437a49677e6fcc5b4198c76575be5" // "original"의 sha256

	thumbnail := posterstore.PendingTranslation{
		ID: 1, ChannelID: sophiaChannel, ImageURL: "https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg", ImageSHA256: collected, Revision: 2,
	}
	store := &fakeStore{pending: []posterstore.PendingTranslation{thumbnail}}
	translator := &fakeTranslator{
		tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: lines(tsuzuriOriginal), renders: [][]string{lines(tsuzuriGoodRender)},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("next week")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Equal(t, []string{reasonSourceUnavailable}, store.failures, "수집 뒤 바뀐 썸네일을 옛 후보의 주로 번역하지 않는다")
	require.Zero(t, translator.checks)
	require.Empty(t, translator.textPrompts)

	sources.original.Data = []byte("original")
	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Len(t, store.recorded, 1)
	require.Equal(t, []string{reasonSourceUnavailable}, store.failures)
}

func TestTranslatePendingRedrawsUntilTheCheckPasses(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{
		tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: lines(tsuzuriOriginal),
		renders:      [][]string{lines(tsuzuriBrokenRender), lines(tsuzuriGoodRender)},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.imagePrompts, 2)
	require.Contains(t, translator.imagePrompts[1], fixPromptHeader)
	require.Contains(t, translator.imagePrompts[1], `- "21:00" must appear 3 times in the translated poster but was found fewer times`)
	require.Len(t, store.recorded, 1)
	require.Empty(t, store.failures)
}

func TestTranslatePendingEditsTheBestDraftUntilTheCheckPasses(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	outputs := []source{
		{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		{MIME: mimePNG, Data: pngBytes(t, 65, 36)},
		{MIME: mimePNG, Data: pngBytes(t, 66, 36)},
		{MIME: mimePNG, Data: pngBytes(t, 67, 36)},
	}
	secondRender := strings.ReplaceAll(strings.Replace(tsuzuriGoodRender, "@shusuchii\n", "", 1), "21:00", "")
	translator := &fakeTranslator{
		tables: []string{tsuzuriTable}, outputs: outputs,
		originalText: lines(tsuzuriOriginal),
		renders:      [][]string{lines(tsuzuriBrokenRender), lines(secondRender), lines(tsuzuriBrokenRender), lines(tsuzuriGoodRender)},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}
	service := testService(store, sources, translator)

	service.retry.MaxAttempts = 4

	require.NoError(t, service.translatePending(t.Context()))

	firstDraft, secondDraft := mustJPEG(t, outputs[0]), mustJPEG(t, outputs[1])
	inputs := make([][]byte, 0, len(translator.imageInputs))

	for _, input := range translator.imageInputs {
		inputs = append(inputs, input.Data)
	}

	require.Equal(t, [][]byte{[]byte("original"), firstDraft, secondDraft, secondDraft}, inputs, "검사에 걸린 번역본 가운데 문제가 가장 적은 것을 이어 고친다")
	require.NotContains(t, translator.imagePrompts[1], keepProblemsHeader)

	fix, keep, ok := strings.Cut(translator.imagePrompts[3], keepProblemsHeader)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(fix, fixPromptHeader))
	require.Contains(t, fix, `- "21:00" must appear 3 times in the translated poster but was found fewer times`)
	require.Contains(t, fix, `- "SHUSUCHII" must appear 1 times in the translated poster but was found fewer times`)
	require.NotContains(t, fix, `"18:00"`, "기준 이미지에서 이미 맞은 문제는 고칠 목록에 넣지 않는다")
	require.Contains(t, keep, `- "18:00" must appear 2 times in the translated poster but was found fewer times`)
	require.Contains(t, keep, `"@shusuchii" → "@shusuchii"`)
	require.Contains(t, keep, `"21:00〜 OVERWATCH @キョーコ" → "21:00~ OVERWATCH @햐쿠토 쿄코"`)
	require.NotContains(t, keep, `"雑談" → "잡담"`, "고칠 문제와 관계없는 줄은 넘기지 않는다")
	require.Len(t, store.recorded, 1)
	require.Equal(t, mustJPEG(t, outputs[3]), store.recorded[0].Data)
}

func mustJPEG(t *testing.T, src source) []byte {
	t.Helper()

	data, err := toJPEG(src)
	require.NoError(t, err)

	return data
}

func TestTranslatePendingNeverStoresARenderThatFailsTheCheck(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{
		tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: lines(tsuzuriOriginal),
		renders:      [][]string{lines(tsuzuriBrokenRender)},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.imagePrompts, 3)
	require.Len(t, translator.textPrompts, 1, "번역표가 원본과 맞으면 다시 그릴 때 번역표를 다시 만들지 않는다")
	require.Empty(t, store.recorded)
	require.Equal(t, []string{reasonTranslationFailed}, store.failures)
	require.Equal(t, lines(tsuzuriOriginal), store.inputs[0].Transcript, "그리기가 실패해도 재번역이 쓸 판독과 번역표를 남긴다")
	require.Equal(t, mustTable(t, tsuzuriTable).String(), store.inputs[0].Table)
}

func TestTranslatePendingStopsRedrawingWhenTheBestDraftStopsImproving(t *testing.T) {
	t.Parallel()

	fewerProblems := strings.ReplaceAll(strings.Replace(tsuzuriGoodRender, "@shusuchii\n", "", 1), "21:00", "")

	for name, test := range map[string]struct {
		renders []string
		draws   int
	}{
		"same problems":                    {renders: []string{tsuzuriBrokenRender}, draws: 3},
		"an improvement restarts counting": {renders: []string{tsuzuriBrokenRender, tsuzuriBrokenRender, fewerProblems, tsuzuriBrokenRender}, draws: 5},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
			translator := &fakeTranslator{
				tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
				originalText: lines(tsuzuriOriginal),
			}

			for _, render := range test.renders {
				translator.renders = append(translator.renders, lines(render))
			}

			service := testService(store, &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}, translator)

			service.retry.MaxAttempts = 10

			require.NoError(t, service.translatePending(t.Context()))
			require.Len(t, translator.imagePrompts, test.draws)
			require.Empty(t, store.recorded)
			require.Equal(t, []string{reasonTranslationFailed}, store.failures)
		})
	}
}

func TestTranslatePendingLogsTheUsageOfEveryModelResponse(t *testing.T) {
	t.Parallel()

	pending := []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}
	usageStages := func(translator *fakeTranslator) []string {
		var logs bytes.Buffer

		service := testService(&fakeStore{pending: pending}, sources, translator)

		service.logger = slog.New(slog.NewJSONHandler(&logs, nil))

		require.NoError(t, service.translatePending(t.Context()))

		var stages []string

		for line := range bytes.Lines(logs.Bytes()) {
			var record struct {
				Msg   string `json:"msg"`
				Stage string `json:"stage"`
				Model string `json:"model"`
			}

			require.NoError(t, jsonv2.Unmarshal(line, &record))

			if record.Msg == "Schedule poster model usage" {
				require.Equal(t, fakeUsage.model, record.Model)

				stages = append(stages, record.Stage)
			}
		}

		return stages
	}

	require.Equal(t, []string{stageOriginal, stageText, stageImage, stageCheck, stageImage, stageCheck}, usageStages(&fakeTranslator{
		tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: lines(tsuzuriOriginal), renders: [][]string{lines(tsuzuriBrokenRender), lines(tsuzuriGoodRender)},
	}))
	require.Equal(t, []string{stageOriginal, stageText, stageImage, stageImage, stageImage}, usageStages(&fakeTranslator{
		tables: []string{tsuzuriTable}, imageErr: errors.New("no image"), originalText: lines(tsuzuriOriginal),
	}), "이미지 없이 끝난 응답도 토큰을 쓰므로 남긴다")
	require.Equal(t, []string{stageOriginal}, usageStages(&fakeTranslator{
		textErr: errors.New("text model unavailable"), originalText: lines(tsuzuriOriginal),
	}), "응답을 받지 못한 호출은 남기지 않는다")
}

func TestTranslatePendingReusesTheInputsOfAFailedRenderOnlyOnce(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{tables: []string{sampleTable}, imageErr: errors.New("image model unavailable"), originalText: sampleOriginalText}
	service := testService(store, &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}, translator, sophia)

	retranslate := func() posterstore.TranslationInputs {
		t.Helper()

		require.NoError(t, service.translatePending(t.Context()))

		store.pending[0].Inputs = store.inputs[len(store.inputs)-1]

		return store.pending[0].Inputs
	}

	saved := retranslate()
	require.Len(t, saved.SHA256, 32)
	require.Equal(t, sampleOriginalText, saved.Transcript)
	require.Equal(t, mustTable(t, sampleTable).String(), saved.Table)

	require.Empty(t, retranslate(), "다시 쓴 판독·번역표는 그 시도로 끝낸다")
	require.Equal(t, 1, translator.originals, "그리기에 실패한 시도의 판독을 다음 재번역 한 번이 다시 쓴다")
	require.Len(t, translator.textPrompts, 1)
	require.Len(t, translator.imagePrompts, 6)

	require.Equal(t, saved, retranslate())
	require.Equal(t, 2, translator.originals, "그다음 재번역은 판독부터 다시 한다")
	require.Len(t, translator.textPrompts, 2)
}

func TestTranslatePendingStartsOverWhenTheSavedTableNoLongerPassesTheCheck(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	withTime := sampleTable + "\n20:00〜 ⇒ 20:00~"
	translator := &fakeTranslator{
		tables: []string{withTime}, imageErr: errors.New("image model unavailable"),
		originalText: append(slices.Clone(sampleOriginalText), "20:00〜"),
	}
	service := testService(store, &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}, translator)

	require.NoError(t, service.translatePending(t.Context()))
	require.Len(t, store.inputs, 1)

	stale := store.inputs[0]

	stale.Table = sampleTable
	store.pending[0].Inputs = stale

	require.NoError(t, service.translatePending(t.Context()))
	require.Equal(t, 2, translator.originals, "지금의 대조를 통과하지 못하는 번역표를 남긴 입력은 판독부터 다시 한다")
	require.Len(t, translator.textPrompts, 2)
	require.Equal(t, mustTable(t, withTime).String(), store.inputs[1].Table)
}

func TestTranslationInputsKeyCoversEveryInputOfTheTranscriptAndTable(t *testing.T) {
	t.Parallel()

	config := TranslationConfig{Model: defaultModel, TextModel: defaultTextModel, TranscribeModel: defaultTranscribeModel}
	original := source{MIME: mimeJPEG, Data: []byte("original")}
	prompt := buildTextPrompt(sophiaChannel, []*domain.Member{sophia}, nil)
	base := translationInputsKey(config, original, prompt)

	withConfig := func(edit func(*TranslationConfig)) []byte {
		changed := config
		edit(&changed)

		return translationInputsKey(changed, original, prompt)
	}

	require.Equal(t, base, translationInputsKey(config, original, prompt))
	require.Equal(t, base, withConfig(func(c *TranslationConfig) { c.Model = "gemini-nano-banana-pro" }), "그리기 모델은 판독·번역표를 바꾸지 않는다")

	for name, key := range map[string][]byte{
		"text_model":       withConfig(func(c *TranslationConfig) { c.TextModel = defaultTranscribeModel }),
		"transcribe_model": withConfig(func(c *TranslationConfig) { c.TranscribeModel = defaultTextModel }),
		"image bytes":      translationInputsKey(config, source{MIME: mimeJPEG, Data: []byte("original, redrawn")}, prompt),
		"image type":       translationInputsKey(config, source{MIME: mimePNG, Data: original.Data}, prompt),
		"members":          translationInputsKey(config, original, buildTextPrompt(sophiaChannel, []*domain.Member{sophia, mela}, nil)),
		"glossary":         translationInputsKey(config, original, buildTextPrompt(sophiaChannel, []*domain.Member{sophia}, map[string]string{"ホロドリ": "홀로도리"})),
	} {
		require.NotEqual(t, base, key, name)
	}
}

func TestTranslatePendingRebuildsATableThatMissesATime(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	withTime := sampleTable + "\n20:00〜 ⇒ 20:00~"
	translator := &fakeTranslator{
		tables:       []string{sampleTable, withTime},
		output:       source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: append(slices.Clone(sampleOriginalText), "20:00〜"),
		renders:      [][]string{{"소라시나 소피아", "20:00~ 홀로도리 방송"}},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.textPrompts, 2)
	require.Contains(t, translator.textPrompts[1], `- "20:00" appears 1 times in the original but only 0 times in the Korean lines of the table`)
	require.Equal(t, []string{buildImagePrompt(mustTable(t, withTime))}, translator.imagePrompts, "원본과 맞는 번역표로만 그린다")
	require.Len(t, store.recorded, 1)
}

func TestBuildImagePromptListsOnlyChangedRows(t *testing.T) {
	t.Parallel()

	table := mustTable(t, "MON ⇒ 월\n12:00PM ⇒ 12:00PM\nCuphead ⇒ Cuphead\nwith めらち ⇒ 메라치와 함께")
	prompt := buildImagePrompt(table)

	require.Contains(t, prompt, "All 2 replacements must appear")
	require.True(t, strings.HasSuffix(prompt, "Replacements (original → Korean):\n1. \"MON\" → \"월\"\n2. \"with めらち\" → \"메라치와 함께\""))
	require.NotContains(t, prompt, `"12:00PM" →`, "바뀌지 않는 줄은 원본 그대로 남으므로 넘기지 않는다")
}

type stalledTranslator struct{ fakeTranslator }

func (*stalledTranslator) transcribe(ctx context.Context, _ source) ([]string, usage, error) {
	<-ctx.Done()

	return nil, usage{}, ctx.Err()
}

func TestTranslatePendingRecordsRowsThatOutliveTheirBudget(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{
		{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2},
		{ID: 2, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 1},
	}}
	service := testService(store, &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}, &stalledTranslator{})

	service.rowBudget = 10 * time.Millisecond

	require.NoError(t, service.translatePending(t.Context()))
	require.Equal(t, []string{reasonTranslationFailed, reasonTranslationFailed}, store.failures,
		"상한을 넘긴 행은 호출자 취소와 달리 실패로 기록하고 다음 행으로 넘어간다")
}

func TestTranslatePendingLeavesRowsPendingWithoutMembers(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{}
	service := testService(store, &fakeSources{}, translator)

	service.members = &fakeMembers{err: errors.New("db down")}

	require.Error(t, service.translatePending(t.Context()))
	require.Empty(t, translator.textPrompts)
	require.Empty(t, store.failures)
	require.Empty(t, store.recorded)
}

func TestTranslatePendingSkipsMemberReadWhenIdle(t *testing.T) {
	t.Parallel()

	members := &fakeMembers{}
	service := testService(&fakeStore{}, &fakeSources{}, &fakeTranslator{})

	service.members = members

	require.NoError(t, service.translatePending(t.Context()))
	require.Zero(t, members.calls)
}

func TestBuildTextPrompt(t *testing.T) {
	t.Parallel()

	graduated := &domain.Member{ChannelID: "UCold", Name: "Old", NameJa: "卒業メンバー", NameKo: "졸업 멤버", IsGraduated: true}
	otherOrg := &domain.Member{ChannelID: "UCvspo", Name: "Other", NameJa: "他社メンバー", NameKo: "타사 멤버", Org: "VSpo"}
	unnamed := &domain.Member{ChannelID: "UCnoja", Name: "No Ja", NameKo: "일본어 없음"}

	prompt := buildTextPrompt(sophiaChannel, []*domain.Member{mela, graduated, otherOrg, unnamed, sophia}, map[string]string{"ホロドリ": "홀로도리", "コラボ": "콜라보"})

	require.True(t, strings.HasPrefix(prompt, textPromptBase))

	owner := strings.Index(prompt, "Poster owner: 宙科そぴあ (Sorashina Sopia) → 소라시나 소피아")
	table := strings.Index(prompt, "Name table (Japanese")

	require.Positive(t, owner)
	require.Greater(t, table, owner, "포스터 주인을 대응표보다 먼저 적는다")
	require.Contains(t, prompt, "\n宙科そぴあ (Sorashina Sopia) → 소라시나 소피아")
	require.Contains(t, prompt, "\n熱千めら (Achichi Mela) → 아치치 메라")
	require.NotContains(t, prompt, "졸업 멤버")
	require.NotContains(t, prompt, "타사 멤버")
	require.NotContains(t, prompt, "일본어 없음")
	require.Contains(t, prompt, "Glossary (original → Korean):\nコラボ → 콜라보\nホロドリ → 홀로도리")
}

func TestRequestsSearchOnlyForTheTranslationTable(t *testing.T) {
	t.Parallel()

	original := source{MIME: mimeJPEG, Data: []byte("original")}
	high := &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}

	contents, text := textRequest(original, "text prompt")
	require.Len(t, contents[0].Parts, 2)
	require.Equal(t, "text prompt", contents[0].Parts[1].Text)
	require.Equal(t, []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}, text.Tools, "이미지 검색은 끄고 웹 검색만 켠다")
	require.Equal(t, high, text.ThinkingConfig)
	require.Empty(t, text.ResponseModalities)

	_, transcribe := transcribeRequest(original)
	require.Empty(t, transcribe.Tools)
	require.Equal(t, &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}, transcribe.ThinkingConfig)

	_, render := imageRequest(original, "image prompt")
	require.Empty(t, render.Tools)
	require.Equal(t, []string{"IMAGE"}, render.ResponseModalities)
	require.Equal(t, &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMedium}, render.ThinkingConfig, "이미지 단계는 모델 기본 thinking level을 쓴다")
	require.Equal(t, "2K", render.ImageConfig.ImageSize)
}

func TestParseTranslationTableKeepsOnlyTableLines(t *testing.T) {
	t.Parallel()

	table, err := parseTranslationTable("번역표입니다.\n- 宙科そぴあ ⇒ 소라시나 소피아\n\nホロドリ配信⇒홀로도리 방송\n⇒ 빈 원문\n참고: 검색 결과를 따랐습니다.")
	require.NoError(t, err)
	require.Equal(t, sampleTable, table.String())

	_, err = parseTranslationTable("번역할 글자가 없습니다.")
	require.Error(t, err)

	_, err = parseTranslationTable(strings.Repeat("あ ⇒ 아\n", maxTableLines+1))
	require.Error(t, err)

	_, err = parseTranslationTable("あ ⇒ " + strings.Repeat("아", maxTableLineRunes))
	require.Error(t, err)
}

func TestParsedTranslationTablesParseAgainFromTheirString(t *testing.T) {
	t.Parallel()

	longest, err := parseTranslationTable(strings.Repeat("あ", 100) + tableSeparator + strings.Repeat("아", maxTableLineRunes-103))
	require.NoError(t, err)

	again, err := parseTranslationTable(longest.String())
	require.NoError(t, err)
	require.Equal(t, longest, again)

	_, err = parseTranslationTable(strings.Repeat("あ", 100) + tableSeparator + strings.Repeat("아", maxTableLineRunes-102))
	require.Error(t, err, "구분자 양쪽에 공백을 넣으면 한도를 넘는 줄은 처음부터 받지 않는다")
}

func TestFirstImageSkipsThoughtImages(t *testing.T) {
	t.Parallel()

	response := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
		{Text: "검색으로 표기를 확인했습니다."},
		{Thought: true, InlineData: &genai.Blob{MIMEType: mimePNG, Data: []byte("draft")}},
		{InlineData: &genai.Blob{MIMEType: mimePNG, Data: []byte("final")}},
	}}}}}

	final, err := firstImage(response)
	require.NoError(t, err)
	require.Equal(t, []byte("final"), final.Data)

	response.Candidates[0].Content.Parts = response.Candidates[0].Content.Parts[:2]
	_, err = firstImage(response)
	require.Error(t, err)
}

func TestFirstImageReportsWhyResponseIsEmpty(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		response *genai.GenerateContentResponse
		want     string
	}{
		"blocked prompt": {
			response: &genai.GenerateContentResponse{PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: genai.BlockedReasonProhibitedContent}},
			want:     "block reason PROHIBITED_CONTENT",
		},
		"candidate without content": {
			response: &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonImageSafety}}},
			want:     "finish reason IMAGE_SAFETY",
		},
		"output budget spent on thinking": {
			response: &genai.GenerateContentResponse{
				Candidates:    []*genai.Candidate{{FinishReason: genai.FinishReasonMaxTokens}},
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{ThoughtsTokenCount: 32000, CandidatesTokenCount: 768},
			},
			want: "finish reason MAX_TOKENS; thoughts 32000 tokens, output 768 tokens",
		},
		"no reason": {response: &genai.GenerateContentResponse{}, want: "no reason given"},
	} {
		_, err := firstImage(tc.response)
		require.ErrorContains(t, err, tc.want, name)
	}
}

func TestToJPEGRejectsOversizedImages(t *testing.T) {
	t.Parallel()

	_, err := toJPEG(source{MIME: mimePNG, Data: pngBytes(t, maxImageDimension+1, 8)})
	require.Error(t, err)

	_, err = toJPEG(source{MIME: mimePNG, Data: []byte("not an image")})
	require.Error(t, err)

	_, err = toJPEG(source{MIME: "image/gif", Data: pngBytes(t, 8, 8)})
	require.Error(t, err)
}

func TestCandidateFromSkipsPhotosOutsideXMedia(t *testing.T) {
	t.Parallel()

	service := testService(&fakeStore{}, &fakeSources{}, &fakeTranslator{})
	target := Target{XUserID: sophiaUserID, ChannelID: sophiaChannel}
	item := &status{ID: "2119784604752343207", Text: "週間スケジュール"}

	for photo, want := range map[string]bool{
		"https://pbs.twimg.com/media/a.jpg?name=orig": true,
		"http://pbs.twimg.com/media/a.jpg":            false,
		"https://video.twimg.com/a.mp4":               false,
		"https://user@pbs.twimg.com/media/a.jpg":      false,
	} {
		item.Media = jsontext.Value(`{"photos":[{"url":"` + photo + `"}]}`)

		_, ok := service.candidateFrom(item, target, time.Unix(1794231008, 0))
		require.Equal(t, want, ok, photo)
	}
}

func TestCandidateFromKeepsWeeklyPostersOnly(t *testing.T) {
	t.Parallel()

	service := testService(&fakeStore{}, &fakeSources{}, &fakeTranslator{})
	target := Target{XUserID: sophiaUserID, ChannelID: sophiaChannel}
	item := &status{ID: "2119784604752343207", Media: jsontext.Value(`{"photos":[{"url":"https://pbs.twimg.com/media/a.jpg?name=orig"}]}`)}

	for text, want := range map[string]bool{
		"🌟週間スケジュール🌟 11/9(月) ～ 11/15(日)":      true,
		"【11/09～11/15】 今週の予定となってます":          true,
		"📅今週のスケジュール📢 おまたせー！":                 true,
		"📅スケジュールのお知らせ📅 11/2(月)-11/8(日)までの予定": true,
		"Weekly Schedule": true,
		"📢今日の配信予定📅 ⏰１２時～":            false,
		"ARKアプデ入ったので、配信予定ズラして15:30～": false,
	} {
		item.Text = text

		_, ok := service.candidateFrom(item, target, time.Unix(1794231008, 0))
		require.Equal(t, want, ok, text)
	}
}

func TestSniffSourceUsesContentNotHeader(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		"\xff\xd8\xff\xe0jpeg":             "image/jpeg",
		"\x89PNG\r\n\x1a\npng":             "image/png",
		"RIFF\x00\x00\x00\x00WEBPVP8 webp": "image/webp",
	} {
		got, err := sniffSource([]byte(body))
		require.NoError(t, err)
		require.Equal(t, want, got.MIME)
	}

	_, err := sniffSource([]byte("<!doctype html><title>blocked</title>"))
	require.Error(t, err)
}

func TestTranslatePendingDoesNotRetryRejectedRequests(t *testing.T) {
	t.Parallel()

	pending := []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	for code, attempts := range map[int]int{400: 1, 403: 1, 429: 3, 503: 3} {
		store := &fakeStore{pending: pending}
		translator := &fakeTranslator{textErr: fmt.Errorf("generate poster translation table: %w", genai.APIError{Code: code})}

		require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
		require.Len(t, translator.textPrompts, attempts, "HTTP %d", code)
		require.Equal(t, []string{reasonTranslationFailed}, store.failures)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig("")
	require.ErrorIs(t, err, ErrDisabled)

	_, err = LoadConfig("relative.json")
	require.Error(t, err)

	write := func(body string) string {
		path := filepath.Join(t.TempDir(), "schedule-poster.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		return path
	}

	config, err := LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project"}}`))
	require.NoError(t, err)
	require.Equal(t, time.Hour, config.PollInterval())
	require.Equal(t, TranslationConfig{
		Project: "example-project", Location: "global", Model: "gemini-nano-banana-2.1", TextModel: "gemini-4-argon", TranscribeModel: "gemini-4-hydrogen",
	}, config.Translation)

	config, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","transcribe_model":"gemini-4-argon"}}`))
	require.NoError(t, err)
	require.Equal(t, "gemini-4-argon", config.Translation.TranscribeModel)

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","transcribe_model":"Gemini 4"}}`))
	require.Error(t, err)

	config, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","glossary":{"ホロドリ":"홀로도리"}}}`))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"ホロドリ": "홀로도리"}, config.Translation.Glossary)

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","glossary":{"ホロドリ":"홀로도리\n다음 지시"}}}`))
	require.Error(t, err, "줄바꿈이 든 용어는 프롬프트 한 줄을 넘으므로 거부한다")

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project"},"api_key":"x"}`))
	require.Error(t, err, "모르는 키는 거부한다")

	_, err = LoadConfig(write(`{"targets":[],"translation":{"project":"example-project"}}`))
	require.Error(t, err)

	config, err = LoadConfig(write(`{"targets":[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg","youtube_community":true}],"translation":{"project":"example-project"}}`))
	require.NoError(t, err, "커뮤니티만 보는 대상은 X 계정이 없어도 된다")
	require.Equal(t, []Target{{ChannelID: "UC8eitCE9Z6EwUCs-VUi1blg", YouTubeCommunity: true}}, config.Targets)

	_, err = LoadConfig(write(`{"targets":[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg"}],"translation":{"project":"example-project"}}`))
	require.Error(t, err, "출처가 하나도 없는 대상은 거부한다")

	config, err = LoadConfig(write(`{"targets":[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg","youtube_thumbnail_video_ids":["4uuYjSf0rpU"]}],"translation":{"project":"example-project"}}`))
	require.NoError(t, err, "썸네일만 보는 대상도 X 계정이 없어도 된다")
	require.Equal(t, []Target{{ChannelID: "UC8eitCE9Z6EwUCs-VUi1blg", YouTubeThumbnailVideoIDs: []string{"4uuYjSf0rpU"}}}, config.Targets)

	for _, targets := range []string{
		`[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg","youtube_thumbnail_video_ids":["4uuYjSf0rp"]}]`,
		`[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg","youtube_thumbnail_video_ids":["4uuYjSf0rpU/../a"]}]`,
		`[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg","youtube_thumbnail_video_ids":["4uuYjSf0rpU"]},{"channel_id":"UCROQtXcp2loQEmvpe5rhJzQ","youtube_thumbnail_video_ids":["4uuYjSf0rpU"]}]`,
		`[{"channel_id":"UC8eitCE9Z6EwUCs-VUi1blg","youtube_thumbnail_video_ids":["AAAAAAAAAA1","AAAAAAAAAA2","AAAAAAAAAA3","AAAAAAAAAA4","AAAAAAAAAA5","AAAAAAAAAA6"]}]`,
	} {
		_, err = LoadConfig(write(`{"targets":` + targets + `,"translation":{"project":"example-project"}}`))
		require.Error(t, err, targets)
	}
}

func TestCollectFetchesThumbnailTargets(t *testing.T) {
	t.Parallel()

	const melaChannel = "UC8eitCE9Z6EwUCs-VUi1blg"

	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("thumbnail")}}
	store := &fakeStore{}
	service := testService(store, sources, &fakeTranslator{})

	service.config.Targets = []Target{{ChannelID: melaChannel, YouTubeThumbnailVideoIDs: []string{"4uuYjSf0rpU"}}}

	require.NoError(t, service.collect(t.Context()))
	require.Empty(t, sources.cursors, "X 계정이 없는 대상은 FxTwitter를 부르지 않는다")
	require.Equal(t, []string{"https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg"}, sources.imageURLs)
	require.Equal(t, []posterstore.ThumbnailCandidate{{
		ChannelID: melaChannel, VideoID: "4uuYjSf0rpU", PostedAt: service.now(),
		ImageSHA256: "80f61f96184524ba54db767ed49487392430ad26bf5cf2ef689905f3400325d7", // "thumbnail"의 sha256
	}}, store.thumbnails)

	sources.err = errors.New("unexpected status 404")

	require.ErrorContains(t, service.collect(t.Context()), "collect thumbnail 4uuYjSf0rpU: unexpected status 404")
	require.Len(t, store.thumbnails, 1, "받지 못한 썸네일은 후보가 되지 않는다")
}

func TestCollectReadsCommunityOnlyTargetsFromStore(t *testing.T) {
	t.Parallel()

	sources := &fakeSources{pages: map[string]string{"": "statuses_page1.json", "page2": "statuses_page2.json", "page3": "statuses_page3.json"}}
	store := &fakeStore{}
	service := testService(store, sources, &fakeTranslator{})

	service.config.Targets = append(service.config.Targets, Target{ChannelID: "UC8eitCE9Z6EwUCs-VUi1blg", YouTubeCommunity: true})

	require.NoError(t, service.collect(t.Context()))
	require.Equal(t, []string{"", "page2", "page3"}, sources.cursors, "X 계정이 없는 대상은 FxTwitter를 부르지 않는다")
	require.Equal(t, []string{"UC8eitCE9Z6EwUCs-VUi1blg"}, store.communityChannels)
	require.Equal(t, posterTextExpr, store.communityPattern)
}
