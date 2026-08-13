package htmlscraper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

type testMemberDataProvider struct {
	members []*domain.Member
}

func (p testMemberDataProvider) GetAllMembers() []*domain.Member                       { return p.members }
func (p testMemberDataProvider) FindMemberByChannelID(string) *domain.Member           { return nil }
func (p testMemberDataProvider) FindMemberByName(string) *domain.Member                { return nil }
func (p testMemberDataProvider) FindMemberByAlias(string) *domain.Member               { return nil }
func (p testMemberDataProvider) GetChannelIDs() []string                               { return nil }
func (p testMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider { return p }
func (p testMemberDataProvider) FindMembersByName(string) []*domain.Member             { return nil }
func (p testMemberDataProvider) FindMembersByAlias(string) []*domain.Member            { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingReadCloser struct {
	reader io.Reader
	closed atomic.Int32
}

func (r *trackingReadCloser) Read(buffer []byte) (int, error) {
	return r.reader.Read(buffer)
}

func (r *trackingReadCloser) Close() error {
	r.closed.Add(1)
	return nil
}

func newOfficialScheduleTestService(
	t *testing.T,
	handler http.Handler,
	members []*domain.Member,
) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewTestServiceWithHTTPClient(server.Client(), logger, server.URL, nil)
	service.identityIndex = buildOfficialScheduleIdentityIndex(testMemberDataProvider{members: members})
	return service, server
}

func writeJSON(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := io.WriteString(writer, body); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func TestOfficialScheduleAPIMapsGroup2(t *testing.T) {
	service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != officialScheduleAPIPath {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if accept := request.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept = %q", accept)
		}
		writeJSON(t, writer, `{
			"dateGroupList": [
				{"videoList": [
					{
						"datetime": "2026/12/31 23:59:58",
						"isLive": false,
						"url": "https://www.youtube.com/watch?v=video_one&feature=share",
						"thumbnail": "https://cdn.example/one.jpg",
						"title": "Provider title",
						"name": "Member One",
						"talent": {"name": "Member One"},
						"unknown": {"accepted": true}
					},
					{
						"datetime": "2027/01/01 00:01:02",
						"isLive": true,
						"url": "https://youtube.com/watch?v=video_two",
						"thumbnail": "http://invalid.example/two.jpg",
						"title": "",
						"name": "",
						"talent": {"name": "Member Two"},
						"collaboTalents": [{"name": "Member One"}]
					}
				]}
			]
		}`)
	}), []*domain.Member{
		{Name: "Member One", ChannelID: "channel-1"},
		{Name: "Member Two", ChannelID: "channel-2"},
	})

	streams, err := service.fetchOfficialScheduleAPI(context.Background())
	if err != nil {
		t.Fatalf("fetchOfficialScheduleAPI() error = %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("len(streams) = %d, want 2", len(streams))
	}

	first := streams[0]
	if first.ID != "video_one" || first.ChannelID != "channel-1" || first.Title != "Provider title" {
		t.Fatalf("first stream = %#v", first)
	}
	if first.Link == nil || *first.Link != "https://www.youtube.com/watch?v=video_one" {
		t.Fatalf("first link = %v", first.Link)
	}
	if first.Thumbnail == nil || *first.Thumbnail != "https://cdn.example/one.jpg" {
		t.Fatalf("first thumbnail = %v", first.Thumbnail)
	}
	if first.StartScheduled == nil || first.StartScheduled.Location().String() != "Asia/Tokyo" {
		t.Fatalf("first scheduled = %v", first.StartScheduled)
	}

	second := streams[1]
	if second.Status != domain.StreamStatusUpcoming || second.StartActual != nil {
		t.Fatalf("isLive API row changed live truth: %#v", second)
	}
	if second.Title != "Member Two" || second.ChannelID != "channel-2" {
		t.Fatalf("second stream = %#v", second)
	}
	wantThumbnail := "https://img.youtube.com/vi/video_two/maxresdefault.jpg"
	if second.Thumbnail == nil || *second.Thumbnail != wantThumbnail {
		t.Fatalf("second thumbnail = %v, want %s", second.Thumbnail, wantThumbnail)
	}
}

