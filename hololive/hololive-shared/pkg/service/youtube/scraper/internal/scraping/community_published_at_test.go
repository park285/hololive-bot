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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPublishedAtFromHTML(t *testing.T) {
	t.Run("meta tag", func(t *testing.T) {
		publishedAt, err := extractPublishedAtFromHTML(`
			<html>
				<head>
					<meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00">
				</head>
			</html>
		`)
		require.NoError(t, err)
		require.NotNil(t, publishedAt)
		assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
	})

	t.Run("json ld", func(t *testing.T) {
		publishedAt, err := extractPublishedAtFromHTML(`
			<html>
				<head>
					<script type="application/ld+json">
						{"datePublished":"2026-04-10T10:11:12+09:00"}
					</script>
				</head>
			</html>
		`)
		require.NoError(t, err)
		require.NotNil(t, publishedAt)
		assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
	})
}

func TestExtractCommunityPublishedAtFromHTML(t *testing.T) {
	publishedAt, err := extractCommunityPublishedAtFromHTML(`
		<html>
			<head>
				<meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00">
			</head>
		</html>
	`)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}
