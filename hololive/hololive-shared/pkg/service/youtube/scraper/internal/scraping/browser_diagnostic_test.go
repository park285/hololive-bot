package scraping

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBrowserSnapshotEngineDoesNotBecomeDefaultPageFetcher(t *testing.T) {
	client := NewClient(WithFetcherEngine(FetcherEngineBrowserSnapshot))

	_, isNetHTTP := client.currentPageFetcher().(netHTTPPageFetcher)

	require.True(t, isNetHTTP)
}

func TestBrowserSnapshotEngineRequiresExplicitFetcher(t *testing.T) {
	fetcher := NewBrowserSnapshotFetcher("http://127.0.0.1:1/browser", time.Second)
	client := NewClient(
		WithFetcherEngine(FetcherEngineBrowserSnapshot),
		WithBrowserSnapshotFetcher(fetcher),
	)

	require.Same(t, fetcher, client.currentPageFetcher())
}

func TestCaptureBrowserDiagnosticSnapshotRequiresParserDriftHealth(t *testing.T) {
	ctx := context.Background()
	store := newChannelHealthTestStore()
	sink := &captureSink{}
	client := NewClient(
		WithStateStore(store),
		WithSnapshotSink(sink),
		WithSnapshotPolicy(SnapshotPolicy{Enabled: true, AllowedReasons: map[FailureReason]bool{FailureReasonParserDrift: true}}),
		WithBrowserSnapshotFetcher(NewBrowserSnapshotFetcher("http://127.0.0.1:1/browser", time.Millisecond)),
	)

	err := client.CaptureBrowserDiagnosticSnapshot(ctx, "UC_TEST", "https://www.youtube.com/channel/UC_TEST")

	require.NoError(t, err)
	require.Empty(t, sink.snapshots)
}

func TestCaptureBrowserDiagnosticSnapshotRequiresSnapshotEnabled(t *testing.T) {
	ctx := context.Background()
	store := newChannelHealthTestStore()
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"html":"<html>rendered</html>"}`))
	}))
	defer server.Close()

	client := NewClient(
		WithStateStore(store),
		WithSnapshotSink(&captureSink{}),
		WithBrowserSnapshotFetcher(NewBrowserSnapshotFetcher(server.URL, time.Second)),
	)
	client.channelHealth.RecordFailure(ctx, "UC_TEST", FailureDetail{Source: FailureSourceHTML, Reason: FailureReasonParserDrift}, time.Now())
	client.channelHealth.RecordFailure(ctx, "UC_TEST", FailureDetail{Source: FailureSourceHTML, Reason: FailureReasonParserDrift}, time.Now())
	client.channelHealth.RecordFailure(ctx, "UC_TEST", FailureDetail{Source: FailureSourceHTML, Reason: FailureReasonParserDrift}, time.Now())

	err := client.CaptureBrowserDiagnosticSnapshot(ctx, "UC_TEST", "https://www.youtube.com/channel/UC_TEST")

	require.NoError(t, err)
	require.False(t, serverCalled)
}

func TestCaptureBrowserDiagnosticSnapshotReservesIntervalBeforeFetch(t *testing.T) {
	ctx := context.Background()
	store := newChannelHealthTestStore()
	sink := &captureSink{}
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"html":"<html>rendered</html>"}`))
	}))
	defer server.Close()
	client := NewClient(
		WithStateStore(store),
		WithSnapshotSink(sink),
		WithSnapshotPolicy(SnapshotPolicy{
			Enabled:        true,
			MinInterval:    time.Hour,
			AllowedReasons: map[FailureReason]bool{FailureReasonParserDrift: true},
		}),
		WithBrowserSnapshotFetcher(NewBrowserSnapshotFetcher(server.URL, time.Second)),
	)
	client.channelHealth.RecordFailure(ctx, "UC_TEST", FailureDetail{Source: FailureSourceHTML, Reason: FailureReasonParserDrift}, time.Now())
	client.channelHealth.RecordFailure(ctx, "UC_TEST", FailureDetail{Source: FailureSourceHTML, Reason: FailureReasonParserDrift}, time.Now())
	client.channelHealth.RecordFailure(ctx, "UC_TEST", FailureDetail{Source: FailureSourceHTML, Reason: FailureReasonParserDrift}, time.Now())

	require.NoError(t, client.CaptureBrowserDiagnosticSnapshot(ctx, "UC_TEST", "https://www.youtube.com/channel/UC_TEST"))
	require.NoError(t, client.CaptureBrowserDiagnosticSnapshot(ctx, "UC_TEST", "https://www.youtube.com/channel/UC_TEST"))

	require.Equal(t, 1, serverCalls)
	require.Len(t, sink.snapshots, 1)
}

func TestBrowserSnapshotFetcherPostsSnapshotRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"html":"<html>rendered</html>"}`))
	}))
	defer server.Close()
	fetcher := NewBrowserSnapshotFetcher(server.URL, time.Second)

	resp, err := fetcher.FetchPage(context.Background(), pageFetchRequest{
		URL:    "https://www.youtube.com/channel/UC_TEST",
		Header: http.Header{"User-Agent": []string{"test"}},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []byte("<html>rendered</html>"), resp.Body)
}
