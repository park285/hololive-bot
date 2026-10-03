package youtubejscollector

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

var (
	testPublishedAt = time.Date(2026, time.August, 14, 0, 30, 0, 0, time.UTC)
	testPremiereAt  = time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC)
)

// 첫 실행은 목록 순서로 상한만큼만 영상별 확인을 보내고, 다음 실행은 저장된 cursor 근거를 재사용해 남은 항목만 조회합니다.
// Premiere가 공개로 바뀌면 바뀐 항목으로 다시 조회하고, 시각이 없던 항목은 그 뒤에 재조회합니다.
func TestContentRunnerEnrichesBoundedCandidatesAndReusesDurableCursor(t *testing.T) {
	t.Parallel()

	const premiere = "premiere"

	list := contentList(
		youtubejs.ContentItem{VideoID: "new-a", ChannelID: restrictedTestChannelID, Title: "A"},
		youtubejs.ContentItem{VideoID: premiere, ChannelID: restrictedTestChannelID, Title: "P", IsUpcoming: true},
		youtubejs.ContentItem{VideoID: "old-c", ChannelID: restrictedTestChannelID, Title: "C"},
	)
	checks := map[string]youtubejs.VideoLiveCheckResult{
		"new-a":  publishedCheck("new-a", testPublishedAt),
		premiere: premiereCheck(premiere, testPremiereAt),
	}

	first := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: list}, videoChecks: checks}
	payload, cursor := collectVideoList(t, first, nil)

	if !slices.Equal(first.videoCalls, []string{"new-a", premiere}) {
		t.Fatalf("first enrichment calls = %v", first.videoCalls)
	}

	requirePublished(t, payload, "new-a", testPublishedAt)
	requirePremiere(t, payload, premiere, testPremiereAt)
	requireNoPublication(t, payload, "old-c")

	second := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: list}, videoChecks: checks}

	payload, cursor = collectVideoList(t, second, earlierSlot(t, cursor))

	if !slices.Equal(second.videoCalls, []string{"old-c"}) {
		t.Fatalf("second enrichment calls = %v", second.videoCalls)
	}

	requirePublished(t, payload, "new-a", testPublishedAt)
	requirePremiere(t, payload, premiere, testPremiereAt)
	requireUnresolved(t, payload, "old-c")

	released := contentList(list.Items[0], youtubejs.ContentItem{VideoID: premiere, ChannelID: restrictedTestChannelID, Title: "P"}, list.Items[2])
	releasedAt := testPremiereAt.Add(time.Minute)
	third := &contentFake{
		results:     map[string]youtubejs.ContentResult{contentTabVideos: released},
		videoChecks: map[string]youtubejs.VideoLiveCheckResult{premiere: publishedCheck(premiere, releasedAt), "old-c": publishedCheck("old-c", testPublishedAt)},
	}

	payload, _ = collectVideoList(t, third, earlierSlot(t, cursor))

	if !slices.Equal(third.videoCalls, []string{premiere, "old-c"}) {
		t.Fatalf("third enrichment calls = %v", third.videoCalls)
	}

	requirePublished(t, payload, "new-a", testPublishedAt)
	requirePublished(t, payload, premiere, releasedAt)
	requirePublished(t, payload, "old-c", testPublishedAt)
}

