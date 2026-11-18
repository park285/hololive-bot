package scheduleposter

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"cloud.google.com/go/auth"
	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const changeUser = "1001"

type ledgerPost struct {
	posterstore.Post

	poster  int64
	stamp   time.Time
	reason  string
	changes []posterstore.Change
}

type matchRecord struct {
	postID int64
	result posterstore.MatchResult
}

type fakeChangeStore struct {
	schedule     posterstore.PublishedSchedule
	published    bool
	now          time.Time
	posts        []*ledgerPost
	refuseMatch  bool
	channels     []string
	inserts      [][]posterstore.Post
	insertErr    error
	posters      map[string]int64
	streams      map[string]posterstore.OfficialStream
	streamErr    error
	streamCalls  [][]string
	decisions    map[int64]posterstore.PostDecision
	decisionErr  error
	matches      []matchRecord
	matchErrs    int
	matchCalls   int
	beforeRecord func(*fakeChangeStore)
	afterRecord  func(*fakeChangeStore, int64)
	onPublished  func(*fakeChangeStore, int)
	purges       []time.Time
}

func (f *fakeChangeStore) find(id int64) *ledgerPost {
	return f.findFor(f.schedule.PosterID, id)
}

func (f *fakeChangeStore) findFor(posterID, id int64) *ledgerPost {
	for _, post := range f.posts {
		if post.poster == posterID && post.ID == id {
			return post
		}
	}

	return nil
}

func (f *fakeChangeStore) posterOf(channelID string) int64 {
	return cmp.Or(f.posters[channelID], f.schedule.PosterID)
}

func (f *fakeChangeStore) OfficialStreams(_ context.Context, videoIDs []string) (map[string]posterstore.OfficialStream, error) {
	f.streamCalls = append(f.streamCalls, videoIDs)

	if f.streamErr != nil {
		return nil, f.streamErr
	}

	found := map[string]posterstore.OfficialStream{}

	for _, id := range videoIDs {
		if stream, ok := f.streams[id]; ok {
			found[id] = stream
		}
	}

	return found, nil
}

func (f *fakeChangeStore) PublishedRows(_ context.Context, channelID string, _ time.Time) (posterstore.PublishedSchedule, bool, error) {
	f.channels = append(f.channels, channelID)

	if f.onPublished != nil {
		f.onPublished(f, len(f.channels))
	}

	if !f.published {
		return posterstore.PublishedSchedule{}, false, nil
	}

	schedule := f.schedule

	schedule.PosterID = f.posterOf(channelID)
	schedule.Changes = []posterstore.Change{}

	for _, post := range f.ordered() {
		if post.poster != schedule.PosterID || post.Status != posterstore.PostMatched || !post.stamp.Equal(schedule.ExtractedAt) {
			continue
		}

		for i := range post.changes {
			if change := post.changes[i]; change.Kind != posterstore.ChangeRejected {
				change.RecordedAt = f.now
				schedule.Changes = append(schedule.Changes, change)
			}
		}
	}

	return schedule, true, nil
}

func (f *fakeChangeStore) ordered() []*ledgerPost {
	return slices.SortedFunc(slices.Values(f.posts), func(a, b *ledgerPost) int {
		return cmp.Or(a.PostedAt.Compare(b.PostedAt), cmp.Compare(a.ID, b.ID))
	})
}

func (f *fakeChangeStore) PendingPosts(_ context.Context, schedule posterstore.PublishedSchedule, limit int) ([]posterstore.Post, error) {
	var pending []posterstore.Post

	for _, post := range f.ordered() {
		if post.poster != schedule.PosterID {
			continue
		}

		rematch := (post.Status == posterstore.PostMatched || post.Status == posterstore.PostMatchFailed) && !post.stamp.Equal(schedule.ExtractedAt)
		waiting := post.Status == posterstore.PostUnjudged || !f.schedule.RowsPending && (post.Status == posterstore.PostRelevant || rematch)

		if waiting && len(pending) < limit {
			read := post.Post

			read.MatchedRowsAt = post.stamp
			pending = append(pending, read)
		}
	}

	return pending, nil
}

func (f *fakeChangeStore) InsertPosts(_ context.Context, posterID int64, posts []posterstore.Post) (int64, error) {
	if f.insertErr != nil {
		return 0, f.insertErr
	}

	f.inserts = append(f.inserts, posts)

	var inserted int64

	for _, post := range posts {
		if f.findFor(posterID, post.ID) == nil {
			post.Status, post.CreatedAt = posterstore.PostUnjudged, f.now
			f.posts = append(f.posts, &ledgerPost{Post: post, poster: posterID})
			inserted++
		}
	}

	return inserted, nil
}

func (f *fakeChangeStore) RecordDecision(_ context.Context, channelID string, postID int64, decision posterstore.PostDecision) (bool, error) {
	if f.decisionErr != nil {
		return false, f.decisionErr
	}

	post := f.findFor(f.posterOf(channelID), postID)
	if post == nil || post.Status != posterstore.PostUnjudged {
		return false, nil
	}

	if f.decisions == nil {
		f.decisions = map[int64]posterstore.PostDecision{}
	}

	f.decisions[postID] = decision
	post.Status, post.reason = decision.Status, decision.Reason

	return true, nil
}

func (f *fakeChangeStore) RecordMatch(_ context.Context, schedule posterstore.PublishedSchedule, read posterstore.Post, result posterstore.MatchResult) (bool, error) {
	postID := read.ID

	if err := posterstore.ValidateChanges(result.Changes); err != nil {
		return false, fmt.Errorf("record match: %w", err)
	}

	f.matchCalls++

	if f.matchErrs > 0 {
		f.matchErrs--

		return false, errors.New("record match: connection refused")
	}

	if f.beforeRecord != nil {
		f.beforeRecord(f)
	}

	post := f.findFor(schedule.PosterID, postID)
	if f.refuseMatch || post == nil || !slices.Contains([]posterstore.PostStatus{posterstore.PostRelevant, posterstore.PostMatched, posterstore.PostMatchFailed}, post.Status) ||
		!schedule.ExtractedAt.Equal(f.schedule.ExtractedAt) || post.stamp.Equal(schedule.ExtractedAt) ||
		post.Status != read.Status || !post.stamp.Equal(read.MatchedRowsAt) {
		return false, nil
	}

	f.matches = append(f.matches, matchRecord{postID: postID, result: result})
	post.Status, post.reason, post.stamp, post.changes = result.Status, result.Reason, schedule.ExtractedAt, result.Changes

	if slices.ContainsFunc(result.Changes, func(change posterstore.Change) bool { return change.Kind.Applied() }) {
		f.requeueAfter(post)
	}

	if f.afterRecord != nil {
		f.afterRecord(f, postID)
	}

	return true, nil
}

func (f *fakeChangeStore) requeueAfter(post *ledgerPost) {
	for _, later := range f.posts {
		after := cmp.Or(later.PostedAt.Compare(post.PostedAt), cmp.Compare(later.ID, post.ID)) > 0
		if after && later.poster == post.poster && later.stamp.Equal(post.stamp) && (later.Status == posterstore.PostMatched || later.Status == posterstore.PostMatchFailed) {
			later.Status, later.reason, later.stamp = posterstore.PostRelevant, "", time.Time{}
		}
	}
}

func (f *fakeChangeStore) PurgePosts(_ context.Context, before time.Time, _ int) (int64, error) {
	f.purges = append(f.purges, before)

	return 0, nil
}

