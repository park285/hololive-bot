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
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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
		_, err := finder.LoadAllMembers(ctx)
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

// 우회 경로의 전체 멤버 조회는 PostgreSQL 결과로 답하되 snapshot을 게시하지 않는다.
func TestCache_BypassDoesNotPublishSnapshot(t *testing.T) {
	cache := newEpochTestCache(&fakeMemberEpochAuthority{epoch: 1})
	cache.authorityHealthy.Store(false)

	cache.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{
			{ID: 1, Name: testMemberMiko},
			{ID: 2, Name: testMemberPekora},
		}, nil
	}

	members, err := cache.LoadAllMembers(t.Context())
	if err != nil || len(members) != 2 {
		t.Fatalf("bypass LoadAllMembers() = %+v, %v; want both members", members, err)
	}

	if cache.allMembersSnapshot.Load() != nil {
		t.Fatal("bypass published a snapshot while epoch authority was uncertain")
	}
}
