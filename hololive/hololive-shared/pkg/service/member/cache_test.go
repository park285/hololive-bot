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
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestCacheInvalidateAll_UsesScanKeysAndDelMany(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	member := &domain.Member{Name: "pekora", ChannelID: "UC_1"}
	cache := cachemocks.NewStrictClient()

	var scannedPattern string
	var scannedBatchSize int64
	cache.ScanKeysFunc = func(_ context.Context, pattern string, batchSize int64) ([]string, error) {
		scannedPattern = pattern
		scannedBatchSize = batchSize
		return []string{"member:channel:UC_1", "member:name:pekora"}, nil
	}

	var deletedKeys []string
	cache.DelManyFunc = func(_ context.Context, keys []string) (int64, error) {
		deletedKeys = append([]string(nil), keys...)
		return int64(len(keys)), nil
	}

	c := &Cache{
		cache:  cache,
		logger: logger,
	}
	c.byChannelID.Store(member.ChannelID, member)
	c.byName.Store(member.Name, member)
	c.allMembers.Store(allChannelIDsKey, []string{member.ChannelID})

	if err := c.InvalidateAll(ctx); err != nil {
		t.Fatalf("InvalidateAll failed: %v", err)
	}

	if scannedPattern != memberCachePattern {
		t.Fatalf("unexpected scan pattern: got %q want %q", scannedPattern, memberCachePattern)
	}
	if scannedBatchSize != 100 {
		t.Fatalf("unexpected scan batch size: got %d want 100", scannedBatchSize)
	}

	wantDeleted := []string{"member:channel:UC_1", "member:name:pekora"}
	if !reflect.DeepEqual(deletedKeys, wantDeleted) {
		t.Fatalf("unexpected deleted keys: got %v want %v", deletedKeys, wantDeleted)
	}

	if _, ok := c.byChannelID.Load(member.ChannelID); ok {
		t.Fatalf("expected channel cache to be cleared")
	}
	if _, ok := c.byName.Load(member.Name); ok {
		t.Fatalf("expected name cache to be cleared")
	}
	if _, ok := c.allMembers.Load(allChannelIDsKey); ok {
		t.Fatalf("expected all-members cache to be cleared")
	}
}

func TestCacheInvalidateAll_DeletesValkeyBeforeNewGenerationRead(t *testing.T) {
	t.Parallel()

	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	lookupStarted := make(chan struct{})
	var deleted atomic.Bool
	var eventsMu sync.Mutex
	events := make([]string, 0, 3)
	cacheClient := cachemocks.NewLenientClient()
	cacheClient.ScanKeysFunc = func(context.Context, string, int64) ([]string, error) {
		eventsMu.Lock()
		events = append(events, "scan")
		eventsMu.Unlock()
		close(scanStarted)
		<-releaseScan
		return []string{"member:name:Old"}, nil
	}
	cacheClient.DelManyFunc = func(context.Context, []string) (int64, error) {
		deleted.Store(true)
		eventsMu.Lock()
		events = append(events, "delete")
		eventsMu.Unlock()
		return 1, nil
	}
	cacheClient.GetFunc = func(_ context.Context, _ string, dest any) error {
		eventsMu.Lock()
		events = append(events, "get")
		eventsMu.Unlock()
		if deleted.Load() {
			return errors.New("cache miss")
		}
		member, ok := dest.(*domain.Member)
		if !ok {
			return errors.New("cache destination is not a member")
		}
		*member = domain.Member{ChannelID: "old-channel", Name: "Old"}
		return nil
	}
	c := &Cache{cache: cacheClient, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.byName.Store("Old", &domain.Member{Name: "Old"})

	invalidateDone := make(chan error, 1)
	go func() {
		invalidateDone <- c.InvalidateAll(context.Background())
	}()
	<-scanStarted
	if _, ok := c.byName.Load("Old"); ok {
		t.Fatal("memory was not cleared before Valkey deletion")
	}

	lookupDone := make(chan *domain.Member, 1)
	go func() {
		close(lookupStarted)
		generation := c.currentSnapshotGeneration()
		lookupDone <- c.loadNameFromDistributedCache(context.Background(), "Old", generation)
	}()
	<-lookupStarted
	close(releaseScan)
	if err := <-invalidateDone; err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}
	if member := <-lookupDone; member != nil {
		t.Fatalf("distributed lookup member = %+v, want cache miss after deletion", member)
	}

	eventsMu.Lock()
	gotEvents := append([]string(nil), events...)
	eventsMu.Unlock()
	wantEvents := []string{"scan", "delete", "get"}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("invalidate/read order = %v, want %v", gotEvents, wantEvents)
	}
	if _, ok := c.byName.Load("Old"); ok {
		t.Fatal("stale Valkey value was published into the new generation")
	}
}