func (f *fakeChangeStore) status(id int64) posterstore.PostStatus {
	for _, post := range f.posts {
		if post.ID == id {
			return post.Status
		}
	}

	return ""
}

func (f *fakeChangeStore) setStatus(id int64, status posterstore.PostStatus) {
	for _, post := range f.posts {
		if post.ID == id {
			post.Status = status
		}
	}
}

func (f *fakeChangeStore) seed(status posterstore.PostStatus, createdAt time.Time, post posterstore.Post) {
	post.Status, post.CreatedAt = status, createdAt
	f.posts = append(f.posts, &ledgerPost{Post: post, poster: f.schedule.PosterID})
}

type fakeClassifier struct {
	errs  map[string]error
	kinds map[string]string
	calls []string
}

func (f *fakeClassifier) classifyPost(_ context.Context, body string) (postAnswer, usage, error) {
	f.calls = append(f.calls, body)

	if err := f.errs[body]; err != nil {
		return postAnswer{}, usage{}, err
	}

	return postAnswer{ScheduleInfo: 0.9, Kind: cmp.Or(f.kinds[body], "change")}, fakeUsage, nil
}

type fakeMatcher struct {
	items   map[string][]matchItem
	errs    []error
	err     error
	prompts []string
}

func (f *fakeMatcher) matchPost(_ context.Context, prompt string) ([]matchItem, usage, error) {
	f.prompts = append(f.prompts, prompt)

	if len(f.errs) > 0 {
		err := f.errs[0]

		f.errs = f.errs[1:]

		return nil, usage{}, err
	}

	if f.err != nil {
		return nil, usage{}, f.err
	}

	for body, items := range f.items {
		if strings.HasSuffix(prompt, "\nPost text:\n"+body) {
			return items, fakeUsage, nil
		}
	}

	return []matchItem{}, fakeUsage, nil
}

type fakeChangeTranslator struct {
	out     []translatedRow
	err     error
	prompts []string
}

func (*fakeChangeTranslator) extractRows(context.Context, source, string) ([]extractedRow, usage, error) {
	return nil, usage{}, errors.New("unexpected row extraction")
}

func (f *fakeChangeTranslator) translateRows(_ context.Context, prompt string) ([]translatedRow, usage, error) {
	f.prompts = append(f.prompts, prompt)

	return f.out, fakeUsage, f.err
}

type fakeStatuses struct {
	pages   map[string]statusPage
	errs    map[string]error
	cursors []string
}

func (f *fakeStatuses) statuses(_ context.Context, userID, cursor string) (statusPage, error) {
	f.cursors = append(f.cursors, cursor)

	if userID != changeUser {
		return statusPage{}, errors.New("unexpected user")
	}

	if err := f.errs[cursor]; err != nil {
		return statusPage{}, err
	}

	return f.pages[cursor], nil
}

func (*fakeStatuses) image(context.Context, string) (source, error) {
	return source{}, errors.New("unexpected image fetch")
}

func xStatus(id int64, postedAt time.Time, text string) status {
	item := status{ID: fmt.Sprint(id), Text: text, CreatedTimestamp: postedAt.Unix()}

	item.Author.ID = changeUser

	return item
}

func statusesPage(next string, items ...status) statusPage {
	page := statusPage{Results: items}

	page.Cursor.Bottom = next

	return page
}

type changeFixture struct {
	store      *fakeChangeStore
	sources    *fakeStatuses
	classifier *fakeClassifier
	matcher    *fakeMatcher
	translator *fakeChangeTranslator
	members    *fakeMembers
	service    *Service
	clock      time.Time
}

const (
	chatDate  = "2026-11-12"
	songDate  = "2026-11-13"
	guestText = "ゲスト ぺこら"
	pekoraKo  = "페코라"
)

var (
	changeNow = at("2026-11-11 21:00")
	postA     = posterstore.Post{ID: 101, PostedAt: at("2026-11-11 10:00"), Body: "明日の雑談は22時からに変更します"}
	postB     = posterstore.Post{ID: 102, PostedAt: at("2026-11-11 12:00"), Body: "明後日の歌枠は21時から"}
	itemsA    = []matchItem{{Row: new(1), Date: chatDate, Start: new("22:00"), Status: posterstore.RowPlanned, Evidence: "22時からに変更"}}
	itemsB    = []matchItem{{Row: new(2), Date: songDate, Start: new("21:00"), Status: posterstore.RowPlanned, Evidence: "21時から"}}
)

func changeSchedule() posterstore.PublishedSchedule {
	return posterstore.PublishedSchedule{
		PosterID: 7, WeekStart: utcDate("2026-11-09"), WeekEnd: utcDate("2026-11-15"),
		PostedAt: at("2026-11-08 21:00"), ExtractedAt: at("2026-11-08 22:00"),
		Rows: []posterstore.Row{
			{Ordinal: 1, Day: utcDate(chatDate), Start: "21:00", TZ: "JST", Status: posterstore.RowPlanned, SourceText: "雑談", TitleKo: "잡담"},
			{Ordinal: 2, Day: utcDate(songDate), Start: "20:00", Status: posterstore.RowPlanned, SourceText: "歌枠", TitleKo: "노래"},
		},
	}
}

func newChangeFixture(pages ...status) *changeFixture {
	f := &changeFixture{
		store:      &fakeChangeStore{schedule: changeSchedule(), published: true, now: changeNow},
		sources:    &fakeStatuses{pages: map[string]statusPage{"": statusesPage("", pages...)}},
		classifier: &fakeClassifier{},
		matcher:    &fakeMatcher{items: map[string][]matchItem{postA.Body: itemsA, postB.Body: itemsB}},
		translator: &fakeChangeTranslator{},
		members:    &fakeMembers{members: []*domain.Member{sophia}},
		clock:      changeNow,
	}

	config := Config{
		ChangePollSeconds: 600,
		Targets:           []Target{{XUserID: changeUser, ChannelID: sophiaChannel}},
		Translation:       TranslationConfig{Rows: RowsConfig{Decide: DecideConfig{ScheduleInfoMin: 0.7}}},
	}
	service := newService(config, &fakeStore{}, &fakeStore{}, f.sources, f.members, slog.New(slog.DiscardHandler))

	service.now = func() time.Time { return f.clock }
	service.retry = retry.RetryOptions{MaxAttempts: 3, Sleep: func(context.Context, time.Duration) bool { return true }}
	service.changes, service.ledger, service.classifier, service.matcher = f.store, f.store, f.classifier, f.matcher
	service.rowModel, service.rows = f.translator, &fakeRowStore{}
	f.service = service

	return f
}

func (f *changeFixture) tick(t *testing.T) {
	t.Helper()

	require.NoError(t, f.service.detectChanges(t.Context()))
}

func (f *changeFixture) tickStoreError(t *testing.T) {
	t.Helper()

	require.Error(t, f.service.detectChanges(t.Context()), "저장소 오류로 멈춘 주기는 작업 실패다")
}

func postStatus(post posterstore.Post) status {
	return xStatus(post.ID, post.PostedAt, post.Body)
}

func matchedIDs(f *changeFixture) []int64 {
	ids := make([]int64, len(f.store.matches))
	for i, match := range f.store.matches {
		ids[i] = match.postID
	}

	return ids
}

