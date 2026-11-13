package scheduleposter

import (
	"bytes"
	"context"
	"encoding/base64"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/netguard"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

type fakeJudge struct {
	scores     map[string]posterScores
	errs       map[string]error
	periods    map[string]printedPeriod
	periodErrs map[string]error
	judged     []string
	read       []string
}

func (f *fakeJudge) judge(_ context.Context, src source) (posterScores, usage, error) {
	f.judged = append(f.judged, string(src.Data))

	return f.scores[string(src.Data)], fakeUsage, f.errs[string(src.Data)]
}

func (f *fakeJudge) readPeriod(_ context.Context, src source) (printedPeriod, usage, error) {
	f.read = append(f.read, string(src.Data))

	return f.periods[string(src.Data)], fakeUsage, f.periodErrs[string(src.Data)]
}

const (
	confirmName  = "confirm"
	firstPoster  = "first"
	secondPoster = "second"
	nov9         = "2026-11-09"
)

var (
	certain     = posterScores{schedule: 0.93, oneWeek: 0.94, oneStreamer: 0.92}
	thisWeek    = printedPeriod{FirstMonth: 11, FirstDay: 9, FirstWeekday: 1, LastMonth: 11, LastDay: 15, LastWeekday: 7}
	reviewPosts = time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC)
)

func reviewCandidate(id int64, imageURL string) posterstore.ReviewCandidate {
	return posterstore.ReviewCandidate{
		ID: id, ChannelID: sophiaChannel, ImageURL: imageURL, PostedAt: reviewPosts, Revision: 1,
	}
}

func reviewingService(store *fakeStore, sources *fakeSources, judge *fakeJudge) *Service {
	service := testService(store, sources, &fakeTranslator{})

	service.config.Targets = []Target{{ChannelID: sophiaChannel, YouTubeCommunity: true}}
	service.judge = judge

	return service
}

func TestCollectAutoReviewsEveryCandidate(t *testing.T) {
	t.Parallel()

	image := func(name string) source { return source{MIME: mimeJPEG, Data: []byte(name)} }

	thumbnail := posterstore.ReviewCandidate{
		ID: 7, ChannelID: sophiaChannel, ImageURL: "https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg",
		ImageSHA256: imageSHA256([]byte("collected")), PostedAt: reviewPosts, Revision: 1,
	}

	store := &fakeStore{unreviewed: []posterstore.ReviewCandidate{
		reviewCandidate(1, "u/"+confirmName), reviewCandidate(2, "u/reject"), reviewCandidate(3, "u/partial"),
		reviewCandidate(4, "u/undated"), reviewCandidate(5, "u/blocked"), reviewCandidate(6, "u/gone"), thumbnail,
		reviewCandidate(8, "u/group"),
	}}
	sources := &fakeSources{pages: xPages, images: map[string]source{
		"u/" + confirmName: image(confirmName), "u/reject": image("reject"), "u/partial": image("partial"), "u/undated": image("undated"),
		"u/blocked": image("blocked"), thumbnail.ImageURL: image("changed"), "u/group": image("group"),
	}}
	judge := &fakeJudge{
		scores: map[string]posterScores{
			confirmName: certain, "reject": {schedule: 0.1, oneWeek: 0.9, oneStreamer: 0.9},
			"partial": {schedule: 0.93, oneWeek: 0.19, oneStreamer: 0.91}, "undated": certain,
			"group": {schedule: 0.93, oneWeek: 0.94, oneStreamer: 0.1},
		},
		errs:    map[string]error{"blocked": fmt.Errorf("%w: input blocked: SAFETY", errUnjudgeable)},
		periods: map[string]printedPeriod{confirmName: thisWeek, "undated": {FirstMonth: 11, FirstDay: 9}},
	}

	service := reviewingService(store, sources, judge)

	service.config.Targets = []Target{{XUserID: sophiaUserID, ChannelID: sophiaChannel, YouTubeCommunity: true}}

	require.ErrorContains(t, service.collect(t.Context()), "auto review schedule poster 6: fetch poster image")

	require.Empty(t, store.communityPattern, "문구 패턴이 놓친 일정표까지 판정하도록 이미지 글을 모두 넣는다")
	require.Equal(t, []bool{true, true, true}, store.autoReviews, "X·커뮤니티·썸네일 후보 모두 자동 검토를 기다린다")
	require.Equal(t, []time.Time{service.now().Add(-handOverAfter)}, store.handOverBefore, "판정하지 못한 채 오래된 후보는 판정 전에 넘긴다")
	require.Equal(t, []string{
		"confirm 1@1 2026-11-09..2026-11-15",
		"reject 2@1",
		"request 3",
		"request 4",
		"request 5",
		"request 7",
		"request 8",
	}, store.reviewed, "원본을 받지 못한 6은 다음 주기에 다시 본다")
	require.Equal(t, []string{confirmName, "reject", "partial", "undated", "blocked", "group"}, judge.judged, "수집 뒤 바뀐 썸네일은 판정하지 않는다")
	require.Equal(t, []string{confirmName, "undated"}, judge.read, "세 답이 모두 높은 후보만 날짜를 읽는다")
}

