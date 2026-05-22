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

package statscollector

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDetermineMemberName(t *testing.T) {
	service := &StatsService{}
	lookup := map[string]string{"ch1": "lookup"}
	prev := &domain.TimestampedStats{MemberName: "prev"}

	if got := service.determineMemberName("ch1", prev, lookup); got != "lookup" {
		t.Fatalf("expected lookup name, got %s", got)
	}
	if got := service.determineMemberName("ch2", prev, lookup); got != "prev" {
		t.Fatalf("expected prev name, got %s", got)
	}
	if got := service.determineMemberName("ch2", nil, nil); got != "" {
		t.Fatalf("expected empty name, got %s", got)
	}
}

func TestSaveCurrentStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &StatsService{logger: logger}

	item := &youtube.Channel{
		Id: "ch1",
		Statistics: &youtube.ChannelStatistics{
			SubscriberCount: 10,
			VideoCount:      2,
			ViewCount:       3,
		},
	}
	prev := &domain.TimestampedStats{SubscriberCount: 7}

	change := service.saveCurrentStats(context.Background(), item, "name", prev)
	if change != 3 {
		t.Fatalf("expected subscriber change 3, got %d", change)
	}
}
