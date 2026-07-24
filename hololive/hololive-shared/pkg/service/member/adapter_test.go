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

package member

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func testAdapterLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type adapterContextKey struct{}

func TestNewMemberServiceAdapter_PreservesCancellation(t *testing.T) {
	parent := context.WithValue(context.Background(), adapterContextKey{}, "value")
	ctx, cancel := context.WithCancel(parent)
	cancel()

	adapter := NewMemberServiceAdapter(ctx, nil, testAdapterLogger())
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
	if err := adapter.ctx.Err(); err != context.Canceled {
		t.Fatalf("adapter ctx should preserve cancellation, got err=%v", err)
	}
	if got := adapter.ctx.Value(adapterContextKey{}); got != "value" {
		t.Fatalf("adapter ctx should preserve values, got=%v", got)
	}
}

func TestNewMemberServiceAdapter_NilContextUsesBackground(t *testing.T) {
	var nilCtx context.Context

	adapter := NewMemberServiceAdapter(nilCtx, nil, nil)
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
	if adapter.ctx == nil {
		t.Fatal("adapter ctx is nil")
	}
	if err := adapter.ctx.Err(); err != nil {
		t.Fatalf("adapter ctx err = %v, want nil", err)
	}
	if adapter.logger == nil {
		t.Fatal("adapter logger is nil")
	}
}

func TestServiceAdapter_WithContext_UsesProvidedContext(t *testing.T) {
	adapter := NewMemberServiceAdapter(context.Background(), nil, testAdapterLogger())

	child, cancel := context.WithCancel(context.Background())
	cancel()

	derived, ok := adapter.WithContext(child).(*ServiceAdapter)
	if !ok {
		t.Fatal("WithContext should return *ServiceAdapter")
	}
	if err := derived.ctx.Err(); err != context.Canceled {
		t.Fatalf("derived ctx should preserve provided cancellation, got=%v", err)
	}
}

func TestServiceAdapter_LoadAllMembers_ExplicitlyReturnsErrorWhileLegacyGetterReturnsNil(t *testing.T) {
	adapter := NewMemberServiceAdapter(context.Background(), &Cache{}, testAdapterLogger())

	members := adapter.GetAllMembers()
	if members != nil {
		t.Fatalf("GetAllMembers() = %+v, want nil", members)
	}

	_, err := adapter.LoadAllMembers()
	if err == nil {
		t.Fatal("LoadAllMembers() error = nil, want non-nil")
	}
	if got := err.Error(); got != "member repository is nil" {
		t.Fatalf("LoadAllMembers() error = %q, want %q", got, "member repository is nil")
	}
}

func TestServiceAdapter_FindMembersByName_MatchesLocalizedNames(t *testing.T) {
	cache := newAdapterTestCache(
		&domain.Member{ChannelID: "suisei", Name: "Suisei", NameKo: "별빛"},
		&domain.Member{ChannelID: "hoshino", Name: "별빛", NameJa: "ほしの"},
		&domain.Member{ChannelID: "miko", Name: "Miko", NameJa: "みこ"},
	)
	adapter := NewMemberServiceAdapter(context.Background(), cache, testAdapterLogger())

	got := adapter.FindMembersByName("  별빛 ")
	if len(got) != 2 {
		t.Fatalf("FindMembersByName() len = %d, want 2", len(got))
	}
	if got[0] == nil || got[0].ChannelID != "suisei" {
		t.Fatalf("FindMembersByName()[0] = %+v, want suisei", got[0])
	}
	if got[1] == nil || got[1].ChannelID != "hoshino" {
		t.Fatalf("FindMembersByName()[1] = %+v, want hoshino", got[1])
	}

	got[0] = nil
	again := adapter.FindMembersByName("별빛")
	if len(again) != 2 || again[0] == nil || again[0].ChannelID != "suisei" {
		t.Fatalf("FindMembersByName() should return cloned slice, got %+v", again)
	}
}

func TestServiceAdapter_FindMembersByAlias_ReturnsAllAliasMatches(t *testing.T) {
	cache := newAdapterTestCache(
		&domain.Member{
			ChannelID: "aqua",
			Name:      "Aqua",
			Aliases:   &domain.Aliases{Ko: []string{"Aqua"}},
		},
		&domain.Member{
			ChannelID: "marine",
			Name:      "Marine",
			Aliases:   &domain.Aliases{Ja: []string{" aqua "}},
		},
		&domain.Member{
			ChannelID: "pekora",
			Name:      "Pekora",
			Aliases:   &domain.Aliases{Ko: []string{"Usada"}},
		},
	)
	adapter := NewMemberServiceAdapter(context.Background(), cache, testAdapterLogger())

	got := adapter.FindMembersByAlias("  AQUA ")
	if len(got) != 2 {
		t.Fatalf("FindMembersByAlias() len = %d, want 2", len(got))
	}
	if got[0] == nil || got[0].ChannelID != "aqua" {
		t.Fatalf("FindMembersByAlias()[0] = %+v, want aqua", got[0])
	}
	if got[1] == nil || got[1].ChannelID != "marine" {
		t.Fatalf("FindMembersByAlias()[1] = %+v, want marine", got[1])
	}

	got[1] = nil
	again := adapter.FindMembersByAlias("aqua")
	if len(again) != 2 || again[1] == nil || again[1].ChannelID != "marine" {
		t.Fatalf("FindMembersByAlias() should return cloned slice, got %+v", again)
	}
}

func newAdapterTestCache(members ...*domain.Member) *Cache {
	cache := &Cache{}
	snapshot := make([]*domain.Member, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		snapshot = append(snapshot, member)
	}
	cache.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return snapshot, nil
	}
	return cache
}