func TestAutoReviewLeavesFailedCandidatesForTheNextCycle(t *testing.T) {
	t.Parallel()

	for name, judge := range map[string]*fakeJudge{
		"decision": {
			errs: map[string]error{firstPoster: errors.New("decision status 500 INTERNAL")}, scores: map[string]posterScores{secondPoster: certain},
			periods: map[string]printedPeriod{secondPoster: thisWeek},
		},
		"period": {
			scores: map[string]posterScores{firstPoster: certain, secondPoster: certain}, periods: map[string]printedPeriod{secondPoster: thisWeek},
			periodErrs: map[string]error{firstPoster: errors.New("read poster period: 500 INTERNAL")},
		},
		"source": {scores: map[string]posterScores{secondPoster: certain}, periods: map[string]printedPeriod{secondPoster: thisWeek}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			images := map[string]source{"u/first": {MIME: mimeJPEG, Data: []byte(firstPoster)}, "u/second": {MIME: mimeJPEG, Data: []byte(secondPoster)}}

			if name == "source" {
				delete(images, "u/first")
			}

			store := &fakeStore{unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/first"), reviewCandidate(2, "u/second")}}
			service := reviewingService(store, &fakeSources{images: images}, judge)

			err := service.autoReview(t.Context())
			require.ErrorContains(t, err, "auto review schedule poster 1")
			require.NotErrorIs(t, err, errReviewUnavailable)
			require.Equal(t, []string{"confirm 2@1 2026-11-09..2026-11-15"}, store.reviewed, "한 후보의 실패가 뒤 후보를 막지 않고, 그 후보는 넘기지 않는다")
		})
	}
}

func TestAutoReviewStopsWhileGeminiIsUnavailable(t *testing.T) {
	t.Parallel()

	unavailable := fmt.Errorf("%w: decision status 429 RESOURCE_EXHAUSTED", errReviewUnavailable)

	for name, judge := range map[string]*fakeJudge{
		"decision": {errs: map[string]error{firstPoster: unavailable}, scores: map[string]posterScores{secondPoster: certain}},
		"period": {
			scores: map[string]posterScores{firstPoster: certain, secondPoster: certain}, periodErrs: map[string]error{firstPoster: unavailable},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			images := map[string]source{"u/first": {MIME: mimeJPEG, Data: []byte(firstPoster)}, "u/second": {MIME: mimeJPEG, Data: []byte(secondPoster)}}
			store := &fakeStore{unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/first"), reviewCandidate(2, "u/second")}}

			require.ErrorIs(t, reviewingService(store, &fakeSources{images: images}, judge).autoReview(t.Context()), errReviewUnavailable)
			require.Empty(t, store.reviewed)
			require.Equal(t, []string{firstPoster}, judge.judged, "같은 실패가 뻔한 뒤 후보는 보내지 않는다")
		})
	}
}

func TestCollectHandsOverWaitingCandidatesWhenAutoReviewIsOff(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	service := testService(store, &fakeSources{pages: xPages}, &fakeTranslator{})

	require.NoError(t, service.collect(t.Context()))
	require.Equal(t, []time.Time{service.now()}, store.handOverBefore, "자동 검토를 끄면 기다리던 후보를 모두 넘긴다")
	require.Equal(t, []bool{false, false, false}, store.autoReviews)
}