// 근거 조회 실패는 그 항목만 근거 미수집으로 남기고 같은 job의 추가 조회를 멈춥니다. 목록·Shorts 관측은 그대로 발행되며,
// 다음 실행은 처음 보는 항목을 먼저 조회한 뒤 실패한 항목을 다시 조회합니다.
func TestContentRunnerKeepsListAndShortsWhenEnrichmentFails(t *testing.T) {
	t.Parallel()

	var shorts youtubejs.ContentResult

	loadJSON(t, "shorts.json", &shorts)

	list := contentList(
		youtubejs.ContentItem{VideoID: "failed", ChannelID: restrictedTestChannelID, Title: "F"},
		youtubejs.ContentItem{VideoID: "skipped", ChannelID: restrictedTestChannelID, Title: "S"},
	)
	fake := &contentFake{
		results:   map[string]youtubejs.ContentResult{contentTabVideos: list, contentTabShorts: shorts},
		videoErrs: map[string]error{"failed": collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "player timeout")},
	}

	result, err := NewContentRunner(fake, &cursorFake{}, 10, 0).Collect(t.Context(), contentInput(t))
	if err != nil {
		t.Fatal(err)
	}

	if result.Kind() != collectutil.CollectComplete || len(result.Output().Observations()) != 2 {
		t.Fatalf("result kind=%s observations=%d", result.Kind(), len(result.Output().Observations()))
	}

	if !slices.Equal(fake.videoCalls, []string{"failed"}) {
		t.Fatalf("enrichment continued after failure: %v", fake.videoCalls)
	}

	payload, cursor := videoListOutput(t, result.Output())
	requireNoPublication(t, payload, "failed")
	requireNoPublication(t, payload, "skipped")

	next := contentList(
		youtubejs.ContentItem{VideoID: "newest", ChannelID: restrictedTestChannelID, Title: "N"},
		list.Items[0], list.Items[1],
	)
	retry := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: next}}

	_, _ = collectVideoList(t, retry, earlierSlot(t, cursor))

	if !slices.Equal(retry.videoCalls, []string{"newest", "skipped"}) {
		t.Fatalf("retry order = %v, want fresh items before the failed retry", retry.videoCalls)
	}
}

func TestContentRunnerStopsOnNonDegradableEnrichmentFailure(t *testing.T) {
	t.Parallel()

	fake := &contentFake{
		results: map[string]youtubejs.ContentResult{
			contentTabVideos: contentList(youtubejs.ContentItem{VideoID: "a", ChannelID: restrictedTestChannelID}),
			contentTabShorts: {MissingTab: true},
		},
		videoErrs: map[string]error{"a": collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "helper configuration")},
	}

	result, err := NewContentRunner(fake, &cursorFake{}, 10, 0).Collect(t.Context(), contentInput(t))
	if err == nil || !result.IsZero() || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestContentRunnerPreservesNonDegradableErrorAfterEnrichmentDeadline(t *testing.T) {
	t.Parallel()

	fake := &contentFake{
		results: map[string]youtubejs.ContentResult{
			contentTabVideos: contentList(youtubejs.ContentItem{VideoID: "blocked", ChannelID: restrictedTestChannelID}),
			contentTabShorts: {MissingTab: true},
		},
		videoBlock: map[string]bool{"blocked": true},
		videoErrs:  map[string]error{"blocked": collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "helper configuration")},
	}
	input := contentInput(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)

	defer cancel()

	result, err := NewContentRunner(fake, &cursorFake{}, 10, time.Hour-200*time.Millisecond).Collect(ctx, input)
	if ctx.Err() != nil || err == nil || !result.IsZero() || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("parent=%v zero=%t kind=%s err=%v", ctx.Err(), result.IsZero(), result.Kind(), err)
	}
}

func TestContentRunnerRequiresPublicationContractGeneration(t *testing.T) {
	t.Parallel()

	input := youtubeInputWithGenerations(t, restrictedTestChannelID, "youtubejs_content",
		map[contract.ObservationKind]int64{contract.KindVideoList: contract.VideoListLegacyContractGeneration, contract.KindShortsList: 1},
		contract.KindVideoList, contract.KindShortsList)
	fake := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: contentList()}}

	result, err := NewContentRunner(fake, &cursorFake{}, 10, 0).Collect(t.Context(), input)
	if err == nil || !result.IsZero() || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("legacy generation collected: result=%#v err=%v", result, err)
	}
}

