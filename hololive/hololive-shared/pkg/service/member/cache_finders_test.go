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
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 전체 멤버 적재 실패는 빈 결과가 아니라 오류다.
func TestCache_LoadAllMembers_ReturnsError(t *testing.T) {
	finder := &Cache{}

	_, err := finder.LoadAllMembers(t.Context())
	if err == nil {
		t.Fatal("LoadAllMembers() error = nil, want non-nil")
	}

	if got := err.Error(); got != "member repository is nil" {
		t.Fatalf("LoadAllMembers() error = %q, want %q", got, "member repository is nil")
	}
}

// 캐시 없이 만든 어댑터는 어떤 조회도 미존재로 꾸미지 않고 구성 오류를 돌려준다.
func TestCache_FindMembersByName_MatchesLocalizedNames(t *testing.T) {
	cache := newAdapterTestCache(
		&domain.Member{ID: 1, ChannelID: "suisei", Name: "Suisei", NameKo: "별빛"},
		&domain.Member{ID: 2, ChannelID: "hoshino", Name: "별빛", NameJa: "ほしの"},
		&domain.Member{ID: 3, ChannelID: testMemberMikoSlug, Name: testMemberMiko, NameJa: "みこ"},
	)
	finder := cache

	got, err := finder.FindMembersByName(t.Context(), "  별빛 ")
	if err != nil || len(got) != 2 {
		t.Fatalf("FindMembersByName() = %+v, %v; want 2 members", got, err)
	}

	if got[0] == nil || got[0].ChannelID != "suisei" {
		t.Fatalf("FindMembersByName()[0] = %+v, want suisei", got[0])
	}

	if got[1] == nil || got[1].ChannelID != "hoshino" {
		t.Fatalf("FindMembersByName()[1] = %+v, want hoshino", got[1])
	}

	got[0] = nil

	again, err := finder.FindMembersByName(t.Context(), "별빛")
	if err != nil || len(again) != 2 || again[0] == nil || again[0].ChannelID != "suisei" {
		t.Fatalf("FindMembersByName() should return cloned slice, got %+v, %v", again, err)
	}

	none, err := finder.FindMembersByName(t.Context(), "없는이름")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("FindMembersByName(missing) = %#v, %v; want empty non-nil slice and nil error", none, err)
	}
}

func TestCache_FindMembersByAlias_ReturnsAllAliasMatches(t *testing.T) {
	cache := newAdapterTestCache(
		&domain.Member{
			ID:        1,
			ChannelID: "aqua",
			Name:      "Aqua",
			Aliases:   &domain.Aliases{Ko: []string{"Aqua"}},
		},
		&domain.Member{
			ID:        2,
			ChannelID: "marine",
			Name:      "Marine",
			Aliases:   &domain.Aliases{Ja: []string{" aqua "}},
		},
		&domain.Member{
			ID:        3,
			ChannelID: "pekora",
			Name:      testMemberPekora,
			Aliases:   &domain.Aliases{Ko: []string{"Usada"}},
		},
	)
	finder := cache

	got, err := finder.FindMembersByAlias(t.Context(), "  AQUA ")
	if err != nil || len(got) != 2 {
		t.Fatalf("FindMembersByAlias() = %+v, %v; want 2 members", got, err)
	}

	if got[0] == nil || got[0].ChannelID != "aqua" {
		t.Fatalf("FindMembersByAlias()[0] = %+v, want aqua", got[0])
	}

	if got[1] == nil || got[1].ChannelID != "marine" {
		t.Fatalf("FindMembersByAlias()[1] = %+v, want marine", got[1])
	}

	got[1] = nil

	again, err := finder.FindMembersByAlias(t.Context(), "aqua")
	if err != nil || len(again) != 2 || again[1] == nil || again[1].ChannelID != "marine" {
		t.Fatalf("FindMembersByAlias() should return cloned slice, got %+v, %v", again, err)
	}
}

// 다건 조회는 snapshot 적재 실패를 빈 결과로 바꾸지 않는다. 공백뿐인 질의만 적재 없이 빈 결과다.
func TestCache_MultiFinderPropagatesLoadFailure(t *testing.T) {
	wantErr := errors.New("members table unavailable")

	var calls atomic.Int64

	cache := &Cache{
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return nil, wantErr
		},
	}
	finder := cache

	if got, err := finder.FindMembersByName(t.Context(), " \t"); err != nil || len(got) != 0 || calls.Load() != 0 {
		t.Fatalf("blank query = %+v, %v, loads %d; want empty without loading", got, err, calls.Load())
	}

	if got, err := finder.FindMembersByName(t.Context(), testMemberMiko); !errors.Is(err, wantErr) || got != nil {
		t.Fatalf("FindMembersByName() = %+v, %v; want load failure", got, err)
	}

	if got, err := finder.FindMembersByAlias(t.Context(), "미코"); !errors.Is(err, wantErr) || got != nil {
		t.Fatalf("FindMembersByAlias() = %+v, %v; want load failure", got, err)
	}
}

// Epoch이 불확실해 PostgreSQL을 직접 읽는 우회 경로는 공유 적재가 아니므로 호출자 취소를 그대로 따른다.
func TestCache_BypassHonorsCallerCancellation(t *testing.T) {
	cache := newEpochTestCache(&fakeMemberEpochAuthority{epoch: 1})
	cache.authorityHealthy.Store(false)

	started := make(chan struct{})

	cache.loadAllMembers = func(ctx context.Context) ([]*domain.Member, error) {
		close(started)
		<-ctx.Done()

		return nil, ctx.Err()
	}

	finder := cache
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		_, err := finder.FindMembersByName(ctx, testMemberMiko)
		done <- err
	}()

	<-started
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("bypass error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bypass load ignored caller cancellation")
	}
}

// 우회 경로의 다건 조회도 snapshot 색인과 같은 규칙으로 답하고, 결과를 게시하지 않는다.
func TestCache_BypassUsesSameSearchRules(t *testing.T) {
	cache := newEpochTestCache(&fakeMemberEpochAuthority{epoch: 1})
	cache.authorityHealthy.Store(false)

	cache.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{
			{ID: 1, Name: testMemberMiko, Aliases: &domain.Aliases{Ko: []string{"엘리트"}}},
			{ID: 2, Name: "MIKO ", Aliases: &domain.Aliases{Ja: []string{" 엘리트 "}}},
		}, nil
	}

	finder := cache

	names, err := finder.FindMembersByName(t.Context(), " miko")
	if err != nil || len(names) != 2 {
		t.Fatalf("bypass FindMembersByName() = %+v, %v; want both same-name members", names, err)
	}

	aliases, err := finder.FindMembersByAlias(t.Context(), "엘리트")
	if err != nil || len(aliases) != 2 {
		t.Fatalf("bypass FindMembersByAlias() = %+v, %v; want both shared-alias members", aliases, err)
	}

	if cache.allMembersSnapshot.Load() != nil {
		t.Fatal("bypass published a snapshot while epoch authority was uncertain")
	}
}

func newAdapterTestCache(members ...*domain.Member) *Cache {
	cache := &Cache{logger: slog.New(slog.DiscardHandler)}
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