func TestDetectChangesJudgesEachPostOnceAndMatchesOncePerBundle(t *testing.T) {
	t.Parallel()

	chat := posterstore.Post{ID: 103, PostedAt: at("2026-11-11 15:00"), Body: "今日もありがとう"}
	f := newChangeFixture(postStatus(chat), postStatus(postB), postStatus(postA))

	f.classifier.kinds = map[string]string{chat.Body: "other"}

	f.tick(t)
	f.tick(t)

	require.Equal(t, []string{postA.Body, postB.Body, chat.Body}, f.classifier.calls, "판별은 글마다 한 번이다")
	require.Len(t, f.matcher.prompts, 2, "둘째 tick은 같은 묶음에 대응한 글을 다시 부르지 않는다")
	require.Equal(t, posterstore.PostNotRelevant, f.store.decisions[chat.ID].Status)

	f.store.schedule.ExtractedAt = at("2026-11-11 20:00")
	f.tick(t)

	require.Len(t, f.classifier.calls, 3, "묶음이 바뀌어도 판별은 다시 하지 않는다")
	require.Len(t, f.matcher.prompts, 4, "묶음이 바뀌면 대응만 다시 한다")
	require.Equal(t, []int64{postA.ID, postB.ID, postA.ID, postB.ID}, matchedIDs(f))
	require.Equal(t, slices.Repeat([]time.Time{changeNow.Add(-60 * 24 * time.Hour)}, 3), f.store.purges, "60일 지난 글만 지운다")
}

func TestDetectChangesMatchesPostsInPostingOrderWithEarlierChanges(t *testing.T) {
	t.Parallel()

	repost := postStatus(postA)

	repost.RepostedBy = jsontext.Value(`{"id":"1001"}`)

	f := newChangeFixture()

	f.sources.pages = map[string]statusPage{
		"":   statusesPage("p2", repost, postStatus(postB)),
		"p2": statusesPage("p3", postStatus(postA), xStatus(90, at("2026-11-08 20:00"), "先週の配信")),
	}

	f.tick(t)

	require.Equal(t, []string{"", "p2"}, f.sources.cursors, "1페이지가 셀프 리포스트와 하한 뒤 글뿐이어도 2페이지를 읽고, 2페이지에서 멈춘다")
	require.Equal(t, []string{postA.Body, postB.Body}, f.classifier.calls)
	require.Len(t, f.matcher.prompts, 2)

	_, firstTail, _ := strings.Cut(f.matcher.prompts[0], "\nPost to process\n")
	require.NotContains(t, firstTail, "[update:")
	require.Contains(t, f.matcher.prompts[1], "\n1. 2026-11-12 22:00 JST [planned] 雑談 [update: start 21:00 -> 22:00]\n",
		"둘째 글의 대응 입력은 첫 글의 변경을 반영한다")
	require.Equal(t, []int64{postA.ID, postB.ID}, matchedIDs(f))
	require.Equal(t, posterstore.ChangeTimeChanged, f.store.matches[1].result.Changes[0].Kind)
}

func TestDetectChangesMakesNoModelCallsWithoutAMatchableSchedule(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(*fakeChangeStore){
		"no schedule":    func(store *fakeChangeStore) { store.published = false },
		"converted zone": func(store *fakeChangeStore) { store.schedule.Rows[1].SourceZone = "PDT" },
	} {
		f := newChangeFixture(postStatus(postA))

		edit(f.store)
		f.tick(t)

		require.Empty(t, f.sources.cursors, name)
		require.Empty(t, f.classifier.calls, name)
		require.Empty(t, f.matcher.prompts, name)
		require.Empty(t, f.store.inserts, name)
	}

	f := newChangeFixture(postStatus(postB))

	f.store.schedule.RowsPending = true
	f.store.seed(posterstore.PostRelevant, changeNow, postA)
	f.tick(t)

	require.Equal(t, []string{postB.Body}, f.classifier.calls, "재추출 대기 중에도 새 글은 판별한다")
	require.Empty(t, f.matcher.prompts, "재추출 대기 중에는 대응하지 않는다")
}

func TestDetectChangesStopsTheMemberWhenADecisionOutcomeIsUnknown(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA), postStatus(postB))

	f.classifier.errs = map[string]error{postA.Body: fmt.Errorf("%w: status 500", errChangeOutcomeUnknown)}
	f.tick(t)

	require.Equal(t, []string{postA.Body}, f.classifier.calls, "앞 글이 남아 있으면 뒤 글을 부르지 않는다")
	require.Empty(t, f.store.decisions)
	require.Empty(t, f.matcher.prompts)
}

func TestDetectChangesClosesPostsUnknownForSixHours(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.store.seed(posterstore.PostUnjudged, changeNow.Add(-7*time.Hour), postA)
	f.store.seed(posterstore.PostUnjudged, changeNow.Add(-7*time.Hour), postB)

	f.classifier.errs = map[string]error{postA.Body: fmt.Errorf("%w: status 500", errChangeOutcomeUnknown)}
	f.tick(t)

	require.Empty(t, f.store.decisions, "오래 기다린 글도 첫 불명 실패에는 닫지 않는다")

	f.clock = changeNow.Add(changeGiveUpAfter - time.Minute)
	f.tick(t)

	require.Empty(t, f.store.decisions, "첫 시도에서 6시간이 지나지 않았으면 닫지 않는다")

	f.clock = changeNow.Add(changeGiveUpAfter)
	f.tick(t)

	require.Equal(t, posterstore.PostDecision{Status: posterstore.PostJudgeFailed, Reason: reasonUnavailable}, f.store.decisions[postA.ID])
	require.Equal(t, []int64{postB.ID}, matchedIDs(f), "닫은 글 다음 글로 넘어간다")
	require.Equal(t, []string{postA.Body, postA.Body, postA.Body, postB.Body}, f.classifier.calls)
}

func TestDetectChangesCountsSixHoursFromTheFirstTryOnTheBundle(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.store.schedule.ExtractedAt = changeNow.Add(-72 * time.Hour)
	f.store.seed(posterstore.PostRelevant, changeNow.Add(-7*time.Hour), postA)

	f.matcher.err = errors.New("match response has no candidate")
	f.tick(t)

	require.Len(t, f.matcher.prompts, 3, "재시도 한 묶음을 쓴다")
	require.Empty(t, f.store.matches, "글과 묶음이 오래됐어도 첫 불명 실패에는 닫지 않는다")

	f.clock = changeNow.Add(changeGiveUpAfter - time.Minute)
	f.tick(t)

	require.Empty(t, f.store.matches, "첫 시도에서 6시간이 지나지 않았으면 닫지 않는다")

	f.clock = changeNow.Add(changeGiveUpAfter)
	f.tick(t)

	require.Equal(t, []matchRecord{{postID: postA.ID, result: posterstore.MatchResult{Status: posterstore.PostMatchFailed, Reason: reasonUnavailable}}},
		f.store.matches)

	f = newChangeFixture()

	f.store.schedule.ExtractedAt = changeNow.Add(-72 * time.Hour)
	f.store.seed(posterstore.PostRelevant, changeNow.Add(-7*time.Hour), postA)

	f.matcher.err = errors.New("match response has no candidate")
	f.tick(t)

	f.store.schedule.ExtractedAt = changeNow.Add(-time.Hour)
	f.clock = changeNow.Add(changeGiveUpAfter)
	f.tick(t)

	require.Empty(t, f.store.matches, "새 묶음은 그 묶음의 첫 시도부터 다시 센다")
}

