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

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

type liveStreamProviderStub struct {
	liveStreams []*domain.Stream
}

func (s *liveStreamProviderStub) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	return s.liveStreams, nil
}

func (s *liveStreamProviderStub) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *liveStreamProviderStub) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *liveStreamProviderStub) GetChannel(context.Context, string) (*domain.Channel, error) {
	return nil, nil
}

func TestBuildChzzkLiveStreams(t *testing.T) {
	t.Parallel()

	streams := buildChzzkLiveStreams(
		[]*domain.Member{
			{
				ChannelID:      "yt-1",
				Name:           "미코",
				ChzzkChannelID: "cz-1",
			},
			{
				ChannelID:      "yt-2",
				Name:           "졸업멤버",
				ChzzkChannelID: "cz-2",
				IsGraduated:    true,
			},
		},
		[]chzzk.LiveData{
			{ChannelID: "cz-1", LiveTitle: "치지직 방송"},
			{ChannelID: "cz-2", LiveTitle: "보이면 안됨"},
			{ChannelID: "unknown", LiveTitle: "무시"},
		},
	)

	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}

	if streams[0].ChannelID != "yt-1" {
		t.Fatalf("channel_id = %q, want yt-1", streams[0].ChannelID)
	}

	if streams[0].Title != "치지직 방송" {
		t.Fatalf("title = %q, want 치지직 방송", streams[0].Title)
	}

	if !streams[0].IsChzzkOnly {
		t.Fatal("expected chzzk-only stream")
	}
}

func TestCollectChzzkLiveStreams_UsesBatchResult(t *testing.T) {
	t.Parallel()

	members := []*domain.Member{
		{ChannelID: "yt-1", Name: "미코", ChzzkChannelID: "cz-1"},
		{ChannelID: "yt-2", Name: "스이세이", ChzzkChannelID: "cz-2"},
	}

	batchCalls := 0

	streams := collectChzzkLiveStreams(
		members,
		func(channelIDs []string) ([]chzzk.LiveData, error) {
			batchCalls++

			if len(channelIDs) != 2 {
				t.Fatalf("unexpected channelIDs: %#v", channelIDs)
			}

			return []chzzk.LiveData{
				{ChannelID: "cz-1", LiveTitle: "batch-live-1"},
				{ChannelID: "cz-2", LiveTitle: "batch-live-2"},
			}, nil
		},
	)

	if batchCalls != 1 {
		t.Fatalf("batchCalls = %d, want 1", batchCalls)
	}

	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}

	if streams[0].Title != "batch-live-1" {
		t.Fatalf("title = %q, want batch-live-1", streams[0].Title)
	}

	if streams[1].Title != "batch-live-2" {
		t.Fatalf("title = %q, want batch-live-2", streams[1].Title)
	}
}

func TestCollectChzzkLiveStreams_ReturnsNilOnBatchError(t *testing.T) {
	t.Parallel()

	streams := collectChzzkLiveStreams(
		[]*domain.Member{
			{ChannelID: "yt-1", Name: "미코", ChzzkChannelID: "cz-1"},
		},
		func([]string) ([]chzzk.LiveData, error) {
			return nil, errors.New("batch failed")
		},
	)

	if streams != nil {
		t.Fatalf("expected nil streams on batch error, got %#v", streams)
	}
}

func TestLiveCommand_MemberLookupPropagatesRequestContextToMatcher(t *testing.T) {
	t.Parallel()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: "ch-aqua",
		Name:      "Aqua",
	}})
	deps := &Dependencies{
		Holodex: &liveStreamProviderStub{},
		//lint:ignore SA1012 nil base context is the behavior under test; Execute must supply ctx.
		Matcher:   matcher.NewMatcher(nil, memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error {
			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected send error")
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	ctx := context.WithValue(t.Context(), testContextKey("request-id"), "live-propagation")

	err := NewLiveCommand(deps).Execute(ctx, &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !memberProvider.ctxCapture.saw(ctx) {
		t.Fatal("expected matcher provider to receive request context")
	}
}
