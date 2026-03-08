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

package holodex

import "testing"

func TestBuildSearchChannelsCacheKey_NormalizesEquivalentQueries(t *testing.T) {
	t.Parallel()

	first := buildSearchChannelsCacheKey("  Aqua ")
	second := buildSearchChannelsCacheKey("aqua")
	if first != second {
		t.Fatalf("buildSearchChannelsCacheKey equivalent queries mismatch: %q vs %q", first, second)
	}
	if first == searchChannelsCacheKeyPrefix+"empty" {
		t.Fatalf("buildSearchChannelsCacheKey(%q) returned empty key", "Aqua")
	}
}

func TestBuildSearchChannelsCacheKey_UsesEmptySuffixForBlankQuery(t *testing.T) {
	t.Parallel()

	if got := buildSearchChannelsCacheKey("   "); got != searchChannelsCacheKeyPrefix+"empty" {
		t.Fatalf("buildSearchChannelsCacheKey(blank) = %q, want %q", got, searchChannelsCacheKeyPrefix+"empty")
	}
}

func TestNormalizeStreamOrg_TrimsSpaceAndBang(t *testing.T) {
	t.Parallel()

	if got := normalizeStreamOrg("  Hololive! "); got != "hololive" {
		t.Fatalf("normalizeStreamOrg() = %q, want %q", got, "hololive")
	}
}
