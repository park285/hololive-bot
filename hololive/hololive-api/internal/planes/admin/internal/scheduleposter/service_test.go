package scheduleposter

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
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
	pages    map[string]string
	cursors  []string
	original source
	err      error
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

func (f *fakeSources) image(context.Context, string) (source, error) {
	return f.original, f.err
}

type fakeStore struct {
	inserted          []posterstore.Candidate
	communityChannels []string
	communityPattern  string
	pending           []posterstore.PendingTranslation
	recorded          []posterstore.Image
	failures          []string
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

func (f *fakeStore) ExpirePending(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeStore) Purge(context.Context, time.Time, int) (int64, error) { return 0, nil }

func (f *fakeStore) PendingTranslations(context.Context, int) ([]posterstore.PendingTranslation, error) {
	return f.pending, nil
}

func (f *fakeStore) RecordTranslation(_ context.Context, _ posterstore.PendingTranslation, translated posterstore.Image) (bool, error) {
	f.recorded = append(f.recorded, translated)

	return true, nil
}

func (f *fakeStore) RecordTranslationFailure(_ context.Context, _ posterstore.PendingTranslation, reason string) (bool, error) {
	f.failures = append(f.failures, reason)

	return true, nil
}

const sampleTable = "宙科そぴあ ⇒ 소라시나 소피아\nホロドリ配信 ⇒ 홀로도리 방송"

var sampleOriginalText = []string{"ホロドリ配信", "宙科そぴあ"}

type fakeTranslator struct {
	tables       []string
	textErr      error
	output       source
	imageErr     error
	originalText []string
	renders      [][]string
	textPrompts  []string
	imagePrompts []string
	checks       int
}

// translateText는 tables를 차례로 내고 마지막 것을 되풀이한다.
func (f *fakeTranslator) translateText(_ context.Context, _ source, prompt string) (textTranslation, error) {
	f.textPrompts = append(f.textPrompts, prompt)

	if f.textErr != nil {
		return textTranslation{}, f.textErr
	}

	table, err := parseTranslationTable(f.tables[min(len(f.textPrompts), len(f.tables))-1])

	return textTranslation{table: table, webSearchQueries: 2}, err
}

func (f *fakeTranslator) transcribe(_ context.Context, src source) ([]string, error) {
	if string(src.Data) == "original" {
		return f.originalText, nil
	}

	f.checks++

	return f.renders[min(f.checks, len(f.renders))-1], nil
}

func (f *fakeTranslator) renderImage(_ context.Context, _ source, prompt string) (source, error) {
	f.imagePrompts = append(f.imagePrompts, prompt)

	return f.output, f.imageErr
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
	require.Contains(t, translator.textPrompts[0], "이 포스터의 멤버: 宙科そぴあ (Sorashina Sopia) → 소라시나 소피아")
	require.Contains(t, translator.textPrompts[0], "熱千めら (Achichi Mela) → 아치치 메라")
	require.Equal(t, []string{imagePromptBase + sampleTable}, translator.imagePrompts, "이미지 단계는 번역표대로만 그린다")
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

	translator = &fakeTranslator{tables: []string{sampleTable}, imageErr: errors.New("image model unavailable"), originalText: sampleOriginalText}
	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Len(t, translator.textPrompts, 1, "이미지 단계 재시도는 번역표를 다시 만들지 않는다")
	require.Len(t, translator.imagePrompts, 3)
	require.Equal(t, []string{reasonTranslationFailed, reasonTranslationFailed}, store.failures)
	require.Empty(t, store.recorded)

	sources.err = errors.New("gone")
	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Equal(t, []string{reasonTranslationFailed, reasonTranslationFailed, reasonSourceUnavailable}, store.failures)
}

func TestTranslatePendingRedrawsUntilTheCheckPasses(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{
		tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: strings.Split(tsuzuriOriginal, "\n"),
		renders:      [][]string{strings.Split(tsuzuriBrokenRender, "\n"), strings.Split(tsuzuriGoodRender, "\n")},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.imagePrompts, 2)
	require.Contains(t, translator.imagePrompts[1], "이전 결과에서 대조로 찾은 문제다. 이번에는 모두 고친다:")
	require.Contains(t, translator.imagePrompts[1], "- ‘21:00’가 3번 있어야 하는데 번역본에는 1번 있다")
	require.Len(t, store.recorded, 1)
	require.Empty(t, store.failures)
}

func TestTranslatePendingNeverStoresARenderThatFailsTheCheck(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{
		tables: []string{tsuzuriTable}, output: source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: strings.Split(tsuzuriOriginal, "\n"),
		renders:      [][]string{strings.Split(tsuzuriBrokenRender, "\n")},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.imagePrompts, 3)
	require.Len(t, translator.textPrompts, 1, "번역표가 원본과 맞으면 다시 그릴 때 번역표를 다시 만들지 않는다")
	require.Empty(t, store.recorded)
	require.Equal(t, []string{reasonTranslationFailed}, store.failures)
}

func TestTranslatePendingRebuildsATableThatMissesOriginalText(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ChannelID: sophiaChannel, ImageURL: sampleImageURL, Revision: 2}}}
	translator := &fakeTranslator{
		tables:       []string{"ホロドリ配信 ⇒ 홀로도리 방송", sampleTable},
		output:       source{MIME: mimePNG, Data: pngBytes(t, 64, 36)},
		originalText: sampleOriginalText, renders: [][]string{{"소라시나 소피아", "홀로도리 방송"}},
	}
	sources := &fakeSources{original: source{MIME: mimeJPEG, Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, translator.textPrompts, 2)
	require.Contains(t, translator.textPrompts[1], "- 원본의 ‘宙科そぴあ’ 부분이 번역표에 빠졌다")
	require.Equal(t, []string{imagePromptBase + sampleTable}, translator.imagePrompts, "원본과 맞는 번역표로만 그린다")
	require.Len(t, store.recorded, 1)
}

type stalledTranslator struct{ fakeTranslator }

func (*stalledTranslator) transcribe(ctx context.Context, _ source) ([]string, error) {
	<-ctx.Done()

	return nil, ctx.Err()
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

	owner := strings.Index(prompt, "이 포스터의 멤버: 宙科そぴあ (Sorashina Sopia) → 소라시나 소피아")
	table := strings.Index(prompt, "이름 대응표 (일본어")

	require.Positive(t, owner)
	require.Greater(t, table, owner, "포스터 주인을 대응표보다 먼저 적는다")
	require.Contains(t, prompt, "\n宙科そぴあ (Sorashina Sopia) → 소라시나 소피아")
	require.Contains(t, prompt, "\n熱千めら (Achichi Mela) → 아치치 메라")
	require.NotContains(t, prompt, "졸업 멤버")
	require.NotContains(t, prompt, "타사 멤버")
	require.NotContains(t, prompt, "일본어 없음")
	require.Contains(t, prompt, "용어 대응표 (원문 → 한국어):\nコラボ → 콜라보\nホロドリ → 홀로도리")
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

	_, render := imageRequest(original, "image prompt")
	require.Empty(t, render.Tools)
	require.Equal(t, []string{"IMAGE"}, render.ResponseModalities)
	require.Equal(t, high, render.ThinkingConfig)
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
	require.Equal(t, TranslationConfig{Project: "example-project", Location: "global", Model: "gemini-nano-banana-2.1", TextModel: "gemini-4-argon"}, config.Translation)

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