// 직전 video_list cursor를 해석할 수 없으면 조회 이력을 잃은 것처럼 다시 조회하지 않고 job을 실패시킵니다.
// 이전 cursor가 없는(NULL) 경우만 이력 없음이며, 다른 형식·버전·근거 규칙을 어긴 항목은 RPC 전에 거부합니다.
func TestContentRunnerFailsClosedOnUnreadableCursor(t *testing.T) {
	t.Parallel()

	cursors := map[string]string{
		"unknown version": `{"version":99,"scheduled_for":"2026-08-14T00:00:00Z","entries":[]}`,
		"foreign shape":   `{"page":2}`,
		"not an object":   `[]`,
		"entry without video": `{"version":1,"scheduled_for":"2026-08-14T00:00:00Z","entries":[` +
			`{"video_id":"","attempted_at":"2026-08-14T00:00:00Z"}]}`,
		"published evidence without time": `{"version":1,"scheduled_for":"2026-08-14T00:00:00Z","entries":[` +
			`{"video_id":"a","attempted_at":"2026-08-14T00:00:00Z","publication":{"status":"PUBLISHED","checked_at":"2026-08-14T00:00:00Z"}}]}`,
		"published after its check": `{"version":1,"scheduled_for":"2026-08-14T00:00:00Z","entries":[` +
			`{"video_id":"a","attempted_at":"2026-08-14T00:00:00Z","publication":{"status":"PUBLISHED",` +
			`"published_at":"2026-08-15T00:00:00Z","checked_at":"2026-08-14T00:00:00Z"}}]}`,
	}

	for name, raw := range cursors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &contentFake{
				results:     map[string]youtubejs.ContentResult{contentTabVideos: contentList(youtubejs.ContentItem{VideoID: "a", ChannelID: restrictedTestChannelID})},
				videoChecks: map[string]youtubejs.VideoLiveCheckResult{"a": publishedCheck("a", testPublishedAt)},
			}

			result, err := NewContentRunner(fake, &cursorFake{cursor: []byte(raw)}, 10, 0).Collect(t.Context(), contentInput(t))
			if err == nil || !result.IsZero() {
				t.Fatalf("unreadable cursor collected: result=%#v err=%v", result, err)
			}

			if len(fake.tabs) != 0 || len(fake.videoCalls) != 0 {
				t.Fatalf("provider called despite unreadable cursor: tabs=%v video calls=%v", fake.tabs, fake.videoCalls)
			}
		})
	}
}

// helper가 다른 영상의 응답을 돌려주면 그 시각을 요청 영상의 근거로 쓰지 않고 실패한 시도로 남기며 같은 job의 조회를 멈춥니다.
func TestContentRunnerNeverAttributesAnotherVideosEvidence(t *testing.T) {
	t.Parallel()

	fake := &contentFake{
		results: map[string]youtubejs.ContentResult{contentTabVideos: contentList(
			youtubejs.ContentItem{VideoID: "a", ChannelID: restrictedTestChannelID, Title: "A"},
			youtubejs.ContentItem{VideoID: "b", ChannelID: restrictedTestChannelID, Title: "B"},
		)},
		videoChecks: map[string]youtubejs.VideoLiveCheckResult{"a": publishedCheck("other-video", testPublishedAt)},
	}

	payload, raw := collectVideoList(t, fake, nil)
	if !slices.Equal(fake.videoCalls, []string{"a"}) {
		t.Fatalf("calls = %v", fake.videoCalls)
	}

	requireNoPublication(t, payload, "a")
	requireNoPublication(t, payload, "b")

	var cursor publicationCursor

	if err := jsonv2.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}

	if len(cursor.Entries) != 1 || cursor.Entries[0].VideoID != "a" || cursor.Entries[0].AttemptedAt.IsZero() || cursor.Entries[0].Publication != nil {
		t.Fatalf("cursor = %#v", cursor)
	}
}

