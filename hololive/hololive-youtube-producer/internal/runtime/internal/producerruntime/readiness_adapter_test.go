package producerruntime

import "testing"

func TestNewReadinessStateWithFetcherEngineIncludesEngine(t *testing.T) {
	state := newReadinessStateWithFetcherEngine("youtube-producer", ingestionRuntimeFeatures{
		youtubeEnabled: true,
	}, "goscrapy")
	state.MarkRunning()

	_, payload := state.Response()

	if payload["scraper_fetcher_engine"] != "goscrapy" {
		t.Fatalf("scraper_fetcher_engine = %v, want goscrapy", payload["scraper_fetcher_engine"])
	}
}