func TestOfficialScheduleIdentityRequiresOneDistinctChannel(t *testing.T) {
	index := buildOfficialScheduleIdentityIndex(testMemberDataProvider{members: []*domain.Member{
		{Name: "Shared", ChannelID: "channel-1", Aliases: &domain.Aliases{Ko: []string{"공유"}}},
		{Name: "Shared", ChannelID: "channel-2"},
		{Name: "Duplicate Same ID", ChannelID: "channel-3", Aliases: &domain.Aliases{Ja: []string{"同じ"}}},
		{Name: "Duplicate Same ID Again", ChannelID: "channel-3", Aliases: &domain.Aliases{Ja: []string{"同じ"}}},
	}})
	if got := index.resolve("Shared"); got != "" {
		t.Fatalf("ambiguous identity resolved to %q", got)
	}
	if got := index.resolve("同じ"); got != "channel-3" {
		t.Fatalf("same-channel duplicate resolved to %q", got)
	}
	if got := index.resolve("unknown"); got != "" {
		t.Fatalf("unknown identity resolved to %q", got)
	}
}

func TestOfficialScheduleAPIResponseContract(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantReason  officialScheduleReason
		wantError   bool
	}{
		{name: "valid empty", status: http.StatusOK, contentType: "application/json; charset=utf-8", body: `{"dateGroupList":[]}`},
		{name: "wrong content type", status: http.StatusOK, contentType: "text/html", body: `<html></html>`, wantReason: officialScheduleReasonContentType, wantError: true},
		{name: "not found", status: http.StatusNotFound, contentType: "application/json", body: `{}`, wantReason: officialScheduleReasonStatus, wantError: true},
		{name: "forbidden", status: http.StatusForbidden, contentType: "application/json", body: `{}`, wantReason: officialScheduleReasonStatus, wantError: true},
		{name: "rate limited", status: http.StatusTooManyRequests, contentType: "application/json", body: `{}`, wantReason: officialScheduleReasonStatus, wantError: true},
		{name: "server error", status: http.StatusServiceUnavailable, contentType: "application/json", body: `{}`, wantReason: officialScheduleReasonStatus, wantError: true},
		{name: "malformed JSON", status: http.StatusOK, contentType: "application/json", body: `{`, wantReason: officialScheduleReasonDecode, wantError: true},
		{name: "missing groups", status: http.StatusOK, contentType: "application/json", body: `{}`, wantReason: officialScheduleReasonSchema, wantError: true},
		{name: "wrong root type", status: http.StatusOK, contentType: "application/json", body: `[]`, wantReason: officialScheduleReasonSchema, wantError: true},
		{name: "null groups", status: http.StatusOK, contentType: "application/json", body: `{"dateGroupList":null}`, wantReason: officialScheduleReasonSchema, wantError: true},
		{name: "wrong group type", status: http.StatusOK, contentType: "application/json", body: `{"dateGroupList":{}}`, wantReason: officialScheduleReasonSchema, wantError: true},
		{name: "missing video list", status: http.StatusOK, contentType: "application/json", body: `{"dateGroupList":[{}]}`, wantReason: officialScheduleReasonSchema, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", test.contentType)
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}), nil)
			streams, err := service.fetchOfficialScheduleAPI(context.Background())
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError %v", err, test.wantError)
			}
			if err != nil && classifyOfficialScheduleReason(err, 0) != test.wantReason {
				t.Fatalf("reason = %q, want %q", classifyOfficialScheduleReason(err, 0), test.wantReason)
			}
			if err == nil && len(streams) != 0 {
				t.Fatalf("len(streams) = %d, want 0", len(streams))
			}
		})
	}
}

func TestOfficialScheduleAPISkipsInvalidRowsAndFailsWhenAllRowsInvalid(t *testing.T) {
	service := &Service{identityIndex: officialScheduleIdentityIndex{}}
	partial := []byte(`{"dateGroupList":[{"videoList":[
		{"datetime":"bad","url":"https://www.youtube.com/watch?v=invalid","name":"Bad"},
		{"datetime":"2026/12/31 20:00:00","url":"https://www.youtube.com/watch?v=valid","name":"Good","title":"Good"}
	]}]}`)
	streams, stats, err := service.decodeOfficialScheduleAPI(partial)
	if err != nil {
		t.Fatalf("decodeOfficialScheduleAPI() error = %v", err)
	}
	if len(streams) != 1 || stats.Invalid != 1 || stats.Unmapped != 1 {
		t.Fatalf("streams=%d stats=%+v", len(streams), stats)
	}

	allInvalid := []byte(`{"dateGroupList":[{"videoList":[
		{"datetime":"bad","url":"https://example.com/not-youtube","name":"Bad"}
	]}]}`)
	_, stats, err = service.decodeOfficialScheduleAPI(allInvalid)
	if err == nil || !IsStructureError(err) || stats.Invalid != 1 {
		t.Fatalf("error=%v stats=%+v", err, stats)
	}
}