// helper 목록 항목에 시각이 실려 와도 video_list 항목 시각은 영상별 근거에서만 나옵니다. 근거가 시각을 주지 않으면 시각 없이 남습니다.
func TestContentRunnerNeverCarriesListItemTimes(t *testing.T) {
	t.Parallel()

	lockupAt := testPublishedAt.Add(-24 * time.Hour)
	fake := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: contentList(
		youtubejs.ContentItem{VideoID: "a", ChannelID: restrictedTestChannelID, Title: "A", PublishedAt: new(lockupAt)},
		youtubejs.ContentItem{VideoID: "b", ChannelID: restrictedTestChannelID, Title: "B", ScheduledFor: new(lockupAt), IsUpcoming: true},
	)}}

	payload, _ := collectVideoList(t, fake, nil)

	requireUnresolved(t, payload, "a")
	requireUnresolved(t, payload, "b")

	if item := videoItem(t, payload, "b"); item.IsPremiere != nil {
		t.Fatalf("list upcoming flag became premiere evidence: %#v", item)
	}
}

// cache보다 큰 목록도 순환 위치를 따라 모두 조회하며, 직렬화된 이력은 매번 durable 상한 안에 머뭅니다.
func TestContentPublicationCursorDoesNotStarveBeyondCache(t *testing.T) {
	t.Parallel()

	const itemCount = maxPublicationCursorEntries + 12

	items := make([]youtubejs.ContentItem, itemCount)
	checks := make(map[string]youtubejs.VideoLiveCheckResult, itemCount)

	for i := range items {
		id := fmt.Sprintf("%0128d", i)

		items[i] = youtubejs.ContentItem{VideoID: id, ChannelID: restrictedTestChannelID}
		checks[id] = publishedCheck(id, testPublishedAt)
	}

	reader := &cursorFake{}
	fake := &contentFake{videoChecks: checks}
	runner := NewContentRunner(fake, reader, itemCount, 0)
	prior := storedPublicationCursor{}
	seen := make(map[string]bool, itemCount)

	for poll := range itemCount / ContentPublicationMaxCalls {
		before := len(fake.videoCalls)

		publications, cursor, err := runner.enrichPublications(t.Context(), t.Context(), restrictedTestChannelID, items, prior, 1<<20)
		if err != nil {
			t.Fatal(err)
		}

		calls := fake.videoCalls[before:]
		if len(calls) > ContentPublicationMaxCalls {
			t.Fatalf("poll %d exceeded call budget: %v", poll, calls)
		}

		for _, id := range calls {
			seen[id] = true
		}

		for i, publication := range publications {
			if publication != nil && (publication.Status != contract.VideoPublicationPublished || !publication.PublishedAt.Equal(testPublishedAt)) {
				t.Fatalf("poll %d changed trusted publication for %s: %+v", poll, items[i].VideoID, publication)
			}
		}

		cursor.ScheduledFor = testPublishedAt.Add(time.Duration(poll) * time.Minute)

		prior = reloadPublicationCursor(t, runner, reader, cursor)
	}

	for _, item := range items {
		if !seen[item.VideoID] {
			t.Fatalf("legal list entry was starved by the cache bound: %s", item.VideoID)
		}
	}
}

func reloadPublicationCursor(t *testing.T, runner *ContentRunner, reader *cursorFake, cursor publicationCursor) storedPublicationCursor {
	t.Helper()

	raw, err := marshalPublicationCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}

	if len(raw) > maxPublicationCursorBytes {
		t.Fatalf("cursor exceeds durable byte bound: %d", len(raw))
	}

	reader.cursor = raw

	prior, err := runner.loadPublicationCursor(t.Context(), restrictedTestChannelID)
	if err != nil {
		t.Fatal(err)
	}

	return prior
}

type classifyPublicationCase struct {
	name   string
	result youtubejs.VideoLiveCheckResult
	mutate func(*youtubejs.VideoLiveCheckResult)
	want   contract.VideoPublicationStatus
}

func TestClassifyPublicationRequiresExactTrustedFacts(t *testing.T) {
	t.Parallel()

	checkedAt := testPublishedAt.Add(time.Hour)

	for _, tt := range classifyPublicationCases(checkedAt) {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.result

			if tt.mutate != nil {
				tt.mutate(&result)
			}

			got := classifyPublication(&result, restrictedTestChannelID, checkedAt)
			if got.Status != tt.want || !got.CheckedAt.Equal(checkedAt) {
				t.Fatalf("classification = %#v, want %s", got, tt.want)
			}

			requireClassifiedTimes(t, &got, &result)
		})
	}
}