func TestDetectChangesClosesStoreFailuresOnlyAfterSixHours(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.store.matchErrs = 1
	f.store.seed(posterstore.PostRelevant, changeNow.Add(-time.Hour), postA)
	f.tickStoreError(t)

	require.Empty(t, f.store.matches, "기준 전에는 저장소 오류를 기록하지 않고 멈춘다")

	f = newChangeFixture()

	f.store.schedule.ExtractedAt = changeNow.Add(-7 * time.Hour)
	f.store.matchErrs = 2
	f.store.seed(posterstore.PostRelevant, changeNow.Add(-7*time.Hour), postA)
	f.tickStoreError(t)

	require.Empty(t, f.store.matches, "첫 시도의 저장소 오류는 글과 묶음이 오래됐어도 닫지 않는다")

	f.clock = changeNow.Add(changeGiveUpAfter)
	f.tick(t)

	require.Len(t, f.matcher.prompts, 1)
	require.Equal(t, []matchRecord{{postID: postA.ID, result: posterstore.MatchResult{Status: posterstore.PostMatchFailed, Reason: reasonUnavailable}}},
		f.store.matches, "기준이 지나면 저장소 오류도 unavailable로 닫는다")
}

func TestDetectChangesRetriesOnlyTheRecordWhileTheStoreFails(t *testing.T) {
	t.Parallel()

	guest := posterstore.Post{ID: 104, PostedAt: at("2026-11-11 14:00"), Body: "明後日の歌枠、ゲストはぺこら"}
	f := newChangeFixture(postStatus(guest))

	f.matcher.items[guest.Body] = []matchItem{{Row: new(2), Date: songDate, Status: posterstore.RowPlanned, Text: guestText, Evidence: "ゲストはぺこら"}}
	f.translator.out = []translatedRow{{Index: 1, TitleKo: "노래", WithKo: []string{pekoraKo}}}
	f.store.matchErrs = 2

	f.tickStoreError(t)
	f.tickStoreError(t)
	require.Empty(t, f.store.matches)

	f.tick(t)

	require.Len(t, f.matcher.prompts, 1, "기록만 다시 하고 모델은 다시 부르지 않는다")
	require.Len(t, f.translator.prompts, 1)
	require.Equal(t, 3, f.store.matchCalls)
	require.Len(t, f.store.matches, 1)
	require.Equal(t, "노래", f.store.matches[0].result.Changes[0].TitleKo)

	f = newChangeFixture(postStatus(postA))

	f.store.matchErrs = 1
	f.tickStoreError(t)

	f.store.schedule.ExtractedAt = at("2026-11-11 20:00")
	f.tick(t)

	require.Len(t, f.matcher.prompts, 2, "묶음이 바뀌면 다시 계산한다")
	require.Len(t, f.store.matches, 1)
}

func TestDetectChangesRematchesLaterPostsAfterAnEarlierPostApplies(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postB))

	f.tick(t)
	f.store.seed(posterstore.PostRelevant, changeNow, postA)
	f.tick(t)
	f.tick(t)
	f.tick(t)

	require.Equal(t, []int64{postB.ID, postA.ID, postB.ID}, matchedIDs(f), "앞선 글이 반영되면 뒤 글을 한 번 다시 대응하고 되풀이하지 않는다")
	require.Contains(t, f.matcher.prompts[2], "\n1. 2026-11-12 22:00 JST [planned] 雑談 [update: start 21:00 -> 22:00]\n",
		"다시 대응한 입력은 앞선 글을 반영한 상태다")
	require.Equal(t, posterstore.PostMatched, f.store.status(postB.ID))

	f = newChangeFixture(postStatus(postB))

	f.matcher.items[postA.Body] = nil
	f.tick(t)
	f.store.seed(posterstore.PostRelevant, changeNow, postA)
	f.tick(t)
	f.tick(t)

	require.Equal(t, []int64{postB.ID, postA.ID}, matchedIDs(f), "앞선 글에 반영 변경이 없으면 뒤 글은 그대로다")
}

func TestDetectChangesDoesNotReuseAKeptMatchAfterEarlierChangesAreReplaced(t *testing.T) {
	t.Parallel()

	middle := posterstore.Post{ID: 103, PostedAt: at("2026-11-11 11:00"), Body: "明後日の歌枠は23時から", Status: posterstore.PostMatched}
	f := newChangeFixture()

	f.store.posts = append(f.store.posts, &ledgerPost{Post: middle, poster: f.store.schedule.PosterID, stamp: f.store.schedule.ExtractedAt, changes: []posterstore.Change{{
		PostID: middle.ID, Item: 1, PostedAt: middle.PostedAt, Kind: posterstore.ChangeTimeChanged, Slot: 2, Start: new("23:00"),
	}}})
	f.store.seed(posterstore.PostRelevant, changeNow, postB)

	f.store.matchErrs = 1
	f.tickStoreError(t)

	require.Len(t, f.matcher.prompts, 1)
	require.Contains(t, f.matcher.prompts[0], "[update: start 20:00 -> 23:00]")

	f.store.seed(posterstore.PostRelevant, changeNow, postA)
	f.tick(t)

	require.Equal(t, []int64{postA.ID, middle.ID, postB.ID}, matchedIDs(f), "대기열로 돌아간 가운데 글을 뒤 글보다 먼저 대응한다")
	require.Len(t, f.matcher.prompts, 4, "앞선 변경의 수가 같아도 내용이 바뀌었으면 보관한 결과를 쓰지 않는다")
	require.NotContains(t, f.matcher.prompts[3], "[update: start 20:00 -> 23:00]")
	require.Contains(t, f.matcher.prompts[3], "[update: start 21:00 -> 22:00]")
}

func TestDetectChangesMatchesRequeuedPostsBeforeLaterOnesInTheSameTick(t *testing.T) {
	t.Parallel()

	middle := posterstore.Post{ID: 103, PostedAt: at("2026-11-11 11:00"), Body: "明後日の歌枠は23時から", Status: posterstore.PostMatched}
	f := newChangeFixture()

	f.matcher.items[middle.Body] = []matchItem{{Row: new(2), Date: songDate, Start: new("23:00"), Status: posterstore.RowPlanned, Evidence: "23時から"}}
	f.store.posts = append(f.store.posts, &ledgerPost{Post: middle, poster: f.store.schedule.PosterID, stamp: f.store.schedule.ExtractedAt, changes: []posterstore.Change{{
		PostID: middle.ID, Item: 1, PostedAt: middle.PostedAt, Kind: posterstore.ChangeTimeChanged, Slot: 2, Start: new("23:00"),
	}}})
	f.store.seed(posterstore.PostRelevant, changeNow, postA)
	f.store.seed(posterstore.PostRelevant, changeNow, postB)
	f.tick(t)

	require.Equal(t, []int64{postA.ID, middle.ID, postB.ID}, matchedIDs(f))
	require.Contains(t, f.matcher.prompts[2], "[update: start 21:00 -> 22:00]")
	require.Contains(t, f.matcher.prompts[2], "[update: start 20:00 -> 23:00]", "뒤 글의 입력은 돌아간 글을 다시 대응한 변경을 담는다")

	f.tick(t)

	require.Len(t, f.matcher.prompts, 3, "한 주기 안에 수렴해 다음 주기에 다시 대응하지 않는다")
}

