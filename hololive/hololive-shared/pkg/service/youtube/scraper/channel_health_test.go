package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type channelHealthTestStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newChannelHealthTestStore() *channelHealthTestStore {
	return &channelHealthTestStore{values: make(map[string][]byte)}
}

func (s *channelHealthTestStore) Get(_ context.Context, key string, dest any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.values[key]
	if !ok {
		return errors.New("not found")
	}
	return json.Unmarshal(raw, dest)
}

func (s *channelHealthTestStore) Set(_ context.Context, key string, value any, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.values[key] = raw
	return nil
}

func (s *channelHealthTestStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func TestChannelHealthStoreRecordsParserDriftCooldownPerSource(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	store := NewChannelHealthStore(newChannelHealthTestStore(), ChannelHealthPolicy{
		TTL:             time.Hour,
		ParserDriftBase: 10 * time.Minute,
		ParserDriftMax:  time.Hour,
	})

	delay := store.RecordFailure(ctx, " UC_TEST ", FailureDetail{
		Reason: FailureReasonParserDrift,
		Source: FailureSourceHTML,
	}, now)

	require.Equal(t, 10*time.Minute, delay)
	wait, skip := store.ShouldSkip(ctx, "UC_TEST", FailureSourceHTML, now.Add(time.Minute))
	require.True(t, skip)
	require.Equal(t, 9*time.Minute, wait)
	rssWait, rssSkip := store.ShouldSkip(ctx, "UC_TEST", FailureSourceRSS, now.Add(time.Minute))
	require.False(t, rssSkip)
	require.Zero(t, rssWait)
}

func TestChannelHealthStoreIgnoresRateLimitedGlobalFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	store := NewChannelHealthStore(newChannelHealthTestStore(), DefaultChannelHealthPolicy())

	delay := store.RecordFailure(ctx, "UC_TEST", FailureDetail{
		Reason:     FailureReasonRateLimited,
		Source:     FailureSourceHTML,
		RetryAfter: 30 * time.Minute,
	}, now)

	require.Zero(t, delay)
	_, skip := store.ShouldSkip(ctx, "UC_TEST", FailureSourceHTML, now.Add(time.Minute))
	require.False(t, skip)
}

func TestChannelHealthStoreSuccessClearsCooldown(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	store := NewChannelHealthStore(newChannelHealthTestStore(), ChannelHealthPolicy{
		TTL:             time.Hour,
		ParserDriftBase: 10 * time.Minute,
		ParserDriftMax:  time.Hour,
	})
	store.RecordFailure(ctx, "UC_TEST", FailureDetail{Reason: FailureReasonParserDrift, Source: FailureSourceHTML}, now)

	store.RecordSuccess(ctx, "UC_TEST", FailureSourceHTML, now.Add(time.Minute))

	wait, skip := store.ShouldSkip(ctx, "UC_TEST", FailureSourceHTML, now.Add(2*time.Minute))
	require.False(t, skip)
	require.Zero(t, wait)
}

func TestRecordParserDriftReturnsRetryDelayOnFirstFailure(t *testing.T) {
	ctx := context.Background()
	client := NewClient(
		WithStateStore(newChannelHealthTestStore()),
		WithChannelHealthPolicy(ChannelHealthPolicy{
			TTL:             time.Hour,
			ParserDriftBase: 10 * time.Minute,
			ParserDriftMax:  time.Hour,
		}),
	)

	err := client.recordParserDrift(ctx, "recent_videos", "extract_yt_initial_data", "UC_TEST", "https://example.test", FailureSourceHTML, "<html>", errors.New("missing marker"))

	var cooldown *CooldownError
	require.ErrorAs(t, err, &cooldown)
	require.Equal(t, 10*time.Minute, cooldown.RetryDelay())
	require.True(t, IsParserDriftError(err))
	require.Equal(t, FailureReasonParserDrift, ClassifyFailure(err, FailureSourceHTML).Reason)
}
