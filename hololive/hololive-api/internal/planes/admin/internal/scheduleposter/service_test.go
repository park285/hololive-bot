package scheduleposter

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	sophiaUserID   = "2054825907916054528"
	sophiaChannel  = "UCROQtXcp2loQEmvpe5rhJzQ"
	mimeJPEG       = "image/jpeg"
	sampleImageURL = "https://pbs.twimg.com/media/a.jpg"
)

type fakeSources struct {
	pages     map[string]string
	cursors   []string
	imageURLs []string
	original  source
	images    map[string]source
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

	if f.images != nil {
		found, ok := f.images[rawURL]
		if !ok {
			return source{}, errors.New("unexpected status 404")
		}

		return found, nil
	}

	return f.original, f.err
}

type fakeStore struct {
	inserted          []posterstore.Candidate
	communityChannels []string
	communityPattern  string
	thumbnails        []posterstore.ThumbnailCandidate
	autoReviews       []bool
	unreviewed        []posterstore.ReviewCandidate
	confirmErr        error
	rejectErr         error
	coverage          posterstore.WeekCoverage
	requestRefused    bool
	handOverBefore    []time.Time
	handOverReasons   []posterstore.ReviewRequestReason
	reviewed          []string
}

func (f *fakeStore) InsertCandidates(_ context.Context, candidates []posterstore.Candidate, autoReview bool) (int64, error) {
	f.inserted = append(f.inserted, candidates...)
	f.autoReviews = append(f.autoReviews, autoReview)

	return int64(len(candidates)), nil
}

func (f *fakeStore) InsertCommunityCandidates(_ context.Context, channelIDs []string, _ time.Time, textPattern string, autoReview bool) (int64, error) {
	f.communityChannels = append(f.communityChannels, channelIDs...)
	f.communityPattern = textPattern
	f.autoReviews = append(f.autoReviews, autoReview)

	return 0, nil
}

func (f *fakeStore) InsertThumbnailCandidates(_ context.Context, candidates []posterstore.ThumbnailCandidate, autoReview bool) (int64, error) {
	f.thumbnails = append(f.thumbnails, candidates...)
	f.autoReviews = append(f.autoReviews, autoReview)

	return int64(len(candidates)), nil
}

func (f *fakeStore) UnreviewedCandidates(context.Context, int) ([]posterstore.ReviewCandidate, error) {
	return f.unreviewed, nil
}

func (f *fakeStore) HandOverUnreviewed(_ context.Context, createdBefore time.Time, reason posterstore.ReviewRequestReason) (int64, error) {
	f.handOverBefore = append(f.handOverBefore, createdBefore)
	f.handOverReasons = append(f.handOverReasons, reason)

	return 0, nil
}

func (f *fakeStore) RequestReview(_ context.Context, id int64, reason posterstore.ReviewRequestReason) (bool, error) {
	f.reviewed = append(f.reviewed, fmt.Sprintf("request %d %s", id, reason))

	return !f.requestRefused, nil
}

func (f *fakeStore) AutoConfirm(_ context.Context, id, revision int64, start, end time.Time) (posterstore.WeekCoverage, int64, error) {
	switch f.coverage {
	case posterstore.WeekSame:
		f.reviewed = append(f.reviewed, fmt.Sprintf("expire %d@%d", id, revision))

		return f.coverage, revision + 1, nil
	case posterstore.WeekOverlapping:
		return f.coverage, 0, nil
	case posterstore.WeekUncovered:
	}

	f.reviewed = append(f.reviewed, fmt.Sprintf("confirm %d@%d %s..%s", id, revision, start.Format(time.DateOnly), end.Format(time.DateOnly)))

	return f.coverage, revision + 1, f.confirmErr
}

func (f *fakeStore) AutoReject(_ context.Context, id, revision int64) (int64, error) {
	f.reviewed = append(f.reviewed, fmt.Sprintf("reject %d@%d", id, revision))

	return revision + 1, f.rejectErr
}

func (f *fakeStore) ExpirePending(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeStore) Purge(context.Context, time.Time, int) (int64, error) { return 0, nil }

