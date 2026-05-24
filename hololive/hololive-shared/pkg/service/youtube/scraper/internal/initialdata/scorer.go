// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package initialdata

import (
	json "github.com/park285/shared-go/pkg/json"
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