func TestAutoReviewRecordsCandidatesSomeoneElseSettled(t *testing.T) {
	t.Parallel()

	for name, store := range map[string]*fakeStore{
		"hand over": {unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/partial")}, requestRefused: true},
		"reject":    {unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/reject")}, rejectErr: posterstore.ErrRevisionConflict},
		"confirm":   {unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/"+confirmName)}, confirmErr: posterstore.ErrInvalidState},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sources := &fakeSources{images: map[string]source{
				"u/partial": {MIME: mimeJPEG, Data: []byte("partial")}, "u/reject": {MIME: mimeJPEG, Data: []byte("reject")},
				"u/" + confirmName: {MIME: mimeJPEG, Data: []byte(confirmName)},
			}}
			judge := &fakeJudge{
				scores: map[string]posterScores{
					"partial": {schedule: 0.93, oneWeek: 0.19, oneStreamer: 0.91}, "reject": {schedule: 0.1}, confirmName: certain,
				},
				periods: map[string]printedPeriod{confirmName: thisWeek},
			}

			var logs bytes.Buffer

			service := reviewingService(store, sources, judge)

			service.logger = slog.New(slog.NewJSONHandler(&logs, nil))

			require.NoError(t, service.autoReview(t.Context()))
			require.Contains(t, logs.String(), `"outcome":"superseded"`, "그 사이 처리된 후보를 넘겼거나 판정했다고 기록하지 않는다")
		})
	}
}

func TestAutoReviewKeepsOneConfirmationPerWeek(t *testing.T) {
	t.Parallel()

	for coverage, want := range map[posterstore.WeekCoverage][]string{
		posterstore.WeekSame:        {"expire 1@1"},
		posterstore.WeekOverlapping: {"request 1"},
	} {
		store := &fakeStore{unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/"+confirmName)}, coverage: coverage}
		sources := &fakeSources{images: map[string]source{"u/" + confirmName: {MIME: mimeJPEG, Data: []byte(confirmName)}}}
		judge := &fakeJudge{scores: map[string]posterScores{confirmName: certain}, periods: map[string]printedPeriod{confirmName: thisWeek}}

		require.NoError(t, reviewingService(store, sources, judge).autoReview(t.Context()))
		require.Equal(t, want, store.reviewed, "coverage %d", coverage)
	}
}

func TestAutoReviewHandsAWeekTheStoreRefusesToAdmin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		confirmErr error
		want       []string
	}{
		{confirmErr: posterstore.ErrInvalidWeek, want: []string{"confirm 1@1 2026-11-09..2026-11-15", "request 1"}},
		{confirmErr: posterstore.ErrRevisionConflict, want: []string{"confirm 1@1 2026-11-09..2026-11-15"}},
	} {
		store := &fakeStore{unreviewed: []posterstore.ReviewCandidate{reviewCandidate(1, "u/"+confirmName)}, confirmErr: tc.confirmErr}
		sources := &fakeSources{images: map[string]source{"u/" + confirmName: {MIME: mimeJPEG, Data: []byte(confirmName)}}}
		judge := &fakeJudge{scores: map[string]posterScores{confirmName: certain}, periods: map[string]printedPeriod{confirmName: thisWeek}}

		require.NoError(t, reviewingService(store, sources, judge).autoReview(t.Context()))
		require.Equal(t, tc.want, store.reviewed, "%v", tc.confirmErr)
	}
}

