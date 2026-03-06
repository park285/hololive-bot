package scraper

import (
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/tidwall/gjson"
)

func pickBestYtInitialDataCandidate(candidates []string) (string, bool) {
	best := ""
	bestScore := -1_000_000_000

	for _, candidate := range candidates {
		score := scoreYtInitialDataCandidate(candidate)
		if score > bestScore || (score == bestScore && len(candidate) > len(best)) {
			bestScore = score
			best = candidate
		}
	}

	if best == "" {
		return "", false
	}
	return best, true
}

func scoreYtInitialDataCandidate(candidate string) int {
	if !json.Valid([]byte(candidate)) {
		return -1_000_000
	}

	data := gjson.Parse(candidate)
	score := 0

	if data.Get("contents").Exists() {
		score += 1000
	}
	if data.Get("contents.twoColumnBrowseResultsRenderer.tabs").Exists() {
		score += 4000
	}
	if data.Get("header").Exists() {
		score += 300
	}
	if data.Get("metadata").Exists() {
		score += 200
	}
	if data.Get("onResponseReceivedEndpoints").Exists() {
		score += 300
	}
	if data.Get("alerts").Exists() {
		score += 50
	}
	if data.Get("responseContext").Exists() {
		score += 10
	}

	score += min(len(candidate), 1_000_000) / 1000
	return score
}