func classifyPublicationCases(checkedAt time.Time) []classifyPublicationCase {
	published := publishedCheck("v", testPublishedAt)
	premiere := premiereCheck("v", testPremiereAt)

	return []classifyPublicationCase{
		{name: "public exact", result: published, want: contract.VideoPublicationPublished},
		{name: "members only exact", result: published, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.Availability, result.Method = contract.VideoAvailabilityMembersOnly, contract.VideoAvailabilityMethodPlayerMembersOnly
		}, want: contract.VideoPublicationPublished},
		{name: "future publish date", result: publishedCheck("v", checkedAt.Add(time.Second)), want: contract.VideoPublicationUnresolved},
		{name: "missing publish date", result: published, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.PublishedAt = nil
		}, want: contract.VideoPublicationUnresolved},
		{name: "other channel", result: published, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.ChannelID = "UC_OTHER"
		}, want: contract.VideoPublicationUnresolved},
		{name: "private", result: published, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.Availability, result.Method, result.IsPrivate = contract.VideoAvailabilityPublicUnavailable, contract.VideoAvailabilityMethodPlayerPrivate, new(true)
		}, want: contract.VideoPublicationUnresolved},
		{name: "upcoming premiere", result: premiere, want: contract.VideoPublicationUpcomingPremiere},
		{name: "upcoming live stream", result: premiere, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.IsLiveContent = new(true)
		}, want: contract.VideoPublicationUnresolved},
		{name: "upcoming without confirmed schedule", result: premiere, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.ScheduledAt = nil
		}, want: contract.VideoPublicationUnresolved},
		{name: "upcoming publish date is not a release", result: premiere, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.ScheduledAt, result.PublishedAt = nil, new(testPublishedAt)
		}, want: contract.VideoPublicationUnresolved},
		{name: "published at the check time", result: publishedCheck("v", checkedAt), want: contract.VideoPublicationPublished},
		{name: "unconfirmed identity", result: published, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.IdentityConfirmed = false
		}, want: contract.VideoPublicationUnresolved},
		{name: "unclassified availability", result: published, mutate: func(result *youtubejs.VideoLiveCheckResult) {
			result.Availability, result.Method = contract.VideoAvailabilityUnknown, contract.VideoAvailabilityMethodUnknown
			result.UnknownReason = contract.LiveCheckReasonAvailabilityUnclassified
		}, want: contract.VideoPublicationUnresolved},
	}
}

// requireClassifiedTimes는 분류 상태에 맞는 시각만 player 응답 그대로 실렸는지 확인합니다.
func requireClassifiedTimes(t *testing.T, got *contract.VideoPublicationV1, result *youtubejs.VideoLiveCheckResult) {
	t.Helper()

	switch got.Status {
	case contract.VideoPublicationPublished:
		if got.PublishedAt == nil || !got.PublishedAt.Equal(*result.PublishedAt) || got.ScheduledFor != nil {
			t.Fatalf("published classification = %#v", got)
		}
	case contract.VideoPublicationUpcomingPremiere:
		if got.ScheduledFor == nil || !got.ScheduledFor.Equal(*result.ScheduledAt) || got.PublishedAt != nil {
			t.Fatalf("premiere classification = %#v", got)
		}
	case contract.VideoPublicationUnresolved:
		if got.PublishedAt != nil || got.ScheduledFor != nil {
			t.Fatalf("unresolved classification carries a time: %#v", got)
		}
	}
}

// earlierSlot은 cursor를 직전 slot에서 수락된 것으로 바꿉니다. 테스트 입력은 모두 같은 slot을 쓰므로, 다음 poll을 흉내 낼 때 씁니다.
func earlierSlot(t *testing.T, raw []byte) []byte {
	t.Helper()

	var cursor publicationCursor

	if err := jsonv2.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}

	cursor.ScheduledFor = cursor.ScheduledFor.Add(-time.Hour)

	shifted, err := jsonv2.Marshal(cursor)
	if err != nil {
		t.Fatal(err)
	}

	return shifted
}

