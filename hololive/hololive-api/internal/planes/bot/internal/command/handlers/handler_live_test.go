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
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
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

func TestCollectChzzkLiveStreams_ReturnsEmptySliceWhenNoStreams(t *testing.T) {
	t.Parallel()

	streams := collectChzzkLiveStreams(
		[]*domain.Member{
			{ChannelID: "yt-1", Name: "미코", ChzzkChannelID: "cz-1"},
		},
		func([]string) ([]chzzk.LiveData, error) {
			return nil, nil
		},
	)

	if streams == nil {
		t.Fatal("expected non-nil empty stream slice")
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}

type liveImageRendererStub struct {
	pages   [][]byte
	err     error
	entries []render.LiveCardEntry
}

func (s *liveImageRendererStub) RenderLiveImages(entries []render.LiveCardEntry) ([][]byte, error) {
	s.entries = entries
	return s.pages, s.err
}

func liveCardTestDeps(t *testing.T, members []*domain.Member) (*Dependencies, *[][]byte, *[]byte, *string) {
	t.Helper()

	var multiSent [][]byte
	var singleSent []byte
	var textSent string
	deps := &Dependencies{
		Holodex:   &liveStreamProviderStub{},
		Matcher:   matcher.NewMatcher(nilBaseContext(), newContextAwareMemberProvider(members), nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, msg string) error {
			textSent = msg
			return nil
		},
		SendImage: func(_ context.Context, _ string, data []byte, _ ...iris.SendOption) error {
			singleSent = data
			return nil
		},
		SendMultipleImages: func(_ context.Context, _ string, images [][]byte, _ ...iris.SendOption) error {
			multiSent = images
			return nil
		},
		SendError: func(context.Context, string, string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}
	return deps, &multiSent, &singleSent, &textSent
}

func TestLiveCommand_Execute_MultiPageCardUsesSendMultipleImages(t *testing.T) {
	t.Parallel()

	deps, multiSent, singleSent, textSent := liveCardTestDeps(t, []*domain.Member{
		{ChannelID: "ch-pekora", Name: "Usada Pekora", ShortKoreanName: "페코라", Photo: "https://yt3.googleusercontent.com/p=s88-c"},
	})
	deps.Holodex = &liveStreamProviderStub{liveStreams: []*domain.Stream{
		{ChannelID: "ch-pekora", ChannelName: "Usada Pekora", Title: "건축 방송"},
	}}
	renderer := &liveImageRendererStub{pages: [][]byte{[]byte("p1"), []byte("p2")}}

	err := NewLiveCommand(deps, renderer).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(*multiSent) != 2 {
		t.Fatalf("SendMultipleImages pages = %d, want 2", len(*multiSent))
	}
	if *singleSent != nil || *textSent != "" {
		t.Fatalf("single/text paths used: single=%v text=%q", *singleSent != nil, *textSent)
	}
	if len(renderer.entries) != 1 || renderer.entries[0].Name != "페코라" || renderer.entries[0].Photo == "" {
		t.Fatalf("card entries = %#v, want member-resolved name/photo", renderer.entries)
	}
}

func TestLiveCommand_Execute_SinglePageCardUsesSendImage(t *testing.T) {
	t.Parallel()

	deps, multiSent, singleSent, _ := liveCardTestDeps(t, nil)
	deps.Holodex = &liveStreamProviderStub{liveStreams: []*domain.Stream{
		{ChannelID: "ch-x", ChannelName: "Member X", Title: "방송"},
	}}
	renderer := &liveImageRendererStub{pages: [][]byte{[]byte("p1")}}

	err := NewLiveCommand(deps, renderer).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *singleSent == nil {
		t.Fatal("SendImage not called for single page")
	}
	if *multiSent != nil {
		t.Fatal("SendMultipleImages should not be called for single page")
	}
}

func TestLiveCommand_Execute_CardRenderFailureFallsBackToText(t *testing.T) {
	t.Parallel()

	deps, multiSent, singleSent, textSent := liveCardTestDeps(t, nil)
	deps.Holodex = &liveStreamProviderStub{liveStreams: []*domain.Stream{
		{ChannelID: "ch-x", ChannelName: "Member X", Title: "방송"},
	}}
	renderer := &liveImageRendererStub{err: errors.New("render blew up")}

	err := NewLiveCommand(deps, renderer).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *textSent == "" {
		t.Fatal("expected text fallback message")
	}
	if *singleSent != nil || *multiSent != nil {
		t.Fatal("image paths should not be used on render failure")
	}
}

func TestLiveCommand_MemberLookupPropagatesRequestContextToMatcher(t *testing.T) {
	t.Parallel()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: "ch-aqua",
		Name:      "Aqua",
	}})
	deps := &Dependencies{
		Holodex:   &liveStreamProviderStub{},
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
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

	err := NewLiveCommand(deps, nil).Execute(ctx, &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Aqua",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !memberProvider.ctxCapture.saw(ctx) {
		t.Fatal("expected matcher provider to receive request context")
	}
}