func TestDetectChangesNeverRevisitsAPostWithinATick(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.store.afterRecord = func(store *fakeChangeStore, postID int64) {
		post := store.find(postID)

		post.Status, post.stamp = posterstore.PostRelevant, time.Time{}
	}
	f.store.seed(posterstore.PostRelevant, changeNow, postA)
	f.store.seed(posterstore.PostRelevant, changeNow, postB)
	f.tick(t)

	require.Equal(t, []int64{postA.ID, postB.ID}, matchedIDs(f), "다시 읽은 대기 목록에서는 방금 기록한 글보다 뒤인 글만 이어 처리한다")
}

func TestDetectChangesStopsTheTickWhenVertexFailsForEveryPost(t *testing.T) {
	t.Parallel()

	for name, tc := range vertexFailures() {
		f := newChangeFixture()

		f.service.config.Targets = append(f.service.config.Targets, Target{XUserID: changeUser, ChannelID: "UCsecond"})
		f.store.schedule.ExtractedAt = changeNow.Add(-7 * time.Hour)
		f.matcher.err = tc.err
		f.store.seed(posterstore.PostRelevant, changeNow.Add(-7*time.Hour), postA)
		f.tick(t)

		f.clock = changeNow.Add(changeGiveUpAfter)
		f.tick(t)

		if !tc.service {
			require.Equal(t, []matchRecord{{postID: postA.ID, result: posterstore.MatchResult{Status: posterstore.PostMatchFailed, Reason: reasonUnavailable}}},
				f.store.matches, "%s: 알 수 없는 실패는 첫 시도에서 6시간 뒤 닫는다", name)

			continue
		}

		require.Len(t, f.matcher.prompts, 2*tc.calls, name)
		require.Empty(t, f.store.matches, "%s: 서비스 실패는 6시간이 지나도 기록하지 않는다", name)
		require.NotContains(t, f.store.channels, "UCsecond", "%s: 남은 대상을 보지 않는다", name)
	}
}

type vertexFailure struct {
	err     error
	calls   int
	service bool
}

func vertexFailures() map[string]vertexFailure {
	dial := &url.Error{Op: http.MethodPost, URL: "https://vertex.invalid", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}
	lookup := &url.Error{Op: http.MethodPost, URL: "https://vertex.invalid", Err: &net.DNSError{Err: "no such host", Name: "vertex.invalid"}}
	token := &url.Error{Op: http.MethodPost, URL: "https://vertex.invalid", Err: &auth.Error{
		Response: &http.Response{StatusCode: http.StatusBadRequest}, Body: []byte(`{"error":"invalid_grant"}`),
	}}

	return map[string]vertexFailure{
		"dial":  {dial, 1, true},
		"dns":   {lookup, 1, true},
		"token": {token, 1, true},
		"400":   {genai.APIError{Code: http.StatusBadRequest}, 1, true},
		"401":   {genai.APIError{Code: http.StatusUnauthorized}, 1, true},
		"403":   {genai.APIError{Code: http.StatusForbidden}, 1, true},
		"404":   {genai.APIError{Code: http.StatusNotFound}, 1, true},
		"429":   {genai.APIError{Code: http.StatusTooManyRequests}, 3, true},
		"503":   {genai.APIError{Code: http.StatusServiceUnavailable}, 3, true},
		"500":   {genai.APIError{Code: http.StatusInternalServerError}, 3, false},
	}
}

func TestDetectChangesStopsTheTickWhenTranslationHitsAServiceFailure(t *testing.T) {
	t.Parallel()

	guest := posterstore.Post{ID: 104, PostedAt: at("2026-11-11 14:00"), Body: "明後日の歌枠、ゲストはぺこら"}

	for name, tc := range vertexFailures() {
		if !tc.service {
			continue
		}

		f := newChangeFixture()

		f.service.config.Targets = append(f.service.config.Targets, Target{XUserID: changeUser, ChannelID: "UCsecond"})
		f.store.schedule.ExtractedAt = changeNow.Add(-7 * time.Hour)
		f.matcher.items[guest.Body] = []matchItem{{Row: new(2), Date: songDate, Status: posterstore.RowPlanned, Text: guestText, Evidence: "ゲストはぺこら"}}
		f.translator.err = fmt.Errorf("translate poster rows: %w", tc.err)
		f.store.seed(posterstore.PostRelevant, changeNow.Add(-7*time.Hour), guest)
		f.tick(t)

		f.clock = changeNow.Add(changeGiveUpAfter)
		f.tick(t)

		require.Len(t, f.translator.prompts, 2*tc.calls, name)
		require.Empty(t, f.store.matches, "%s: 한국어 칸을 비운 채 굳히지 않고 6시간이 지나도 닫지 않는다", name)
		require.NotContains(t, f.store.channels, "UCsecond", name)

		f.translator.err = nil
		f.translator.out = []translatedRow{{Index: 1, TitleKo: "노래", WithKo: []string{pekoraKo}}}
		f.tick(t)

		require.Len(t, f.matcher.prompts, 1, "%s: 판정이 끝난 대응은 재번역부터 다시 한다", name)
		require.Len(t, f.store.matches, 1, name)
		require.Equal(t, "노래", f.store.matches[0].result.Changes[0].TitleKo, name)
	}
}

func TestDetectChangesDoesNotOverwriteARevokedPost(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.store.beforeRecord = func(store *fakeChangeStore) { store.setStatus(postB.ID, posterstore.PostRevoked) }
	f.store.seed(posterstore.PostRevoked, changeNow, postA)
	f.store.seed(posterstore.PostRelevant, changeNow, postB)
	f.tick(t)
	f.tick(t)

	require.Empty(t, f.classifier.calls)
	require.Len(t, f.matcher.prompts, 1, "철회된 글은 다시 대응하지 않는다")
	require.Empty(t, f.store.matches, "대응하는 동안 철회된 글은 덮어쓰지 않는다")
	require.Equal(t, posterstore.PostRevoked, f.store.status(postA.ID))
	require.Equal(t, posterstore.PostRevoked, f.store.status(postB.ID))
}

func TestDetectChangesStopsTheMemberWhenTheBundleChangesMidTick(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA))

	f.store.onPublished = func(store *fakeChangeStore, calls int) {
		if calls == 2 {
			store.schedule.ExtractedAt = at("2026-11-11 20:00")
		}
	}
	f.tick(t)

	require.Equal(t, []string{postA.Body}, f.classifier.calls)
	require.Empty(t, f.matcher.prompts, "묶음이 바뀌면 옛 묶음으로 대응하지 않는다")
}

func TestDetectChangesHandlesAtMostOneBatchPerTick(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	for i := range changeBatch + 1 {
		f.store.seed(posterstore.PostUnjudged, changeNow, posterstore.Post{ID: int64(500 + i), PostedAt: at("2026-11-10 12:00").Add(time.Duration(i) * time.Minute), Body: fmt.Sprint("雑談", i)})
	}

	f.tick(t)

	require.Len(t, f.classifier.calls, 20)
	require.Equal(t, "雑談0", f.classifier.calls[0], "게시 순서로 처리한다")
}

func TestDetectChangesUsesTheConfiguredScheduleInfoMinimum(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA))

	f.service.config.Translation.Rows.Decide.ScheduleInfoMin = 0.95
	f.tick(t)

	require.Equal(t, posterstore.PostNotRelevant, f.store.decisions[postA.ID].Status)
	require.Empty(t, f.matcher.prompts)
}

