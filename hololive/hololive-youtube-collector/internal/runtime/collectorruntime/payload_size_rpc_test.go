package collectorruntime

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
)

const payloadBoundaryDeferredState = "DEFERRED"

func TestCommunityRPCPayloadStorageBoundaryAndScheduler(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   int
		publish bool
	}{
		{name: "exact JSONB ceiling"},
		{name: "one over from real runner", extra: 1},
		{name: "one over at publisher", extra: 1, publish: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				fatal []error
				body  []byte
				calls atomic.Int32
			)

			server := newCommunityBoundaryServer(t, &calls, &body)

			rpc := youtubejs.NewRPC(server.Client(), server.URL, nil)
			realRunner := youtubejscollector.NewCommunityRunner(rpc)
			runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

			var (
				collectCause error
				oversized    []byte
			)

			runner.collect = func(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
				result, err := realRunner.Collect(ctx, input)

				collectCause = err

				if err != nil {
					return result, fmt.Errorf("collect community boundary fixture: %w", err)
				}

				if !tc.publish {
					return result, nil
				}

				// 이전 계약으로 준비된 envelope가 publish 경계에 도착해도 size cause를 보존한다.
				observations := result.Output().Observations()

				observations[0].Payload = oversized

				return collection.CompleteFromEnvelopes(observations, time.Now())
			}

			executor, spec := newExecutorFixture(t, runner, &fatal)

			executor.retryBounds = collection.RetryBounds{Minimum: time.Minute, Maximum: 10 * time.Minute}

			pool := executor.publisher.contracts.pool

			oversized, body = communityBoundaryInputs(t, pool, tc.extra, tc.publish)

			executor.runSpec(t.Context(), spec)

			require.Empty(t, fatal)
			require.EqualValues(t, 1, calls.Load())

			if tc.extra != 0 && !tc.publish {
				require.ErrorIs(t, collectCause, contract.ErrPayloadTooLarge)
				require.Equal(t, contract.ClassResourceLimit, collecterr.ClassOf(collectCause))
			}

			assertPayloadBoundaryTerminal(t, pool, spec.JobKey, tc.extra != 0, executor.retryBounds)
		})
	}
}