func contentInput(tb testing.TB) *collectutil.RunInput {
	tb.Helper()

	return youtubeInput(tb, restrictedTestChannelID, "youtubejs_content", contract.KindVideoList, contract.KindShortsList)
}

func contentList(items ...youtubejs.ContentItem) youtubejs.ContentResult {
	return youtubejs.ContentResult{
		Items:     items,
		PageCount: 1, Exhausted: true, Continuity: string(contract.ContinuityContiguous), TerminationReason: youtubejs.TerminationExhausted,
	}
}

func publishedCheck(videoID string, publishedAt time.Time) youtubejs.VideoLiveCheckResult {
	return youtubejs.VideoLiveCheckResult{
		VideoID: videoID, ChannelID: restrictedTestChannelID, IdentityConfirmed: true,
		IsLive: new(false), IsUpcoming: new(false), IsLiveContent: new(false), IsPrivate: new(false),
		PublishedAt: new(publishedAt), Availability: contract.VideoAvailabilityPublic, Method: contract.VideoAvailabilityMethodPlayerPublic,
	}
}

func premiereCheck(videoID string, scheduledAt time.Time) youtubejs.VideoLiveCheckResult {
	return youtubejs.VideoLiveCheckResult{
		VideoID: videoID, ChannelID: restrictedTestChannelID, IdentityConfirmed: true,
		IsLive: new(false), IsUpcoming: new(true), IsLiveContent: new(false), IsPrivate: new(false),
		ScheduledAt: new(scheduledAt), WaitingStateConfirmed: new(true),
		Availability: contract.VideoAvailabilityPublic, Method: contract.VideoAvailabilityMethodPlayerPublic,
	}
}

func collectVideoList(t *testing.T, fake *contentFake, cursor []byte) (contract.VideoListV1, []byte) {
	t.Helper()

	input := withEnabled(t, contentInput(t), map[contract.ObservationKind][]string{
		contract.KindVideoList:  {restrictedTestChannelID},
		contract.KindShortsList: {},
	})

	result, err := NewContentRunner(fake, &cursorFake{cursor: cursor}, 10, 0).Collect(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}

	return videoListOutput(t, result.Output())
}

func videoListOutput(t *testing.T, output collectutil.RunOutput) (contract.VideoListV1, []byte) {
	t.Helper()

	observations := output.Observations()
	checkpoints := output.Checkpoints()

	for i := range observations {
		if observations[i].ObservationKind != contract.KindVideoList {
			continue
		}

		var payload contract.VideoListV1

		if err := jsonv2.Unmarshal(observations[i].Payload, &payload); err != nil {
			t.Fatal(err)
		}

		if observations[i].ContractGeneration != contract.VideoListPublicationContractGeneration {
			t.Fatalf("video list generation = %d", observations[i].ContractGeneration)
		}

		return payload, checkpoints[i].Cursor
	}

	t.Fatal("video list observation is missing")

	return contract.VideoListV1{}, nil
}

func videoItem(t *testing.T, payload contract.VideoListV1, videoID string) contract.VideoListItemV1 {
	t.Helper()

	index := slices.IndexFunc(payload.Videos, func(item contract.VideoListItemV1) bool { return item.VideoID == videoID })
	if index < 0 {
		t.Fatalf("video %s is missing", videoID)
	}

	return payload.Videos[index]
}

func requirePublished(t *testing.T, payload contract.VideoListV1, videoID string, publishedAt time.Time) {
	t.Helper()

	item := videoItem(t, payload, videoID)
	if item.Publication == nil || item.Publication.Status != contract.VideoPublicationPublished ||
		item.PublishedAt == nil || !item.PublishedAt.Equal(publishedAt) || item.ScheduledFor != nil || item.IsPremiere != nil {
		t.Fatalf("%s = %#v", videoID, item)
	}
}