func TestPrintedPeriodWeek(t *testing.T) {
	t.Parallel()

	day := func(value string) time.Time {
		parsed, err := time.Parse(time.DateOnly, value)
		require.NoError(t, err)

		return parsed
	}

	for _, tc := range []struct {
		name   string
		period printedPeriod
		posted string
		want   string
	}{
		{"posted on the first day", thisWeek, nov9, "2026-11-09..2026-11-15"},
		{"posted mid-week", thisWeek, "2026-11-12", "2026-11-09..2026-11-15"},
		{"next week posted on Sunday", printedPeriod{11, 16, 1, 11, 22, 7}, "2026-11-15", "2026-11-16..2026-11-22"},
		{"month from the weekday", printedPeriod{0, 9, 1, 0, 15, 7}, nov9, "2026-11-09..2026-11-15"},
		{"across the new year", printedPeriod{12, 28, 1, 1, 3, 7}, "2026-12-27", "2026-12-28..2027-01-03"},
		{"year from the weekday", printedPeriod{0, 28, 1, 0, 3, 7}, "2027-01-01", "2026-12-28..2027-01-03"},
		{"last date unread with a weekday", printedPeriod{11, 9, 1, 0, 0, 0}, nov9, "2026-11-09..2026-11-15"},
		{"last date unread without a weekday", printedPeriod{11, 9, 0, 0, 0, 0}, nov9, ""},
		{"no month and no weekday", printedPeriod{0, 9, 0, 0, 15, 0}, nov9, ""},
		{"no first date", printedPeriod{11, 0, 0, 11, 15, 7}, nov9, ""},
		{"weekday does not match", printedPeriod{11, 10, 1, 11, 16, 7}, nov9, ""},
		{"six days", printedPeriod{11, 9, 1, 11, 14, 6}, nov9, ""},
		{"last weekday does not match", printedPeriod{11, 9, 1, 11, 15, 6}, nov9, ""},
		{"last weekday read without its date", printedPeriod{11, 9, 1, 0, 0, 6}, nov9, ""},
		{"last month read without its date", printedPeriod{11, 9, 1, 12, 0, 0}, nov9, ""},
		{"a week that already ended", printedPeriod{11, 2, 1, 11, 8, 7}, "2026-11-12", ""},
		{"two weeks ahead", printedPeriod{11, 23, 1, 11, 29, 7}, "2026-11-12", ""},
		{"date that does not exist", printedPeriod{11, 31, 0, 12, 6, 0}, "2026-11-30", ""},
	} {
		start, end, err := tc.period.week(day(tc.posted))
		if tc.want == "" {
			require.Error(t, err, tc.name)

			continue
		}

		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, start.Format(time.DateOnly)+".."+end.Format(time.DateOnly), tc.name)
	}
}

func TestPostedDateUsesTheJapaneseDay(t *testing.T) {
	t.Parallel()

	require.Equal(t, nov9, postedDate(time.Date(2026, time.November, 8, 15, 0, 0, 0, time.UTC)).Format(time.DateOnly))
	require.Equal(t, "2026-11-08", postedDate(time.Date(2026, time.November, 8, 14, 59, 0, 0, time.UTC)).Format(time.DateOnly))
}

const (
	blameCandidate = "candidate"
	blameService   = "unavailable"
	blameNobody    = "retry"
)

func blame(err error) string {
	switch {
	case errors.Is(err, errUnjudgeable):
		return blameCandidate
	case errors.Is(err, errReviewUnavailable):
		return blameService
	default:
		return blameNobody
	}
}