func newCommunityBoundaryServer(t *testing.T, calls *atomic.Int32, body *[]byte) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.Method != http.MethodPost || r.URL.Path != "/v1/community" {
			t.Errorf("unexpected RPC %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)

			return
		}

		var request youtubejs.CommunityRequest

		if err := jsonv2.UnmarshalRead(r.Body, &request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		if request.MaxResults != 10 || request.MaxPages < 1 || len(*body) > request.MaxSuccessResponseBytes {
			t.Errorf("fixture outside RPC request budget: %+v bytes=%d", request, len(*body))
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write(*body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func communityBoundaryInputs(t *testing.T, pool *pgxpool.Pool, extra int, publish bool) ([]byte, []byte) {
	t.Helper()

	payload, wire := communityStorageBoundary(t, pool, extra)
	oversized, err := jsonv2.Marshal(payload)
	require.NoError(t, err)

	oversized, err = contract.CanonicalizeJSON(oversized)
	require.NoError(t, err)

	if publish {
		_, wire = communityStorageBoundary(t, pool, 0)
	}

	body, err := jsonv2.Marshal(wire)
	require.NoError(t, err)
	require.Less(t, len(body), contract.MaxPayloadBytes)

	return oversized, body
}

func communityStorageBoundary(t *testing.T, pool *pgxpool.Pool, extra int) (contract.CommunityPayloadV1, youtubejs.CommunityResult) {
	t.Helper()

	payload := contract.CommunityPayloadV1{
		ChannelID: testSubjectKey,
		Coverage:  contract.CommunityPageCoverageV1{ChannelID: testSubjectKey, MaxResults: 10, PageCount: 1, Exhausted: true},
		Posts:     make([]contract.CommunityPostV1, 10),
	}
	photo := contract.Thumbnail{URL: "https://img.test/" + strings.Repeat("a", 1880), Width: 100, Height: 100}

	for i := range payload.Posts {
		payload.Posts[i] = contract.CommunityPostV1{
			PostID: fmt.Sprintf("post-%02d", i), ChannelID: testSubjectKey, AuthorID: "author", AuthorName: "author",
			ContentText: "x", AuthorPhoto: []contract.Thumbnail{photo}, Images: []contract.Thumbnail{photo, photo, photo},
			PublishedText: "2026-10-01", PublishedAt: new(time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)),
		}
	}

	raw, err := jsonv2.Marshal(payload)
	require.NoError(t, err)

	var baseSize int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT octet_length($1::jsonb::text)`, string(raw)).Scan(&baseSize))

	padding := contract.MaxPayloadBytes + extra - baseSize

	for i := range payload.Posts {
		n := padding / len(payload.Posts)
		if i < padding%len(payload.Posts) {
			n++
		}

		payload.Posts[i].ContentText += strings.Repeat("x", n)
	}

	require.NoError(t, payload.Validate(testSubjectKey))

	raw, err = jsonv2.Marshal(payload)
	require.NoError(t, err)

	var storedSize int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT octet_length($1::jsonb::text)`, string(raw)).Scan(&storedSize))
	require.Equal(t, contract.MaxPayloadBytes+extra, storedSize)
	require.LessOrEqual(t, len(raw), contract.MaxPayloadBytes)

	wire := communityBoundaryWire(&payload, &photo)

	return payload, wire
}

func communityBoundaryWire(payload *contract.CommunityPayloadV1, photo *contract.Thumbnail) youtubejs.CommunityResult {
	wire := youtubejs.CommunityResult{
		ProtocolVersion: youtubejs.ProtocolVersion,
		PageCount:       1, Exhausted: true, Continuity: string(contract.ContinuityContiguous), TerminationReason: youtubejs.TerminationExhausted,
		Posts: make([]*youtubejs.CommunityPost, len(payload.Posts)),
	}
	for i := range payload.Posts {
		post := &payload.Posts[i]
		images := make([]youtubejs.CommunityThumbnail, len(post.Images))

		for j, image := range post.Images {
			images[j] = youtubejs.CommunityThumbnail(image)
		}

		wire.Posts[i] = &youtubejs.CommunityPost{
			PostID: post.PostID, AuthorID: post.AuthorID, AuthorName: post.AuthorName, ContentText: post.ContentText,
			AuthorPhoto: []youtubejs.CommunityThumbnail{youtubejs.CommunityThumbnail(*photo)}, Images: images,
			PublishedText: post.PublishedText, PublishedAt: post.PublishedAt,
		}
	}

	return wire
}

func assertPayloadBoundaryTerminal(t *testing.T, pool *pgxpool.Pool, jobKey string, oversized bool, bounds collection.RetryBounds) {
	t.Helper()

	var (
		state, code, class string
		retryAt            *time.Time
		now                time.Time
	)

	if err := pool.QueryRow(t.Context(), `SELECT slot_state, COALESCE(last_failure_code,''), COALESCE(last_failure_class,''), retry_not_before, clock_timestamp() FROM youtube_collection_job_leases WHERE job_key=$1`, jobKey).Scan(&state, &code, &class, &retryAt, &now); err != nil {
		t.Fatal(err)
	}

	want := 1

	if oversized {
		want = 0

		require.Equal(t, payloadBoundaryDeferredState, state)
		require.Equal(t, string(contract.ErrorResponseTooLarge), code)
		require.Equal(t, string(contract.ClassResourceLimit), class)
		require.NotNil(t, retryAt)
		require.True(t, retryAt.After(now))
		require.False(t, retryAt.After(now.Add(bounds.Maximum)))
	} else {
		require.Equal(t, "IDLE", state)
		require.Empty(t, code)
		require.Empty(t, class)
	}

	for _, table := range []string{"source_observation_payloads", "source_observations", "source_observation_queue", "source_collection_checkpoints"} {
		var count int

		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count != want {
			t.Fatalf("%s count=%d, want %d", table, count, want)
		}
	}
}