func requirePremiere(t *testing.T, payload contract.VideoListV1, videoID string, scheduledFor time.Time) {
	t.Helper()

	item := videoItem(t, payload, videoID)
	if item.Publication == nil || item.Publication.Status != contract.VideoPublicationUpcomingPremiere ||
		item.ScheduledFor == nil || !item.ScheduledFor.Equal(scheduledFor) || item.IsPremiere == nil || !*item.IsPremiere || item.PublishedAt != nil {
		t.Fatalf("%s = %#v", videoID, item)
	}
}

func requireUnresolved(t *testing.T, payload contract.VideoListV1, videoID string) {
	t.Helper()

	item := videoItem(t, payload, videoID)
	if item.Publication == nil || item.Publication.Status != contract.VideoPublicationUnresolved || item.PublishedAt != nil || item.ScheduledFor != nil {
		t.Fatalf("%s = %#v", videoID, item)
	}
}

func requireNoPublication(t *testing.T, payload contract.VideoListV1, videoID string) {
	t.Helper()

	item := videoItem(t, payload, videoID)
	if item.Publication != nil || item.PublishedAt != nil || item.ScheduledFor != nil || item.IsPremiere != nil {
		t.Fatalf("%s = %#v", videoID, item)
	}
}

// 앞쪽 후보가 계속 실패하거나 시각이 없어도 처음 보는 항목이 먼저 조회되고, 근거를 받은 항목은 다음 poll에서 다시 조회하지 않습니다.
// 상한 2의 bounded 조회가 bootstrap 목록에서도 poll마다 전진하고, 실패한 항목은 가장 오래된 시도 순서로 다시 돌아옵니다.
func TestContentRunnerProgressesFairlyAcrossPolls(t *testing.T) {
	t.Parallel()

	ids := []string{"v1", "v2", "v3", "v4", "v5"}
	items := make([]youtubejs.ContentItem, 0, len(ids))
	checks := make(map[string]youtubejs.VideoLiveCheckResult, len(ids))

	for _, id := range ids {
		items = append(items, youtubejs.ContentItem{VideoID: id, ChannelID: restrictedTestChannelID, Title: id})
		checks[id] = publishedCheck(id, testPublishedAt)
	}

	list := contentList(items...)
	failing := map[string]error{"v1": collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "player timeout")}

	var cursor []byte

	for poll, want := range [][]string{{"v1"}, {"v2", "v3"}, {"v4", "v5"}, {"v1"}} {
		fake := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: list}, videoChecks: checks, videoErrs: failing}

		if cursor != nil {
			cursor = earlierSlot(t, cursor)
		}

		var payload contract.VideoListV1

		payload, cursor = collectVideoList(t, fake, cursor)

		if !slices.Equal(fake.videoCalls, want) {
			t.Fatalf("poll %d calls = %v, want %v", poll, fake.videoCalls, want)
		}

		requireNoPublication(t, payload, "v1")

		if poll >= 2 {
			for _, id := range ids[1:] {
				requirePublished(t, payload, id, testPublishedAt)
			}
		}
	}
}