func TestOfficialScheduleAPIDeduplicatesAndMergesProviderFields(t *testing.T) {
	service := &Service{identityIndex: officialScheduleIdentityIndex{}}
	body := []byte(`{"dateGroupList":[{"videoList":[
		{"datetime":"2026/12/31 20:00:00","url":"https://www.youtube.com/watch?v=duplicate","name":"Member","title":"","thumbnail":""},
		{"datetime":"2026/12/31 20:01:00","url":"https://www.youtube.com/watch?v=duplicate&feature=share","name":"Member","title":"Provider title","thumbnail":"https://cdn.example/provider.jpg"}
	]}]}`)
	streams, stats, err := service.decodeOfficialScheduleAPI(body)
	if err != nil {
		t.Fatalf("decodeOfficialScheduleAPI() error = %v", err)
	}
	if len(streams) != 1 || stats.Duplicate != 1 {
		t.Fatalf("streams=%d stats=%+v", len(streams), stats)
	}
	if streams[0].Title != "Provider title" || streams[0].Thumbnail == nil || *streams[0].Thumbnail != "https://cdn.example/provider.jpg" {
		t.Fatalf("merged stream = %#v", streams[0])
	}
}

func TestOfficialScheduleAPIClosesAndBoundsResponseBody(t *testing.T) {
	body := &trackingReadCloser{reader: strings.NewReader(strings.Repeat("x", 128))}
	service := &Service{
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       body,
			}, nil
		})},
		logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialSchedule:     settings.DefaultOfficialScheduleConfig(),
		maxResponseBodyBytes: 32,
		identityIndex:        officialScheduleIdentityIndex{},
	}

	_, err := service.fetchOfficialScheduleAPI(context.Background())
	if err == nil || !errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		t.Fatalf("error = %v, want ErrResponseBodyTooLarge", err)
	}
	if got := body.closed.Load(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
}

func TestOfficialScheduleFetchDeduplicatesConcurrentRequestsAndClonesCache(t *testing.T) {
	var requests atomic.Int32
	service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(25 * time.Millisecond)
		writeJSON(t, writer, `{"dateGroupList":[{"videoList":[{
			"datetime":"2026/12/31 20:00:00",
			"url":"https://www.youtube.com/watch?v=cached",
			"name":"Member",
			"title":"Cached"
		}]}]}`)
	}), nil)
	currentTime := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	service.nowFunc = func() time.Time { return currentTime }
	service.officialSchedule.PageCacheTTL = time.Minute

	const concurrency = 6
	results := make(chan []*domain.Stream, concurrency)
	var waitGroup sync.WaitGroup
	for range concurrency {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			streams, err := service.FetchUpcomingStreams(context.Background(), 0)
			if err != nil {
				t.Errorf("FetchUpcomingStreams() error = %v", err)
				return
			}
			results <- streams
		}()
	}
	waitGroup.Wait()
	close(results)
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}

	first := <-results
	first[0].Title = "mutated"
	cached, err := service.FetchUpcomingStreams(context.Background(), 0)
	if err != nil {
		t.Fatalf("cached FetchUpcomingStreams() error = %v", err)
	}
	if cached[0].Title == "mutated" {
		t.Fatal("cached stream leaked caller mutation")
	}

	currentTime = currentTime.Add(2 * time.Minute)
	if _, err := service.FetchUpcomingStreams(context.Background(), 0); err != nil {
		t.Fatalf("expired FetchUpcomingStreams() error = %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("request count after expiry = %d, want 2", got)
	}
}

