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

package scraping

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testStateStore struct {
	mu   sync.Mutex
	data map[string]stateEntry
}

type stateEntry struct {
	value bool
	until time.Time
}

func newTestStateStore() *testStateStore {
	return &testStateStore{
		data: make(map[string]stateEntry),
	}
}

func (s *testStateStore) Get(_ context.Context, key string, dest any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.data[key]
	if !ok || time.Now().After(entry.until) {
		delete(s.data, key)
		return nil
	}
	if out, ok := dest.(*bool); ok {
		*out = entry.value
	}
	return nil
}

func (s *testStateStore) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := value.(bool)
	if !ok {
		return fmt.Errorf("state value has type %T, want bool", value)
	}
	s.data[key] = stateEntry{
		value: v,
		until: time.Now().Add(ttl),
	}
	return nil
}

func (s *testStateStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func TestCommunityMissingState(t *testing.T) {
	ctx := context.Background()
	client := NewClient()

	require.False(t, client.isCommunityMissing(ctx, "UC_TEST"))

	client.markCommunityMissing(ctx, "UC_TEST")
	require.True(t, client.isCommunityMissing(ctx, "UC_TEST"))

	client.communityMissing.mu.Lock()
	client.communityMissing.until["UC_TEST"] = time.Now().Add(-time.Second)
	client.communityMissing.mu.Unlock()

	require.False(t, client.isCommunityMissing(ctx, "UC_TEST"))
}

func TestVideoRSSBackoffState(t *testing.T) {
	ctx := context.Background()
	client := NewClient()

	require.False(t, client.isVideoRSSBackoff(ctx, "UC_TEST"))

	client.markVideoRSSBackoff(ctx, "UC_TEST")
	require.True(t, client.isVideoRSSBackoff(ctx, "UC_TEST"))

	client.clearVideoRSSBackoff(ctx, "UC_TEST")
	require.False(t, client.isVideoRSSBackoff(ctx, "UC_TEST"))
}

func TestStateStorePersistsAcrossClientInstances(t *testing.T) {
	ctx := context.Background()
	store := newTestStateStore()

	clientA := NewClient(WithStateStore(store))
	clientA.markCommunityMissing(ctx, "UC_TEST")
	clientA.markVideoRSSBackoff(ctx, "UC_TEST")
	require.Greater(t, ytDefaults.CommunityMissingTTL, time.Duration(0))
	require.Greater(t, ytDefaults.VideoRSSBackoffTTL, time.Duration(0))
	require.Len(t, store.data, 2)
	require.Contains(t, store.data, clientA.communityMissingStateKey("UC_TEST"))
	require.Contains(t, store.data, clientA.videoRSSBackoffStateKey("UC_TEST"))

	clientB := NewClient(WithStateStore(store))
	var communityMarker bool
	require.NoError(t, store.Get(ctx, clientB.communityMissingStateKey("UC_TEST"), &communityMarker))
	require.True(t, communityMarker)
	require.True(t, clientB.isCommunityMissing(ctx, "UC_TEST"))
	require.True(t, clientB.isVideoRSSBackoff(ctx, "UC_TEST"))
}