func TestDetectChangesInsertsNothingWhenAPageFails(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.sources.pages = map[string]statusPage{"": statusesPage("p2", postStatus(postB))}
	f.sources.errs = map[string]error{"p2": errors.New("unexpected status 503")}
	f.store.seed(posterstore.PostUnjudged, changeNow, postA)
	f.tick(t)

	require.Empty(t, f.store.inserts, "페이지 하나라도 실패하면 그 tick의 수집분은 넣지 않는다")
	require.Equal(t, []string{postA.Body}, f.classifier.calls, "저장된 글은 처리한다")
	require.Equal(t, []int64{postA.ID}, matchedIDs(f))
}

func TestDetectChangesStopsTheMemberWhenAMatchIsNotRecorded(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA), postStatus(postB))

	f.store.refuseMatch = true
	f.tick(t)

	require.Len(t, f.matcher.prompts, 1)
	require.Equal(t, []string{postA.Body}, f.classifier.calls)
}

func TestDetectChangesStopsTheTickOnServiceFailure(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.service.config.Targets = append(f.service.config.Targets, Target{XUserID: changeUser, ChannelID: "UCsecond"})
	f.classifier.errs = map[string]error{postA.Body: fmt.Errorf("%w: status 429", errChangeServiceFailed)}
	f.store.seed(posterstore.PostUnjudged, changeNow.Add(-7*time.Hour), postA)
	f.store.seed(posterstore.PostUnjudged, changeNow.Add(-7*time.Hour), postB)
	f.tick(t)

	require.Equal(t, []string{sophiaChannel}, f.store.channels, "남은 대상을 보지 않는다")
	require.Empty(t, f.store.decisions, "서비스 실패는 6시간이 지나도 기록하지 않는다")
	require.Len(t, f.store.purges, 1)
}

func TestDetectChangesClosesPostFaultsAtOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err    error
		calls  int
		reason string
	}{
		{fmt.Errorf("%w: match response has 21 items", errBadAnswer), 3, reasonBadAnswer},
		{fmt.Errorf("%w: SAFETY", errMatchBlocked), 1, reasonBlocked},
	} {
		f := newChangeFixture(postStatus(postA))

		f.matcher.err = tc.err
		f.tick(t)

		require.Len(t, f.matcher.prompts, tc.calls, tc.reason)
		require.Equal(t, []matchRecord{{postID: postA.ID, result: posterstore.MatchResult{Status: posterstore.PostMatchFailed, Reason: tc.reason}}},
			f.store.matches, tc.reason)
	}
}

func TestDetectChangesClosesBadAnswersOnlyWhenEveryAttemptWasMalformed(t *testing.T) {
	t.Parallel()

	unknown := genai.APIError{Code: http.StatusInternalServerError}
	malformed := fmt.Errorf("%w: match response has 21 items", errBadAnswer)

	for name, errs := range map[string][]error{
		"malformed last":  {unknown, unknown, malformed},
		"malformed first": {malformed, malformed, unknown},
	} {
		f := newChangeFixture(postStatus(postA))

		f.matcher.errs = errs
		f.tick(t)

		require.Len(t, f.matcher.prompts, 3, name)
		require.Empty(t, f.store.matches, "%s: 다른 실패가 섞인 재시도 묶음은 불명 실패라 닫지 않는다", name)
	}
}

func TestDetectChangesTranslatesTheChangedSlot(t *testing.T) {
	t.Parallel()

	guest := posterstore.Post{ID: 104, PostedAt: at("2026-11-11 14:00"), Body: "明後日の歌枠、ゲストはぺこら"}
	f := newChangeFixture(postStatus(postA), postStatus(guest))

	f.matcher.items[guest.Body] = []matchItem{{Row: new(2), Date: songDate, Status: posterstore.RowPlanned, Text: guestText, Evidence: "ゲストはぺこら"}}
	f.translator.out = []translatedRow{{Index: 1, TitleKo: "노래", WithKo: []string{pekoraKo}}}
	f.tick(t)

	require.Len(t, f.translator.prompts, 1, "text가 있는 반영 항목이 있는 글만 번역한다")
	require.True(t, strings.HasSuffix(f.translator.prompts[0], "\n\nRows:\n1. 歌枠 [update: ゲスト ぺこら]"), f.translator.prompts[0])
	require.Contains(t, f.translator.prompts[0], "\nPoster time zone: JST\n", "시간대는 포스터 전체 행에서 정한다")

	recorded := f.store.matches[1].result.Changes
	require.Equal(t, posterstore.ChangeContentChanged, recorded[0].Kind)
	require.Equal(t, "노래", recorded[0].TitleKo)
	require.Equal(t, []string{pekoraKo}, recorded[0].WithKo)
}

func TestDetectChangesTranslatesOnlyTheItemsWithText(t *testing.T) {
	t.Parallel()

	guest := posterstore.Post{ID: 104, PostedAt: at("2026-11-11 14:00"), Body: "明日の雑談は22時から、明後日の歌枠のゲストはぺこら"}
	f := newChangeFixture(postStatus(guest))

	f.matcher.items[guest.Body] = []matchItem{
		{Row: new(1), Date: chatDate, Start: new("22:00"), Status: posterstore.RowPlanned, Evidence: "22時から"},
		{Row: new(2), Date: songDate, Status: posterstore.RowPlanned, Text: guestText, Evidence: "ゲストはぺこら"},
	}
	f.translator.out = []translatedRow{{Index: 1, TitleKo: "노래", WithKo: []string{pekoraKo}, Terms: []posterstore.Term{
		{Source: "ぺこら", Ko: pekoraKo, Basis: basisTranslated}, {Source: "ぺこーら", Ko: pekoraKo, Basis: basisTranslated},
	}}}
	f.tick(t)

	require.True(t, strings.HasSuffix(f.translator.prompts[0], "\n\nRows:\n1. 歌枠 [update: ゲスト ぺこら]"), f.translator.prompts[0])

	recorded := f.store.matches[0].result.Changes
	require.Equal(t, posterstore.ChangeTimeChanged, recorded[0].Kind)
	require.Empty(t, recorded[0].TitleKo)
	require.Equal(t, "노래", recorded[1].TitleKo)
	require.Equal(t, []posterstore.Term{{Source: "ぺこら", Ko: pekoraKo, Basis: basisTranslated}}, recorded[1].Terms, "원문에 없는 표기는 저장하지 않는다")
	require.Contains(t, recorded[1].Warnings, "T8 terms 원문 불일치: ぺこーら")
}