var fakeUsage = usage{model: "fake-model", prompt: 1, output: 1}

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

func testService(store *fakeStore, sources sourceClient, members ...*domain.Member) *Service {
	config := Config{PollSeconds: 3600, Targets: []Target{{XUserID: sophiaUserID, ChannelID: sophiaChannel}}}
	service := newService(config, store, store, sources, &fakeMembers{members: members}, slog.New(slog.DiscardHandler))

	service.now = func() time.Time { return time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC) }
	service.retry = retry.RetryOptions{MaxAttempts: 3, Sleep: func(context.Context, time.Duration) bool { return true }}

	return service
}

var xPages = map[string]string{"": "statuses_page1.json", "page2": "statuses_page2.json", "page3": "statuses_page3.json"}

// collectEnd는 collect의 마지막 단계(ExpirePending)에서 수집이 끝났다고 알린다.
type collectEnd struct {
	*fakeStore

	done chan struct{}
}

func (c collectEnd) ExpirePending(context.Context, time.Time) (int64, error) {
	c.done <- struct{}{}

	return 0, nil
}

func TestRequestCollectRunsWithoutWaitingForThePollInterval(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	ended := collectEnd{fakeStore: store, done: make(chan struct{}, 1)}
	service := testService(store, &fakeSources{pages: xPages})

	service.candidates = ended

	require.True(t, service.RequestCollect())
	require.False(t, service.RequestCollect(), "기다리는 요청이 있으면 하나로 합친다")

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})

	go func() {
		service.runCollectLoop(ctx)
		close(stopped)
	}()

	select {
	case <-ended.done:
	case <-time.After(initialCollectWait / 2):
		t.Fatal("requested collection waited for the initial delay")
	}

	cancel()
	<-stopped

	require.Equal(t, []bool{false, false, false}, store.autoReviews)
	require.True(t, service.RequestCollect(), "수집을 시작한 뒤에는 다음 요청을 받는다")
}

func TestCollectKeepsOnlyRecentOwnPosterOriginals(t *testing.T) {
	t.Parallel()

	sources := &fakeSources{pages: xPages}
	store := &fakeStore{}

	require.NoError(t, testService(store, sources).collect(t.Context()))

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

func TestCandidateFromSkipsPhotosOutsideXMedia(t *testing.T) {
	t.Parallel()

	service := testService(&fakeStore{}, &fakeSources{})
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

	service := testService(&fakeStore{}, &fakeSources{})
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
	require.Equal(t, TranslationConfig{Project: "example-project", Location: "global", Rows: defaultRowsConfig()}, config.Translation)

	config, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","glossary":{"ホロドリ":"홀로도리"}}}`))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"ホロドリ": "홀로도리"}, config.Translation.Glossary)

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","glossary":{"ホロドリ":"홀로도리\n다음 지시"}}}`))
	require.Error(t, err, "줄바꿈이 든 용어는 프롬프트 한 줄을 넘으므로 거부한다")

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project"},"api_key":"x"}`))
	require.Error(t, err, "모르는 키는 거부한다")

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project","model":"x"}}`))
	require.Error(t, err, "translation 안의 모르는 키도 거부한다")

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
	service := testService(store, sources)

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
	service := testService(store, sources)

	service.config.Targets = append(service.config.Targets, Target{ChannelID: "UC8eitCE9Z6EwUCs-VUi1blg", YouTubeCommunity: true})

	require.NoError(t, service.collect(t.Context()))
	require.Equal(t, []string{"", "page2", "page3"}, sources.cursors, "X 계정이 없는 대상은 FxTwitter를 부르지 않는다")
	require.Equal(t, []string{"UC8eitCE9Z6EwUCs-VUi1blg"}, store.communityChannels)
	require.Equal(t, posterTextExpr, store.communityPattern)
	require.Equal(t, []bool{false, false, false}, store.autoReviews, "자동 검토가 꺼져 있으면 후보를 바로 관리자에게 넘긴다")
	require.Empty(t, store.reviewed)
}