// shorts 실패로 video_list만 PARTIAL로 수락된 slot을 같은 slot에서 재시도하면 video_list 목록·근거를 다시 조회하지 않습니다.
// 같은 관측 identity에 다른 payload를 만들어 collision을 내거나 cursor 전진을 잃지 않고, 실패한 shorts만 다시 수집합니다.
func TestContentRunnerSameSlotRetryCollectsOnlyUnacceptedKinds(t *testing.T) {
	t.Parallel()

	var shorts youtubejs.ContentResult

	loadJSON(t, "shorts.json", &shorts)

	list := contentList(
		youtubejs.ContentItem{VideoID: "a", ChannelID: restrictedTestChannelID, Title: "A"},
		youtubejs.ContentItem{VideoID: "b", ChannelID: restrictedTestChannelID, Title: "B"},
		youtubejs.ContentItem{VideoID: "c", ChannelID: restrictedTestChannelID, Title: "C"},
	)
	first := &contentFake{
		results:   map[string]youtubejs.ContentResult{contentTabVideos: list},
		errByKind: map[string]error{contentTabShorts: collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "shorts timeout")},
	}

	partial, err := NewContentRunner(first, &cursorFake{}, 10, 0).Collect(t.Context(), contentInput(t))
	if err != nil {
		t.Fatal(err)
	}

	if partial.Kind() != collectutil.CollectPartial || !slices.Equal(first.videoCalls, []string{"a", "b"}) {
		t.Fatalf("first attempt kind=%s calls=%v", partial.Kind(), first.videoCalls)
	}

	_, cursor := videoListOutput(t, partial.Output())
	retry := &contentFake{results: map[string]youtubejs.ContentResult{contentTabVideos: list, contentTabShorts: shorts}}

	result, err := NewContentRunner(retry, &cursorFake{cursor: cursor}, 10, 0).Collect(t.Context(), contentInput(t))
	if err != nil {
		t.Fatal(err)
	}

	observations := result.Output().Observations()
	if result.Kind() != collectutil.CollectComplete || len(observations) != 1 || observations[0].ObservationKind != contract.KindShortsList {
		t.Fatalf("retry kind=%s observations=%#v", result.Kind(), observations)
	}

	if !slices.Equal(retry.tabs, []string{contentTabShorts}) || len(retry.videoCalls) != 0 {
		t.Fatalf("retry tabs=%v video calls=%v", retry.tabs, retry.videoCalls)
	}
}

// 근거 조회는 수집 기한에서 예약분을 뺀 기한 안에서만 합니다. 조회가 그 기한에 끊기면 그 항목을 실패한 시도로 남기고
// 더 조회하지 않으며, 이미 받은 목록·Shorts는 수집 기한 안에 그대로 발행합니다.
func TestContentRunnerPublicationDeadlineKeepsListsAndCursorProgress(t *testing.T) {
	t.Parallel()

	var shorts youtubejs.ContentResult

	loadJSON(t, "shorts.json", &shorts)

	fake := &contentFake{
		results: map[string]youtubejs.ContentResult{contentTabVideos: contentList(
			youtubejs.ContentItem{VideoID: "slow", ChannelID: restrictedTestChannelID, Title: "S"},
			youtubejs.ContentItem{VideoID: "next", ChannelID: restrictedTestChannelID, Title: "N"},
		), contentTabShorts: shorts},
		videoBlock: map[string]bool{"slow": true},
	}
	input := contentInput(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)

	defer cancel()

	result, err := NewContentRunner(fake, &cursorFake{}, 10, time.Hour-50*time.Millisecond).Collect(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	if ctx.Err() != nil || result.Kind() != collectutil.CollectComplete || len(result.Output().Observations()) != 2 {
		t.Fatalf("ctx=%v kind=%s observations=%d", ctx.Err(), result.Kind(), len(result.Output().Observations()))
	}

	if !slices.Equal(fake.videoCalls, []string{"slow"}) || !slices.Equal(fake.tabs, []string{contentTabVideos, contentTabShorts}) {
		t.Fatalf("calls=%v tabs=%v", fake.videoCalls, fake.tabs)
	}

	payload, raw := videoListOutput(t, result.Output())
	requireNoPublication(t, payload, "slow")

	var cursor publicationCursor

	if err := jsonv2.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}

	if len(cursor.Entries) != 1 || cursor.Entries[0].VideoID != "slow" || cursor.Entries[0].Publication != nil {
		t.Fatalf("cursor = %#v", cursor)
	}

	exhausted := &contentFake{results: fake.results}
	if _, err := NewContentRunner(exhausted, &cursorFake{}, 10, 2*time.Hour).Collect(ctx, input); err != nil || len(exhausted.videoCalls) != 0 {
		t.Fatalf("exhausted reserve err=%v calls=%v", err, exhausted.videoCalls)
	}
}
