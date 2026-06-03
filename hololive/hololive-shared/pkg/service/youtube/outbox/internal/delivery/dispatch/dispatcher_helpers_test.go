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

package dispatch

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

func TestBuildTemplateData(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	tests := []struct {
		name      string
		item      domain.YouTubeNotificationOutbox
		wantURL   string
		wantTitle string
		wantPost  string
		wantMil   string
		wantErr   bool
	}{
		{
			name: "video payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewVideo,
				Payload: `{"video_id":"vid1","title":"영상1"}`,
			},
			wantURL:   "https://youtu.be/vid1",
			wantTitle: "영상1",
		},
		{
			name: "short payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewShort,
				Payload: `{"video_id":"short1","title":"쇼츠1"}`,
			},
			wantURL:   "https://www.youtube.com/shorts/short1",
			wantTitle: "쇼츠1",
		},
		{
			name: "community payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindCommunityPost,
				Payload: `{"post_id":"post1","content_text":"내용"}`,
			},
			wantURL:  "https://www.youtube.com/post/post1",
			wantPost: "post1",
		},
		{
			name: "milestone payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindMilestone,
				Payload: `{"milestone":"100만"}`,
			},
			wantMil: "100만",
		},
		{
			name: "invalid payload",
			item: domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewVideo,
				Payload: `{invalid-json}`,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := mf.buildTemplateData("멤버", tt.item)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.MemberName != "멤버" || got.URL != tt.wantURL || got.Title != tt.wantTitle || got.PostID != tt.wantPost || got.Milestone != tt.wantMil {
				t.Fatalf("unexpected template data: %#v", got)
			}
		})
	}
}

func TestFormatMessageFallbackFailures(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}

	if _, err := mf.formatMessageFallback("멤버", domain.YouTubeNotificationOutbox{Kind: domain.OutboxKind("UNKNOWN")}); err == nil {
		t.Fatalf("expected unknown kind error")
	}

	if _, err := mf.formatMessageFallback("멤버", domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, Payload: "{"}); err == nil {
		t.Fatalf("expected invalid payload error")
	}
}

func TestFormatYouTubeOutboxPayloadMatchesSingleFallbackFormatter(t *testing.T) {
	t.Parallel()

	formatter := &MessageFormatter{}
	item := domain.YouTubeNotificationOutbox{
		ID:        1,
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "UC_test",
		ContentID: "short:abc",
		Payload:   `{"video_id":"abc","title":"테스트 쇼츠"}`,
	}
	want, err := formatter.formatMessageFallback("멤버", item)
	if err != nil {
		t.Fatalf("formatMessageFallback() error = %v", err)
	}

	got, err := FormatYouTubeOutboxPayload(context.Background(), domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1},
		Kind:       domain.OutboxKindNewShort,
		AlarmType:  domain.AlarmTypeShorts,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  item.ID,
			ContentID: item.ContentID,
			Payload:   item.Payload,
		}},
	})
	if err != nil {
		t.Fatalf("FormatYouTubeOutboxPayload() error = %v", err)
	}
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestFormatYouTubeOutboxPayloadGroupedFallback(t *testing.T) {
	t.Parallel()

	got, err := FormatYouTubeOutboxPayload(context.Background(), domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  []int64{1, 2},
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UC_test",
		MemberName: "멤버",
		Items: []domain.YouTubeOutboxItem{
			{OutboxID: 1, ContentID: "post-a", Payload: `{"post_id":"post-a","content_text":"첫 글"}`},
			{OutboxID: 2, ContentID: "post-b", Payload: `{"post_id":"post-b","content_text":"둘째 글"}`},
		},
	})
	if err != nil {
		t.Fatalf("FormatYouTubeOutboxPayload() error = %v", err)
	}
	for _, want := range []string{
		"📝 멤버 커뮤니티 알림 (2개)",
		"첫 글",
		"https://www.youtube.com/post/post-a",
		"둘째 글",
		"https://www.youtube.com/post/post-b",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("message %q does not contain %q", got, want)
		}
	}
}

func TestTruncateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{name: "short text unchanged", in: "abc", maxLen: 5, want: "abc"},
		{name: "exact length unchanged", in: "hello", maxLen: 5, want: "hello"},
		{name: "ascii truncated", in: "hello world", maxLen: 8, want: "hello..."},
		{name: "unicode truncated", in: "안녕하세요세계", maxLen: 6, want: "안녕하..."},
		{name: "minimum ellipsis", in: "abcdef", maxLen: 3, want: "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deliverysql.TruncateString(tt.in, tt.maxLen); got != tt.want {
				t.Fatalf("deliverysql.TruncateString(%q, %d) = %q, want %q", tt.in, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestGetGroupedTemplateKeyAndHeader(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	tests := []struct {
		name       string
		kind       domain.OutboxKind
		wantKey    domain.TemplateKey
		wantPrefix string
	}{
		{name: "video", kind: domain.OutboxKindNewVideo, wantKey: domain.TemplateKeyOutboxVideoGroup, wantPrefix: "📺 멤버 새 영상 (2개)"},
		{name: "short", kind: domain.OutboxKindNewShort, wantKey: domain.TemplateKeyOutboxShortsGroup, wantPrefix: "📱 멤버 쇼츠 알림 (2개)"},
		{name: "community", kind: domain.OutboxKindCommunityPost, wantKey: domain.TemplateKeyOutboxCommunityGroup, wantPrefix: "📝 멤버 커뮤니티 알림 (2개)"},
		{name: "fallback", kind: domain.OutboxKind("OTHER"), wantKey: domain.TemplateKeyOutboxVideoGroup, wantPrefix: "🔔 멤버 알림 (2개)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotKey, gotHeader := mf.getGroupedTemplateKeyAndHeader("멤버", tt.kind, 2)
			if gotKey != tt.wantKey || gotHeader != tt.wantPrefix {
				t.Fatalf("getGroupedTemplateKeyAndHeader() = (%s, %q), want (%s, %q)", gotKey, gotHeader, tt.wantKey, tt.wantPrefix)
			}
		})
	}
}

func TestBuildGroupedTemplateData(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	items := []domain.YouTubeNotificationOutbox{
		{Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		{Kind: domain.OutboxKindNewShort, Payload: `{invalid}`},
		{Kind: domain.OutboxKindCommunityPost, Payload: `{"post_id":"p1","content_text":"내용"}`},
	}

	got := mf.buildGroupedTemplateData("멤버", domain.OutboxKindNewVideo, items)
	if got.MemberName != "멤버" || got.Count != 3 || len(got.Items) != 3 {
		t.Fatalf("unexpected grouped template header: %#v", got)
	}

	if got.Items[0].Title != "영상1" || got.Items[0].URL != "https://youtu.be/v1" {
		t.Fatalf("unexpected first item: %#v", got.Items[0])
	}
	if got.Items[1].Title != "" || got.Items[1].URL != "" {
		t.Fatalf("expected invalid payload item to stay empty: %#v", got.Items[1])
	}
	if got.Items[2].ContentText != "내용" || got.Items[2].URL != "https://www.youtube.com/post/p1" {
		t.Fatalf("unexpected third item: %#v", got.Items[2])
	}
}

func TestGroupOutboxItems(t *testing.T) {
	t.Parallel()

	grouper := newOutboxGrouper(nil, nil, nil, Config{})
	items := []domain.YouTubeNotificationOutbox{
		{ID: 1, ChannelID: "ch1", Kind: domain.OutboxKindNewVideo},
		{ID: 2, ChannelID: "ch1", Kind: domain.OutboxKindNewVideo},
		{ID: 3, ChannelID: "ch1", Kind: domain.OutboxKindNewShort},
		{ID: 4, ChannelID: "ch2", Kind: domain.OutboxKindNewVideo}, // no rooms
	}
	roomsByChannel := map[string]channelAlarmRoomTargets{
		"ch1": {
			domain.AlarmTypeLive:   {"room1": true, "room2": true},
			domain.AlarmTypeShorts: {"room1": true, "room2": true},
		},
	}

	groups := grouper.groupOutboxItems(items, roomsByChannel)
	if len(groups) != 4 {
		t.Fatalf("groupOutboxItems count = %d, want 4", len(groups))
	}

	summary := make(map[string]int)
	for _, g := range groups {
		key := strings.Join([]string{g.roomID, g.channelID, string(g.kind)}, "|")
		summary[key] = len(g.items)
	}

	want := map[string]int{
		"room1|ch1|NEW_VIDEO": 2,
		"room2|ch1|NEW_VIDEO": 2,
		"room1|ch1|NEW_SHORT": 1,
		"room2|ch1|NEW_SHORT": 1,
	}
	if !reflect.DeepEqual(summary, want) {
		t.Fatalf("group summary = %#v, want %#v", summary, want)
	}
}

func TestCollectOutboxIDs(t *testing.T) {
	t.Parallel()

	items := []domain.YouTubeNotificationOutbox{
		{ID: 10},
		{ID: 20},
		{ID: 30},
	}

	got := collectOutboxIDs(items)
	want := []int64{10, 20, 30}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectOutboxIDs() = %#v, want %#v", got, want)
	}

	if out := collectOutboxIDs(nil); out != nil {
		t.Fatalf("collectOutboxIDs(nil) = %#v, want nil", out)
	}
}

func TestUniqueInt64s(t *testing.T) {
	t.Parallel()

	in := []int64{1, 2, 1, 3, 2, 4, 4}
	got := deliverysql.UniqueInt64s(in)
	want := []int64{1, 2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deliverysql.UniqueInt64s() = %#v, want %#v", got, want)
	}

	if out := deliverysql.UniqueInt64s(nil); out != nil {
		t.Fatalf("deliverysql.UniqueInt64s(nil) = %#v, want nil", out)
	}
}

func TestFormatGroupedMessageErrors(t *testing.T) {
	t.Parallel()

	mf := &MessageFormatter{}
	if _, err := mf.formatGroupedMessage(context.Background(), "멤버", "ch1", domain.OutboxKindNewVideo, nil); err == nil {
		t.Fatalf("expected empty items error")
	}
	if _, err := mf.formatGroupedMessage(context.Background(), "멤버", "ch1", domain.OutboxKindNewVideo, []domain.YouTubeNotificationOutbox{{}}); err == nil {
		t.Fatalf("expected nil renderer error")
	}
}
