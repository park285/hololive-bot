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

package scraping

import (
	json "github.com/park285/shared-go/pkg/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseSubscriberCount(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"2.76M subscribers", 2_760_000},
		{"1.5K subscribers", 1_500},
		{"1,234,567 subscribers", 1_234_567},
		{"500 subscribers", 500},
		{"1 subscriber", 1},
		{"No subscribers", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseSubscriberCount(tt.input)
			assert.Equal(t, tt.expected, result, "input: %s", tt.input)
		})
	}
}

func TestParseShortNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"2.76M", 2_760_000},
		{"1.5K", 1_500},
		{"1B", 1_000_000_000},
		{"1,234,567", 1_234_567},
		{"500", 500},
		{"No", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseShortNumber(tt.input)
			assert.Equal(t, tt.expected, result, "input: %s", tt.input)
		})
	}
}

func TestParseViewCount(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1,056,229,686 views", 1_056_229_686},
		{"1000 views", 1000},
		{"1 view", 1},
		{"69万回視聴", 690_000},
		{"조회수 1.2만회", 12_000},
		{"1.5M views", 1_500_000},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseViewCount(tt.input)
			assert.Equal(t, tt.expected, result, "input: %s", tt.input)
		})
	}
}

func TestParseVideoCount(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"2,429 videos", 2429},
		{"100 videos", 100},
		{"1 video", 1},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseVideoCount(tt.input)
			assert.Equal(t, tt.expected, result, "input: %s", tt.input)
		})
	}
}

func TestParseJoinedDate(t *testing.T) {
	tests := []struct {
		input   string
		notZero bool
	}{
		{"Joined Jul 2, 2019", true},
		{"Joined January 15, 2020", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseJoinedDate(tt.input)
			if tt.notZero {
				assert.NotEqual(t, int64(0), result, "input: %s", tt.input)
			} else {
				assert.Equal(t, int64(0), result, "input: %s", tt.input)
			}
		})
	}
}

func TestExtractYtInitialData_PrefersRichCandidate(t *testing.T) {
	html := `
<script>
  var ytInitialData = {"responseContext":{"webResponseContextExtensionData":{"ytConfigData":{"visitorData":"A"}}}};
</script>
<script>
  var ytInitialData = {"responseContext":{"webResponseContextExtensionData":{"ytConfigData":{"visitorData":"B"}}},"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Videos"}}]}},"metadata":{"channelMetadataRenderer":{"title":"Test"}}};
</script>
`

	got, err := extractYtInitialData(html)
	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(got)), "추출 결과는 유효 JSON이어야 함")

	data := gjson.Parse(got)
	assert.True(t, data.Get("contents.twoColumnBrowseResultsRenderer.tabs").Exists())
	assert.Equal(t, "Test", data.Get("metadata.channelMetadataRenderer.title").String())
}

func TestExtractYtInitialData_PrefersRichCandidateAcrossPatterns(t *testing.T) {
	html := `
<script>
  var ytInitialData = {"responseContext":{"webResponseContextExtensionData":{"ytConfigData":{"visitorData":"A"}}}};
</script>
<script>
  window["ytInitialData"] = {"responseContext":{"webResponseContextExtensionData":{"ytConfigData":{"visitorData":"B"}}},"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Videos"}}]}},"metadata":{"channelMetadataRenderer":{"title":"Pattern3"}}};
</script>
`

	got, err := extractYtInitialData(html)
	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(got)))

	data := gjson.Parse(got)
	assert.True(t, data.Get("contents.twoColumnBrowseResultsRenderer.tabs").Exists())
	assert.Equal(t, "Pattern3", data.Get("metadata.channelMetadataRenderer.title").String())
}

func TestExtractYtInitialData_IgnoresTrailingStatements(t *testing.T) {
	html := `
<script>
  var ytInitialData = {"responseContext":{"webResponseContextExtensionData":{"ytConfigData":{"visitorData":"A"}}},"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[]}}};
  var extraConfig = {"foo":"bar"};
</script>
`

	got, err := extractYtInitialData(html)
	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(got)))
	assert.True(t, strings.Contains(got, `"twoColumnBrowseResultsRenderer"`))

	data := gjson.Parse(got)
	assert.True(t, data.Get("contents.twoColumnBrowseResultsRenderer.tabs").Exists())
}

func TestExtractYtInitialData_NotFound(t *testing.T) {
	_, err := extractYtInitialData(`<html><body>No ytInitialData here</body></html>`)
	require.ErrorIs(t, err, ErrYtInitialDataNotFound)
}

func TestExtractYtInitialData_DOMFallbackSupportsWindowDotAssignment(t *testing.T) {
	html := `
<html>
  <body>
    <script nonce="abc123">
      window.ytInitialData = {"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Videos"}}]}},"metadata":{"channelMetadataRenderer":{"title":"DOMFallback"}}};
    </script>
  </body>
</html>
`

	got, err := extractYtInitialData(html)
	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(got)))

	data := gjson.Parse(got)
	assert.Equal(t, "DOMFallback", data.Get("metadata.channelMetadataRenderer.title").String())
	assert.True(t, data.Get("contents.twoColumnBrowseResultsRenderer.tabs").Exists())
}