func TestFetchChannelUsesOfficialAPIOnlyAfterYouTubeFailure(t *testing.T) {
	var requests atomic.Int32
	service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeJSON(t, writer, `{"dateGroupList":[{"videoList":[{
			"datetime":"2026/08/13 12:00:00",
			"url":"https://www.youtube.com/watch?v=fallback",
			"name":"Member",
			"title":"Fallback"
		}]}]}`)
	}), []*domain.Member{{Name: "Member", ChannelID: "channel-1"}})
	service.nowFunc = func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, officialScheduleJST) }
	service.fetchUpcoming = func(context.Context, string) ([]*parser.UpcomingEvent, error) {
		return nil, context.DeadlineExceeded
	}
	service.cache = &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) { return nil, false },
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}

	streams, err := service.FetchChannel(context.Background(), "channel-1", 24, false)
	if err != nil {
		t.Fatalf("FetchChannel() error = %v", err)
	}
	if len(streams) != 1 || streams[0].ID != "fallback" {
		t.Fatalf("streams = %#v", streams)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("official API requests = %d, want 1", got)
	}
}

func TestFetchChannelDoesNotUseOfficialAPIAfterYouTubeSuccessEmpty(t *testing.T) {
	var requests atomic.Int32
	service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeJSON(t, writer, `{"dateGroupList":[]}`)
	}), nil)
	service.fetchUpcoming = func(context.Context, string) ([]*parser.UpcomingEvent, error) {
		return []*parser.UpcomingEvent{}, nil
	}
	service.cache = &cachemocks.Client{
		GetStreamsFunc: func(context.Context, string) ([]*domain.Stream, bool) { return nil, false },
		SetStreamsFunc: func(context.Context, string, []*domain.Stream, time.Duration) {},
	}

	streams, err := service.FetchChannel(context.Background(), "channel-1", 24, false)
	if err != nil {
		t.Fatalf("FetchChannel() error = %v", err)
	}
	if len(streams) != 0 || requests.Load() != 0 {
		t.Fatalf("streams=%d official requests=%d", len(streams), requests.Load())
	}
}

func TestOfficialScheduleSharedFetchSurvivesLeaderCancellation(t *testing.T) {
	var requests atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		writeJSON(t, writer, `{"dateGroupList":[]}`)
	}), nil)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := service.FetchUpcomingStreams(leaderCtx, 24)
		leaderDone <- err
	}()
	<-started

	waiterDone := make(chan error, 1)
	go func() {
		_, err := service.FetchUpcomingStreams(context.Background(), 24)
		waiterDone <- err
	}()
	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter error = %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestOfficialScheduleFiltersPastAndHoursWindow(t *testing.T) {
	service, _ := newOfficialScheduleTestService(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(t, writer, `{"dateGroupList":[{"videoList":[
			{"datetime":"2026/08/13 09:59:59","url":"https://www.youtube.com/watch?v=past","name":"Member"},
			{"datetime":"2026/08/13 11:00:00","url":"https://www.youtube.com/watch?v=within","name":"Member"},
			{"datetime":"2026/08/13 13:00:01","url":"https://www.youtube.com/watch?v=beyond","name":"Member"}
		]}]}`)
	}), nil)
	service.nowFunc = func() time.Time {
		return time.Date(2026, 8, 13, 10, 0, 0, 0, officialScheduleJST)
	}

	streams, err := service.FetchUpcomingStreams(context.Background(), 3)
	if err != nil {
		t.Fatalf("FetchUpcomingStreams() error = %v", err)
	}
	if len(streams) != 1 || streams[0].ID != "within" {
		t.Fatalf("streams = %#v, want within only", streams)
	}
}

func TestOfficialScheduleAPIClosesRejectedResponseBody(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
	}{
		{name: "status", status: http.StatusServiceUnavailable, contentType: "application/json"},
		{name: "content type", status: http.StatusOK, contentType: "text/html"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingReadCloser{reader: strings.NewReader("rejected")}
			service := &Service{
				httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: test.status,
						Header:     http.Header{"Content-Type": []string{test.contentType}},
						Body:       body,
					}, nil
				})},
				logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
				officialSchedule:     settings.DefaultOfficialScheduleConfig(),
				maxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
				identityIndex:        officialScheduleIdentityIndex{},
			}
			_, err := service.fetchOfficialScheduleAPI(context.Background())
			if err == nil {
				t.Fatal("fetchOfficialScheduleAPI() error = nil")
			}
			if got := body.closed.Load(); got != 1 {
				t.Fatalf("close count = %d, want 1", got)
			}
		})
	}
}
