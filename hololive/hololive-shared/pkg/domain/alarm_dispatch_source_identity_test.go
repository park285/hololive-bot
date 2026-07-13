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

package domain

import (
	"strings"
	"testing"
)

func youtubeIdentityPayload(contentIDs ...string) *YouTubeOutboxDispatchPayload {
	items := make([]YouTubeOutboxItem, 0, len(contentIDs))
	for _, contentID := range contentIDs {
		items = append(items, YouTubeOutboxItem{ContentID: contentID, Payload: `{}`})
	}
	return &YouTubeOutboxDispatchPayload{
		Kind:      OutboxKindNewVideo,
		AlarmType: AlarmTypeLive,
		ChannelID: "UC_test",
		Items:     items,
	}
}

func mustYouTubeIdentity(t *testing.T, payload *YouTubeOutboxDispatchPayload) string {
	t.Helper()
	identity, err := payload.CanonicalIdentity()
	if err != nil {
		t.Fatalf("CanonicalIdentity() error = %v", err)
	}
	return identity
}

func TestYouTubeOutboxIdentityIsUnambiguous(t *testing.T) {
	first := mustYouTubeIdentity(t, youtubeIdentityPayload("a,b", "c"))
	second := mustYouTubeIdentity(t, youtubeIdentityPayload("a", "b,c"))
	if first == second {
		t.Fatalf("ambiguous item sets share identity %q", first)
	}
}

func TestYouTubeOutboxIdentityDeduplicatesSortsAndTrims(t *testing.T) {
	canonical := mustYouTubeIdentity(t, youtubeIdentityPayload("a", "b"))
	for _, payload := range []*YouTubeOutboxDispatchPayload{
		youtubeIdentityPayload("b", "a"),
		youtubeIdentityPayload("a", "b", "a"),
		youtubeIdentityPayload("  b  ", " a "),
	} {
		if got := mustYouTubeIdentity(t, payload); got != canonical {
			t.Fatalf("CanonicalIdentity() = %q, want %q", got, canonical)
		}
	}
	if !strings.HasPrefix(canonical, "sha256:") || len(canonical) != len("sha256:")+64 {
		t.Fatalf("CanonicalIdentity() = %q, want fixed SHA-256 identity", canonical)
	}
}

func TestYouTubeOutboxIdentityRejectsItemCountBeforeCanonicalAllocation(t *testing.T) {
	payload := youtubeIdentityPayload(make([]string, maxYouTubeOutboxIdentityItems+1)...)
	if err := payload.Validate(); err == nil || !strings.Contains(err.Error(), "too many items") {
		t.Fatalf("Validate() error = %v, want item-count bound", err)
	}
	if identity, err := payload.CanonicalIdentity(); err == nil || identity != "" {
		t.Fatalf("CanonicalIdentity() = %q, %v, want bounded rejection", identity, err)
	}
}

func TestYouTubeOutboxIdentityRejectsOversizedOrEmptyContentID(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		want      string
	}{
		{name: "empty", contentID: "  ", want: "empty"},
		{name: "oversized", contentID: strings.Repeat("x", maxYouTubeOutboxContentIDBytes+1), want: "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := youtubeIdentityPayload(tt.contentID)
			if err := payload.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
			if got := payload.Identity(); got != "" {
				t.Fatalf("Identity() = %q, want empty on invalid payload", got)
			}
		})
	}
}
