package producerruntime

import "testing"

func TestNewReadinessStateWithFetcherEngineIncludesEngine(t *testing.T) {
	state := newReadinessStateWithFetcherEngine("youtube-producer", ingestionRuntimeFeatures{
		youtubeEnabled: true,
	}, "nethttp")
	state.MarkRunning()

	_, payload := state.Response()

	if payload["scraper_fetcher_engine"] != "nethttp" {
		t.Fatalf("scraper_fetcher_engine = %v, want nethttp", payload["scraper_fetcher_engine"])
	}
}
