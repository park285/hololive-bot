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
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCacheInvalidateAll_WithoutValkeyStillClearsMemory(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	member := &domain.Member{ID: 1, Name: testMemberMikoSlug, ChannelID: "UC_2"}
	c := &Cache{
		logger: logger,
	}
	c.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{member}, 0, time.Now()))
	c.cacheMember(&domain.Member{ID: 2, Name: "point", ChannelID: "UC_3"}, 0, true)
	c.cacheChannelIDs([]string{"UC_3"}, 0)

	if err := c.InvalidateAll(ctx); err != nil {
		t.Fatalf("InvalidateAll failed: %v", err)
	}

	if got, _ := c.lookupPointInMemory(pointLookupChannel, member.ChannelID); got != nil {
		t.Fatalf("snapshot channel served after invalidation: %+v", got)
	}

	if got, _ := c.lookupPointInMemory(pointLookupName, "point"); got != nil {
		t.Fatalf("overlay name served after invalidation: %+v", got)
	}

	if _, _, ok := c.channelIDsInMemory(); ok {
		t.Fatal("channel IDs served after invalidation")
	}
}

// snapshot 다건 조회, point overlay 기록, 무효화가 겹쳐도 데이터 경쟁 없이 각 generation 경계를 지킨다. 무효화 뒤에
// 이전 generation으로 기록한 point 결과는 응답하지 않는다.
func TestCacheSearchOverlayAndInvalidationRace(t *testing.T) {
	members := []*domain.Member{
		{ID: 1, Name: testMemberMiko, ChannelID: "UC_miko", Aliases: &domain.Aliases{Ko: []string{"엘리트"}}},
		{ID: 2, Name: testMemberMikoSlug, ChannelID: "UC_other", Aliases: &domain.Aliases{Ja: []string{"エリート"}}},
	}
	c := &Cache{
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Nanosecond,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			return members, nil
		},
	}

	var wg sync.WaitGroup

	for range 4 {
		wg.Go(func() {
			for range 200 {
				got, err := c.MembersByName(t.Context(), " MIKO ")
				if err != nil || len(got) != 2 {
					t.Errorf("MembersByName() = %+v, %v; want both same-name members", got, err)

					return
				}
			}
		})
	}

	wg.Go(func() {
		for i := range 200 {
			_, generation := c.lookupPointInMemory(pointLookupName, "Point")
			c.cacheMember(&domain.Member{ID: 100 + i, Name: "Point", ChannelID: "UC_point"}, generation, true)
			c.lookupPointInMemory(pointLookupChannel, "UC_point")
		}
	})

	wg.Go(func() {
		for range 200 {
			if err := c.InvalidateAll(t.Context()); err != nil {
				t.Errorf("InvalidateAll() error = %v", err)

				return
			}
		}
	})

	wg.Wait()

	_, staleGeneration := c.lookupPointInMemory(pointLookupName, "Late")

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	c.cacheMember(&domain.Member{ID: 999, Name: "Late", ChannelID: "UC_late"}, staleGeneration, true)

	if got, _ := c.lookupPointInMemory(pointLookupName, "Late"); got != nil {
		t.Fatalf("late point load from an invalidated generation was served: %+v", got)
	}
}