func TestPeriodFailureBlame(t *testing.T) {
	t.Parallel()

	keyInvalid := []map[string]any{{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": "API_KEY_INVALID"}}

	for _, tc := range []struct {
		err  error
		want string
	}{
		{genai.APIError{Code: http.StatusBadRequest, Status: "INVALID_ARGUMENT", Message: "Unable to process input image."}, blameCandidate},
		{genai.APIError{Code: http.StatusBadRequest, Status: "INVALID_ARGUMENT", Details: keyInvalid}, blameService},
		{genai.APIError{Code: http.StatusBadRequest, Status: "FAILED_PRECONDITION", Message: "User location is not supported"}, blameService},
		{genai.APIError{Code: http.StatusNotFound, Status: "NOT_FOUND", Message: "model is not found"}, blameService},
		{genai.APIError{Code: http.StatusTooManyRequests, Status: "RESOURCE_EXHAUSTED"}, blameService},
		{genai.APIError{Code: http.StatusServiceUnavailable, Status: "UNAVAILABLE"}, blameService},
		{genai.APIError{Code: http.StatusInternalServerError, Status: "INTERNAL"}, blameNobody},
		{&url.Error{Op: "Post", URL: "https://" + geminiAPIHost, Err: errors.New("connection refused")}, blameService},
		{&url.Error{Op: "Post", URL: "https://" + geminiAPIHost, Err: context.DeadlineExceeded}, blameNobody},
	} {
		require.Equal(t, tc.want, blame(periodFailure(tc.err)), "%v", tc.err)
	}
}

func TestDecodeDecision(t *testing.T) {
	t.Parallel()

	const answered = `{"answers":{"is_schedule":{"type":"noul","noul":0.93,"confidence":0.93},
		"one_week":{"type":"noul","noul":0.91,"confidence":0.91},"one_streamer":{"type":"noul","noul":0.92,"confidence":0.92}},
		"usage":{"total_tokens":1533,"total_input_tokens":1533,"total_output_tokens":0}}`

	scores, spent, err := decodeDecision(http.StatusOK, []byte(answered))
	require.NoError(t, err)
	require.Equal(t, posterScores{schedule: 0.93, oneWeek: 0.91, oneStreamer: 0.92}, scores)
	require.Equal(t, usage{model: decisionModel, prompt: 1533}, spent)

	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"invalid image": {http.StatusBadRequest, `{"error":{"message":"Unable to process input image. Please retry or report in ` +
			`https://developers.generativeai.google/guide/troubleshooting","code":"invalid_request"}}`, blameCandidate},
		"blocked input":   {http.StatusOK, `{"prompt_feedback":{"block_reason":"SAFETY"},"usage":{"total_input_tokens":10}}`, blameCandidate},
		"missing answer":  {http.StatusOK, `{"answers":{"is_schedule":{"noul":0.9},"one_week":{"noul":0.9}}}`, blameCandidate},
		"broken response": {http.StatusOK, `{"answers":`, blameCandidate},
		"rate limited":    {http.StatusTooManyRequests, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, blameService},
		"unavailable":     {http.StatusServiceUnavailable, ``, blameService},
		"server error":    {http.StatusInternalServerError, `{"error":{"code":500,"message":"internal","status":"INTERNAL"}}`, blameNobody},
		"forbidden":       {http.StatusForbidden, `{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`, blameService},
		"invalid key": {http.StatusBadRequest, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.",
			"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`, blameService},
		"unsupported location": {
			http.StatusBadRequest,
			`{"error":{"code":400,"message":"User location is not supported for the API use.","status":"FAILED_PRECONDITION"}}`, blameService,
		},
	} {
		_, _, err := decodeDecision(tc.status, []byte(tc.body))
		require.Error(t, err, name)
		require.Equal(t, tc.want, blame(err), "%s: %v", name, err)
	}
}

type stubResolver struct {
	ip  net.IP
	err error
}

func (r stubResolver) LookupIP(context.Context, string, string) ([]net.IP, error) {
	if r.err != nil {
		return nil, r.err
	}

	return []net.IP{r.ip}, nil
}

func TestGeminiJudgeBlamesTheServiceOnlyWhenItCannotConnect(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(release) })

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	plain := &http.Client{Timeout: 100 * time.Millisecond}
	guarded := func(resolver netguard.Resolver, dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
		return netguard.GuardedClient(&http.Client{Timeout: time.Second, Transport: &http.Transport{DialContext: dial}},
			netguard.Policy{Resolver: resolver, AllowedHosts: []string{geminiAPIHost}, AllowedPorts: []string{"443"}, Schemes: []string{"https"}})
	}
	dialTimeout := func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	}

	for name, tc := range map[string]struct {
		client   *http.Client
		endpoint string
		want     string
	}{
		"no response":     {plain, hang.URL, blameNobody},
		"refused connect": {plain, closed.URL, blameService},
		"lookup timeout": {
			guarded(stubResolver{err: &net.DNSError{Err: "i/o timeout", Name: geminiAPIHost, IsTimeout: true}}, dialTimeout),
			decisionEndpoint, blameService,
		},
		"dial timeout": {guarded(stubResolver{ip: net.IPv4(142, 250, 196, 106)}, dialTimeout), decisionEndpoint, blameService},
	} {
		judge := &geminiJudge{http: tc.client, endpoint: tc.endpoint, key: "test-key-0123456789abcdef"}

		_, _, err := judge.judge(t.Context(), source{MIME: mimeJPEG, Data: []byte("jpeg")})
		require.Error(t, err, name)
		require.Equal(t, tc.want, blame(err), "%s: %v", name, err)
	}
}