func TestCacheInvalidateAll_ValkeyDeleteFollowsInFlightPointPublish(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cacheClient := cachemocks.NewLenientClient()
	setStarted := make(chan struct{})
	releaseSet := make(chan struct{})
	invalidateStarted := make(chan struct{})
	var eventsMu sync.Mutex
	events := make([]string, 0, 5)
	setCalls := 0
	cacheClient.SetFunc = func(context.Context, string, any, time.Duration) error {
		eventsMu.Lock()
		setCalls++
		call := setCalls
		events = append(events, "set")
		eventsMu.Unlock()
		if call == 1 {
			close(setStarted)
			<-releaseSet
		}
		return nil
	}
	cacheClient.ScanKeysFunc = func(context.Context, string, int64) ([]string, error) {
		eventsMu.Lock()
		events = append(events, "scan")
		eventsMu.Unlock()
		return []string{"member:name:Old"}, nil
	}
	cacheClient.DelManyFunc = func(context.Context, []string) (int64, error) {
		eventsMu.Lock()
		events = append(events, "delete")
		eventsMu.Unlock()
		return 1, nil
	}
	c := &Cache{cache: cacheClient, logger: logger, cacheTTL: time.Minute}
	generation := c.currentSnapshotGeneration()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.cacheMember(context.Background(), &domain.Member{ChannelID: "old-channel", Name: "Old"}, generation, "")
	}()
	<-setStarted

	invalidateDone := make(chan error, 1)
	go func() {
		close(invalidateStarted)
		invalidateDone <- c.InvalidateAll(context.Background())
	}()
	<-invalidateStarted
	close(releaseSet)
	<-writerDone
	if err := <-invalidateDone; err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	eventsMu.Lock()
	gotEvents := append([]string(nil), events...)
	eventsMu.Unlock()
	wantEvents := []string{"set", "set", "scan", "delete"}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("Valkey operation order = %v, want %v", gotEvents, wantEvents)
	}
	if _, ok := c.byName.Load("Old"); ok {
		t.Fatal("InvalidateAll() left the in-flight point value in memory")
	}
}

func TestCacheInvalidateAll_WithoutValkeyStillClearsMemory(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	member := &domain.Member{Name: "miko", ChannelID: "UC_2"}
	c := &Cache{
		logger: logger,
	}
	c.byChannelID.Store(member.ChannelID, member)
	c.byName.Store(member.Name, member)
	c.allMembers.Store(allChannelIDsKey, []string{member.ChannelID})

	if err := c.InvalidateAll(ctx); err != nil {
		t.Fatalf("InvalidateAll failed: %v", err)
	}

	if _, ok := c.byChannelID.Load(member.ChannelID); ok {
		t.Fatalf("expected channel cache to be cleared")
	}
	if _, ok := c.byName.Load(member.Name); ok {
		t.Fatalf("expected name cache to be cleared")
	}
	if _, ok := c.allMembers.Load(allChannelIDsKey); ok {
		t.Fatalf("expected all-members cache to be cleared")
	}
}