func TestTranslationLinesRebuildTheChangedSlots(t *testing.T) {
	t.Parallel()

	rows := []posterstore.Row{
		{Ordinal: 1, Day: utcDate(songDate), Start: "19:00", Status: posterstore.RowPlanned, SourceText: "A", Badges: []string{"メン限"}},
		{Ordinal: 2, Day: utcDate(songDate), Start: "21:00", Status: posterstore.RowPlanned, SourceText: "B"},
		{Ordinal: 3, Day: utcDate("2026-11-14"), Status: posterstore.RowOff, SourceText: "お休み", Badges: []string{"告知"}},
		{Ordinal: 4, Day: utcDate("2026-11-15"), Start: "12:00", Status: posterstore.RowCancelled, SourceText: "D"},
		{Ordinal: 5, Day: utcDate("2026-11-15"), Start: "20:00", Status: posterstore.RowPlanned, SourceText: "E"},
	}
	earlier := func(item int, kind posterstore.ChangeKind, edit func(*posterstore.Change)) posterstore.Change {
		c := posterstore.Change{PostID: 3, Item: item, PostedAt: at("2026-11-12 10:00"), Kind: kind}
		edit(&c)

		return c
	}
	prior := []posterstore.Change{
		earlier(1, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text = 2, "t1" }),
		earlier(2, posterstore.ChangeAdded, func(c *posterstore.Change) {
			c.Day, c.Start, c.Status, c.Text = utcDate("2026-11-14"), new("18:00"), posterstore.RowPlanned, "ゲリラ"
		}),
	}
	changes := []posterstore.Change{
		change(1, posterstore.ChangeTimeChanged, func(c *posterstore.Change) { c.Slot, c.Start = 5, new("21:00") }),
		change(2, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.Slot, c.Text = 2, "t2" }),
		change(3, posterstore.ChangeAdded, func(c *posterstore.Change) {
			c.Slot, c.Start, c.Status, c.Text = 3, new("18:30"), posterstore.RowPlanned, "ゲリラ雑談"
		}),
		change(4, posterstore.ChangeConfirmed, func(c *posterstore.Change) { c.Slot, c.Status, c.Text = 4, posterstore.RowPlanned, "復活" }),
		change(5, posterstore.ChangeContentChanged, func(c *posterstore.Change) { c.TargetPostID, c.TargetItem, c.Text = 3, 2, "ゲスト" }),
		change(6, posterstore.ChangeCancelled, func(c *posterstore.Change) { c.Slot, c.Status, c.Text = 1, posterstore.RowCancelled, "体調不良" }),
	}

	lines := translationLines(rows, prior, changes)

	got := make([]string, len(lines))
	targets := make([]int, len(lines))

	for i := range lines {
		got[i], targets[i] = rowLine(i+1, lines[i].row), lines[i].change
	}

	require.Equal(t, []string{
		"1. B [update: t1] [update: t2]",
		"2. ゲリラ雑談",
		"3. D [update: 復活]",
		"4. ゲリラ [update: ゲスト]",
		"5. A [badges: メン限] [" + string(posterstore.RowCancelled) + "] [update: 体調不良]",
	}, got)
	require.Equal(t, []int{1, 2, 3, 4, 5}, targets, "번역 결과는 줄 순서대로 그 줄의 변경에 붙는다")
}

func TestDetectChangesRecordsTimesWhenTranslationFails(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA))

	f.matcher.items[postA.Body] = []matchItem{{
		Row: new(1), Date: chatDate, Start: new("22:00"), Status: posterstore.RowPlanned, Text: guestText, Evidence: "22時からに変更",
	}}
	f.translator.err = errors.New("translate poster rows: unexpected EOF")
	f.tick(t)

	require.Len(t, f.translator.prompts, 3)
	require.Len(t, f.store.matches, 1)

	recorded := f.store.matches[0].result.Changes[0]
	require.Equal(t, posterstore.ChangeTimeChanged, recorded.Kind)
	require.Equal(t, new("22:00"), recorded.Start)
	require.Empty(t, recorded.TitleKo, "번역이 실패하면 한국어 칸만 비운다")
}

func TestDetectChangesStopsMatchingWithoutMembers(t *testing.T) {
	t.Parallel()

	f := newChangeFixture(postStatus(postA))

	f.members.err = errors.New("members unavailable")
	f.tickStoreError(t)

	require.Equal(t, []string{postA.Body}, f.classifier.calls)
	require.Empty(t, f.matcher.prompts)
	require.Equal(t, 1, f.members.calls)
}

func TestCollectPostsKeepsCleanPostsFromTheWeek(t *testing.T) {
	t.Parallel()

	other := xStatus(201, at("2026-11-10 12:00"), "他人の告知")

	other.Author.ID = "2002"

	f := newChangeFixture()

	f.sources.pages = map[string]statusPage{"": statusesPage("p2",
		xStatus(202, at("2026-11-10 12:00"), "  配信\x00告知\xff\n21時〜"),
		other,
		xStatus(202, at("2026-11-10 12:00"), "重複"),
		xStatus(203, at("2026-11-10 13:00"), " \t\n"),
		xStatus(204, at("2026-11-10 14:00"), strings.Repeat("長", posterstore.MaxPostChars+5)),
		xStatus(205, at("2026-11-08 23:30"), "主の週の前日に書いた予定"),
		xStatus(206, at("2026-11-08 20:00"), "ポスター前の告知"),
	)}

	got, err := f.service.collectPosts(t.Context(), f.service.config.Targets[0], new(changeSchedule()))
	require.NoError(t, err)
	require.Equal(t, []string{""}, f.sources.cursors, "마지막 본인 원글이 수집 하한보다 이르면 다음 페이지를 읽지 않는다")
	require.True(t, got.reached)
	require.Equal(t, []posterstore.Post{
		{ID: 202, PostedAt: time.Unix(at("2026-11-10 12:00").Unix(), 0), Body: "  配信 告知\n21時〜"},
		{ID: 204, PostedAt: time.Unix(at("2026-11-10 14:00").Unix(), 0), Body: strings.Repeat("長", posterstore.MaxPostChars)},
	}, got.posts)
}

func quotedPost(authorID, name string, postedAt time.Time, text string) jsontext.Value {
	return jsontext.Value(fmt.Sprintf(`{"type":"status","text":%q,"created_timestamp":%d,"author":{"id":%q,"name":%q}}`,
		text, postedAt.Unix(), authorID, name))
}

func TestCollectPostsAppendsAnotherAccountsQuotedPost(t *testing.T) {
	t.Parallel()

	host := xStatus(211, at("2026-11-10 12:30"), "明日のコラボはこちら！")

	host.Quote = quotedPost("3003", " 霧島なぎ\n🌙 ", at("2026-11-10 12:00"), "📢明日20時から\t\n#クイズ王決定戦")

	self := xStatus(212, at("2026-11-10 13:00"), "配信開始！")

	self.Quote = quotedPost(changeUser, "本人", at("2026-11-10 09:00"), "このあと13時から雑談")

	gone := xStatus(213, at("2026-11-10 14:00"), "これ見て")

	gone.Quote = jsontext.Value(`{"type":"tombstone"}`)

	full := xStatus(214, at("2026-11-10 15:00"), strings.Repeat("長", posterstore.MaxPostChars-10))

	full.Quote = quotedPost("3003", "霧島なぎ", at("2026-11-10 12:00"), "入らない引用")

	cut := xStatus(215, at("2026-11-10 16:00"), strings.Repeat("長", posterstore.MaxPostChars-50))

	cut.Quote = quotedPost("3003", "N", at("2026-11-10 12:00"), strings.Repeat("引", 100))

	f := newChangeFixture(host, self, gone, full, cut)

	got, err := f.service.collectPosts(t.Context(), f.service.config.Targets[0], new(changeSchedule()))
	require.NoError(t, err)

	bodies := make([]string, len(got.posts))
	for i := range got.posts {
		bodies[i] = got.posts[i].Body
	}

	header := "\n\nQuoted post by N at 2026-11-10 12:00 JST:\n"

	require.Equal(t, []string{
		"明日のコラボはこちら！\n\nQuoted post by 霧島なぎ 🌙 at 2026-11-10 12:00 JST:\n📢明日20時から \n#クイズ王決定戦",
		"配信開始！",
		"これ見て",
		strings.Repeat("長", posterstore.MaxPostChars-10),
		strings.Repeat("長", posterstore.MaxPostChars-50) + header + strings.Repeat("引", 50-utf8.RuneCountInString(header)),
	}, bodies, "본인 글·지워진 글 인용은 붙이지 않고, 붙인 본문도 글 상한을 넘지 않는다")
}