func TestGeminiJudgeSendsTheDecisionRequest(t *testing.T) {
	t.Parallel()

	const key = "test-key-0123456789abcdef"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
			Input []struct {
				Type     string `json:"type"`
				MIMEType string `json:"mime_type"`
				Data     string `json:"data"`
			} `json:"input"`
			Questions map[string]decisionQuestion `json:"questions"`
		}

		if r.Method != http.MethodPost || r.Header.Get("x-goog-api-key") != key || jsonv2.UnmarshalRead(r.Body, &request) != nil ||
			request.Model != decisionModel || len(request.Input) != 1 || request.Input[0].Type != "image" ||
			request.Input[0].MIMEType != mimeJPEG || request.Input[0].Data != base64.StdEncoding.EncodeToString([]byte("jpeg")) ||
			len(request.Questions) != 3 {
			w.WriteHeader(http.StatusTeapot)

			return
		}

		for _, question := range request.Questions {
			if question.Type != "noul" || question.Instructions == "" || question.Criteria.TrueDescription == "" || question.Criteria.FalseDescription == "" {
				w.WriteHeader(http.StatusTeapot)

				return
			}
		}

		if _, err := w.Write([]byte(`{"answers":{"is_schedule":{"noul":0.9},"one_week":{"noul":0.8},"one_streamer":{"noul":0.7}},"usage":{"total_input_tokens":1500}}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	judge := &geminiJudge{http: server.Client(), endpoint: server.URL, key: key}

	scores, spent, err := judge.judge(t.Context(), source{MIME: mimeJPEG, Data: []byte("jpeg")})
	require.NoError(t, err)
	require.Equal(t, posterScores{schedule: 0.9, oneWeek: 0.8, oneStreamer: 0.7}, scores)
	require.EqualValues(t, 1500, spent.prompt)
}

func TestPeriodRequestAsksOnlyForPrintedDates(t *testing.T) {
	t.Parallel()

	contents, config := periodRequest(source{MIME: mimeJPEG, Data: []byte("jpeg")})

	require.Len(t, contents, 1)
	require.Equal(t, "application/json", config.ResponseMIMEType)
	require.Equal(t, genai.ThinkingLevelLow, config.ThinkingConfig.ThinkingLevel)
	require.Empty(t, config.Tools, "날짜는 보이는 글자만 읽어야 하므로 검색을 켜지 않는다")
	require.Equal(t, periodFields, config.ResponseSchema.Required)
	require.ElementsMatch(t, periodFields, slices.Collect(maps.Keys(config.ResponseSchema.Properties)))

	period, err := parsePeriod(`{"first_month":11,"first_day":9,"first_weekday":1,"last_month":11,"last_day":15,"last_weekday":7}`)
	require.NoError(t, err)
	require.Equal(t, thisWeek, period)

	_, err = parsePeriod(`{"first_month":11,"week":"this"}`)
	require.ErrorIs(t, err, errUnjudgeable)
}

func TestLoadAPIKey(t *testing.T) {
	t.Parallel()

	key, err := LoadAPIKey("")
	require.NoError(t, err)
	require.Empty(t, key, "경로가 없으면 자동 검토를 끈다")

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		return path
	}

	const secret = "test-key-0123456789abcdef"

	key, err = LoadAPIKey(write("key", secret+"\n"))
	require.NoError(t, err)
	require.Equal(t, secret, key)

	for _, content := range []string{"GEMINI_API_KEY=" + secret, "short", secret + " " + secret} {
		_, err = LoadAPIKey(write("bad", content))
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret, "오류에 키를 싣지 않는다")
	}

	_, err = LoadAPIKey("relative/key")
	require.Error(t, err)
}