func TestCollectPostsStopsOnlyAtTheOwnersOriginalPosts(t *testing.T) {
	t.Parallel()

	oldRepost := xStatus(301, at("2026-11-05 12:00"), "他人の先週の告知")

	oldRepost.Author.ID = "2002"
	oldRepost.RepostedBy = jsontext.Value(`{"id":"1001"}`)

	selfRepost := xStatus(302, at("2026-11-10 09:00"), "自分の告知の再投稿")

	selfRepost.RepostedBy = jsontext.Value(`{"id":"1001"}`)

	f := newChangeFixture()

	f.sources.pages = map[string]statusPage{
		"":   statusesPage("p2", postStatus(postB), oldRepost),
		"p2": statusesPage("p3", selfRepost),
		"p3": statusesPage("", postStatus(postA)),
	}

	got, err := f.service.collectPosts(t.Context(), f.service.config.Targets[0], new(changeSchedule()))
	require.NoError(t, err)
	require.Equal(t, []string{"", "p2", "p3"}, f.sources.cursors, "리포스트로 끝나거나 본인 원글이 없는 페이지 뒤도 읽는다")
	require.True(t, got.reached, "커서가 없으면 끝까지 읽은 것이다")
	require.Equal(t, []int64{postB.ID, postA.ID}, []int64{got.posts[0].ID, got.posts[1].ID})
	require.Len(t, got.posts, 2, "셀프 리포스트는 넣지 않는다")
}

func deepPages() map[string]statusPage {
	return map[string]statusPage{
		"":   statusesPage("p2", postStatus(postB)),
		"p2": statusesPage("p3", postStatus(postA)),
		"p3": statusesPage("p4", xStatus(90, at("2026-11-08 20:00"), "先週の配信")),
	}
}

func TestDetectChangesReadsDeepOnlyUntilTheCollectionIsStored(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.sources.pages = deepPages()
	f.tick(t)

	require.Equal(t, []string{"", "p2", "p3"}, f.sources.cursors, "처음에는 수집 하한까지 읽는다")

	f.tick(t)

	require.Equal(t, []string{"", "p2", "p3", ""}, f.sources.cursors, "저장한 뒤에는 1쪽에서 멈춘다")

	f = newChangeFixture()

	f.sources.pages = deepPages()
	f.store.insertErr = errors.New("insert posts: connection refused")
	require.Error(t, f.service.detectChanges(t.Context()))

	f.store.insertErr = nil
	f.tick(t)

	require.Equal(t, []string{"", "p2", "p3", "", "p2", "p3"}, f.sources.cursors, "저장이 실패한 주기는 기억하지 않는다")
}

func TestDetectChangesReadsDeepAgainWhenThePageLimitIsHit(t *testing.T) {
	t.Parallel()

	f := newChangeFixture()

	f.sources.pages = map[string]statusPage{}

	for i := range maxChangePages {
		item := xStatus(int64(600+i), at("2026-11-11 12:00").Add(-time.Duration(i)*time.Minute), fmt.Sprint("告知", i))

		f.sources.pages[fmt.Sprint("p", i)] = statusesPage(fmt.Sprint("p", i+1), item)
	}

	f.sources.pages[""] = f.sources.pages["p0"]

	f.tick(t)
	require.Len(t, f.sources.cursors, maxChangePages)

	f.tick(t)
	require.Len(t, f.sources.cursors, 2*maxChangePages, "하한에 닿지 못한 수집은 기억하지 않는다")
}

func TestStartRunsChangeDetectionOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		seconds    int
		classifier postClassifier
		want       bool
	}{
		{"enabled", 600, &fakeClassifier{}, true},
		{"poll off", 0, &fakeClassifier{}, false},
		{"no judge key", 600, nil, false},
	} {
		f := newChangeFixture()
		logs := &recordingHandler{}

		f.service.config.ChangePollSeconds = tc.seconds
		f.service.classifier = tc.classifier
		f.service.logger = slog.New(logs)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		f.service.Start(ctx)

		require.Equal(t, tc.want, logs.attr("Starting schedule poster service", "change_detection").Bool(), tc.name)
		require.Equal(t, tc.want, f.service.detectsChanges(), tc.name)
	}
}

type recordingHandler struct {
	records []slog.Record
}

func (*recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())

	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

func (h *recordingHandler) attr(message, key string) slog.Value {
	for i := range h.records {
		record := &h.records[i]
		if record.Message != message {
			continue
		}

		var found slog.Value

		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == key {
				found = attr.Value
			}

			return true
		})

		return found
	}

	return slog.Value{}
}

func TestDetectChangesCountsPostsAndCycles(t *testing.T) {
	counts := func() []float64 {
		return []float64{
			testutil.ToFloat64(postsTotal.WithLabelValues(string(posterstore.PostNotRelevant))),
			testutil.ToFloat64(postsTotal.WithLabelValues(string(posterstore.PostRelevant))),
			testutil.ToFloat64(postsTotal.WithLabelValues(string(posterstore.PostMatched))),
			testutil.ToFloat64(changeCyclesTotal.WithLabelValues(changeCycleCompleted)),
			testutil.ToFloat64(changeCyclesTotal.WithLabelValues(changeCyclePaused)),
			testutil.ToFloat64(changeCyclesTotal.WithLabelValues(changeCycleStoreError)),
		}
	}
	delta := func(before []float64) []float64 {
		after := counts()
		for i := range after {
			after[i] -= before[i]
		}

		return after
	}

	chat := posterstore.Post{ID: 103, PostedAt: at("2026-11-11 15:00"), Body: "今日もありがとう"}
	f := newChangeFixture(postStatus(chat), postStatus(postA))

	f.classifier.kinds = map[string]string{chat.Body: "other"}

	before := counts()

	f.tick(t)

	require.Equal(t, []float64{1, 1, 1, 1, 0, 0}, delta(before))
	require.InDelta(t, float64(changeNow.Unix()), testutil.ToFloat64(changeLastCompleted), 0)

	f = newChangeFixture(postStatus(postA))
	f.classifier.errs = map[string]error{postA.Body: fmt.Errorf("%w: status 429", errChangeServiceFailed)}
	before = counts()

	f.tick(t)

	require.Equal(t, []float64{0, 0, 0, 0, 1, 0}, delta(before))

	f = newChangeFixture(postStatus(postA))
	f.store.matchErrs = 1
	f.clock = changeNow.Add(time.Hour)
	before = counts()

	f.tickStoreError(t)

	require.Equal(t, []float64{0, 1, 0, 0, 0, 1}, delta(before), "대응 단계의 저장소 오류로 멈춘 주기는 completed가 아니다")
	require.InDelta(t, float64(changeNow.Unix()), testutil.ToFloat64(changeLastCompleted), 0, "완료 시각을 갱신하지 않는다")

	f = newChangeFixture(postStatus(postA))
	f.store.decisionErr = errors.New("record decision: connection refused")
	before = counts()

	f.tickStoreError(t)

	require.Equal(t, []float64{0, 0, 0, 0, 0, 1}, delta(before), "판별 단계의 저장소 오류도 같다")
}
