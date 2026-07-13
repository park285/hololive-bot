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

package orchcmd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
)

type admissionLimiterCall struct {
	bucket string
	limit  int
	window time.Duration
}

type stubCommandRateLimiter struct {
	calls     []admissionLimiterCall
	decisions []commandAdmissionDecision
	err       error
}

func (s *stubCommandRateLimiter) Admit(_ context.Context, checks []commandAdmissionCheck) (commandAdmissionDecision, error) {
	for _, check := range checks {
		s.calls = append(s.calls, admissionLimiterCall{bucket: check.bucket, limit: check.limit, window: expensiveHistoryWindow})
	}
	if s.err != nil {
		return commandAdmissionDecision{}, s.err
	}
	if len(s.decisions) == 0 {
		return commandAdmissionDecision{Allowed: true}, nil
	}
	decision := s.decisions[0]
	s.decisions = s.decisions[1:]
	return decision, nil
}

type trackedRouterCommand struct {
	name     string
	executed int
}

func (c *trackedRouterCommand) Name() string        { return c.name }
func (c *trackedRouterCommand) Description() string { return c.name }
func (c *trackedRouterCommand) Execute(context.Context, *domain.CommandContext, map[string]any) error {
	c.executed++
	return nil
}

func TestCommandRouterAppliesUserAndRoomAdmissionToExpensiveHistory(t *testing.T) {
	registry := command.NewRegistry()
	handler := &trackedRouterCommand{name: "broadcast_history"}
	registry.Register(handler)
	limiter := &stubCommandRateLimiter{}
	router := NewCommandRouter(registry, nil, func(context.Context, string, string) error { return nil }, nil, nil)
	router.admission = &commandAdmissionPolicy{limiter: limiter}

	err := router.Execute(t.Context(), &domain.CommandContext{Room: "room-1", UserID: "user-1"}, domain.CommandBroadcastHistory, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if handler.executed != 1 {
		t.Fatalf("handler executions = %d, want 1", handler.executed)
	}
	if len(limiter.calls) != 2 {
		t.Fatalf("limiter calls = %d, want 2", len(limiter.calls))
	}
	if limiter.calls[0].limit != expensiveHistoryRoomLimit || limiter.calls[1].limit != expensiveHistoryUserLimit {
		t.Fatalf("limiter calls = %+v", limiter.calls)
	}
	for _, call := range limiter.calls {
		if call.window != expensiveHistoryWindow {
			t.Fatalf("window = %s, want %s", call.window, expensiveHistoryWindow)
		}
		if strings.Contains(call.bucket, "room-1") || strings.Contains(call.bucket, "user-1") {
			t.Fatalf("bucket exposes raw identity: %q", call.bucket)
		}
	}
}

func TestCommandRouterRejectsRateLimitedHistoryWithoutExecutingHandler(t *testing.T) {
	registry := command.NewRegistry()
	handler := &trackedRouterCommand{name: "broadcast_history"}
	registry.Register(handler)
	limiter := &stubCommandRateLimiter{decisions: []commandAdmissionDecision{{Allowed: false, RetryAfter: 10 * time.Second}}}
	var sent string
	router := NewCommandRouter(registry, nil, func(_ context.Context, _ string, message string) error {
		sent = message
		return nil
	}, nil, nil)
	router.admission = &commandAdmissionPolicy{limiter: limiter}

	err := router.Execute(t.Context(), &domain.CommandContext{Room: "room-1", UserID: "user-1"}, domain.CommandBroadcastHistory, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if handler.executed != 0 {
		t.Fatalf("handler executions = %d, want 0", handler.executed)
	}
	if sent != expensiveHistoryRateLimitMessage {
		t.Fatalf("sent message = %q, want %q", sent, expensiveHistoryRateLimitMessage)
	}
}

func TestCommandAdmissionUserDenialsDoNotConsumeRoomQuota(t *testing.T) {
	policy, limiter, mini := newTestCommandAdmissionPolicy(t)
	ctx := t.Context()
	attacker := &domain.CommandContext{Room: "room-1", UserID: "attacker"}

	for range expensiveHistoryUserLimit {
		if err := policy.Admit(ctx, attacker, "broadcast_history"); err != nil {
			t.Fatalf("attacker admission error = %v", err)
		}
	}
	for range expensiveHistoryRoomLimit - expensiveHistoryUserLimit {
		err := policy.Admit(ctx, attacker, "broadcast_history")
		if !errors.Is(err, errCommandRateLimited) {
			t.Fatalf("attacker over-limit error = %v, want rate limit", err)
		}
	}

	roomKey := limiter.cacheKey(commandAdmissionBucket("history:room", attacker.Room))
	userKey := limiter.cacheKey(commandAdmissionBucket("history:user", attacker.UserID))
	assertSortedSetSize(t, mini, roomKey, expensiveHistoryUserLimit)
	assertSortedSetSize(t, mini, userKey, expensiveHistoryUserLimit)

	victim := &domain.CommandContext{Room: attacker.Room, UserID: "victim"}
	if err := policy.Admit(ctx, victim, "broadcast_thumbnail"); err != nil {
		t.Fatalf("victim admission error = %v, want allowed", err)
	}
}

func TestCommandAdmissionRoomDenialDoesNotConsumeUserQuota(t *testing.T) {
	policy, limiter, mini := newTestCommandAdmissionPolicy(t)
	ctx := t.Context()
	room := "room-1"

	for i := range expensiveHistoryRoomLimit {
		cmdCtx := &domain.CommandContext{Room: room, UserID: "user-" + strconv.Itoa(i)}
		if err := policy.Admit(ctx, cmdCtx, "broadcast_history"); err != nil {
			t.Fatalf("room admission %d error = %v", i, err)
		}
	}

	denied := &domain.CommandContext{Room: room, UserID: "room-denied-user"}
	err := policy.Admit(ctx, denied, "broadcast_history")
	if !errors.Is(err, errCommandRateLimited) {
		t.Fatalf("room-full admission error = %v, want rate limit", err)
	}
	userKey := limiter.cacheKey(commandAdmissionBucket("history:user", denied.UserID))
	assertSortedSetSize(t, mini, userKey, 0)
}

func TestCommandAdmissionUserDenialIsWriteFreeWithExpiredEntries(t *testing.T) {
	policy, limiter, mini := newTestCommandAdmissionPolicy(t)
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	cmdCtx := &domain.CommandContext{Room: "room-1", UserID: "user-1"}
	roomKey := limiter.cacheKey(commandAdmissionBucket("history:room", cmdCtx.Room))
	userKey := limiter.cacheKey(commandAdmissionBucket("history:user", cmdCtx.UserID))
	seedAdmissionBucket(t, mini, roomKey, now, 1)
	seedAdmissionBucket(t, mini, userKey, now, expensiveHistoryUserLimit)
	roomBefore := sortedSetSnapshot(t, mini, roomKey)
	userBefore := sortedSetSnapshot(t, mini, userKey)

	err := policy.Admit(t.Context(), cmdCtx, "broadcast_history")
	if !errors.Is(err, errCommandRateLimited) {
		t.Fatalf("user-full admission error = %v, want rate limit", err)
	}
	assertSortedSetUnchanged(t, mini, roomKey, roomBefore)
	assertSortedSetUnchanged(t, mini, userKey, userBefore)
}

func TestCommandAdmissionRoomDenialIsWriteFreeWithExpiredEntries(t *testing.T) {
	policy, limiter, mini := newTestCommandAdmissionPolicy(t)
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	cmdCtx := &domain.CommandContext{Room: "room-1", UserID: "user-1"}
	roomKey := limiter.cacheKey(commandAdmissionBucket("history:room", cmdCtx.Room))
	userKey := limiter.cacheKey(commandAdmissionBucket("history:user", cmdCtx.UserID))
	seedAdmissionBucket(t, mini, roomKey, now, expensiveHistoryRoomLimit)
	seedAdmissionBucket(t, mini, userKey, now, 1)
	roomBefore := sortedSetSnapshot(t, mini, roomKey)
	userBefore := sortedSetSnapshot(t, mini, userKey)

	err := policy.Admit(t.Context(), cmdCtx, "broadcast_history")
	if !errors.Is(err, errCommandRateLimited) {
		t.Fatalf("room-full admission error = %v, want rate limit", err)
	}
	assertSortedSetUnchanged(t, mini, roomKey, roomBefore)
	assertSortedSetUnchanged(t, mini, userKey, userBefore)
}

func TestCommandAdmissionRejectsUnstableUserIdentityBeforeLimiter(t *testing.T) {
	for _, userID := range []string{"", "unknown", " Unknown "} {
		t.Run(strconv.Quote(userID), func(t *testing.T) {
			limiter := &stubCommandRateLimiter{}
			policy := &commandAdmissionPolicy{limiter: limiter}

			err := policy.Admit(t.Context(), &domain.CommandContext{Room: "room-1", UserID: userID}, "broadcast_history")
			if !errors.Is(err, errCommandAdmissionUnavailable) {
				t.Fatalf("Admit() error = %v, want admission unavailable", err)
			}
			if len(limiter.calls) != 0 {
				t.Fatalf("limiter calls = %d, want 0", len(limiter.calls))
			}
		})
	}
}

func newTestCommandAdmissionPolicy(t *testing.T) (*commandAdmissionPolicy, *atomicCommandAdmissionLimiter, *miniredis.Miniredis) {
	t.Helper()

	mini := miniredis.RunT(t)
	host, portString, err := net.SplitHostPort(mini.Addr())
	if err != nil {
		t.Fatalf("split miniredis address: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse miniredis port: %v", err)
	}
	cacheClient, err := cache.NewCacheService(t.Context(), cache.Config{
		Host:              host,
		Port:              port,
		DB:                0,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}
	t.Cleanup(func() {
		if err := cacheClient.Close(); err != nil {
			t.Errorf("close cache client: %v", err)
		}
	})

	limiter, err := newAtomicCommandAdmissionLimiter(cacheClient)
	if err != nil {
		t.Fatalf("new atomic admission limiter: %v", err)
	}
	return &commandAdmissionPolicy{limiter: limiter}, limiter, mini
}

func assertSortedSetSize(t *testing.T, mini *miniredis.Miniredis, key string, want int) {
	t.Helper()
	members, err := mini.ZMembers(key)
	if err != nil {
		if want == 0 && strings.Contains(err.Error(), "no such key") {
			return
		}
		t.Fatalf("read sorted set %q: %v", key, err)
	}
	if len(members) != want {
		t.Fatalf("sorted set %q size = %d, want %d", key, len(members), want)
	}
}

func seedAdmissionBucket(t *testing.T, mini *miniredis.Miniredis, key string, now time.Time, fresh int) {
	t.Helper()
	expiredScore := now.Add(-expensiveHistoryWindow - time.Second).UnixMilli()
	if _, err := mini.ZAdd(key, float64(expiredScore), "expired"); err != nil {
		t.Fatalf("seed expired member in %q: %v", key, err)
	}
	for i := range fresh {
		score := now.Add(-time.Duration(i) * time.Millisecond).UnixMilli()
		if _, err := mini.ZAdd(key, float64(score), "fresh-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("seed fresh member in %q: %v", key, err)
		}
	}
}

func sortedSetSnapshot(t *testing.T, mini *miniredis.Miniredis, key string) map[string]float64 {
	t.Helper()
	members, err := mini.SortedSet(key)
	if err != nil {
		t.Fatalf("read sorted set %q: %v", key, err)
	}
	return members
}

func assertSortedSetUnchanged(t *testing.T, mini *miniredis.Miniredis, key string, before map[string]float64) {
	t.Helper()
	after := sortedSetSnapshot(t, mini, key)
	if !maps.Equal(before, after) {
		t.Fatalf("sorted set %q changed on denial: before=%v after=%v", key, before, after)
	}
}

func TestCommandRouterFailsClosedWhenHistoryAdmissionIsUnavailable(t *testing.T) {
	registry := command.NewRegistry()
	handler := &trackedRouterCommand{name: "broadcast_history"}
	registry.Register(handler)
	router := NewCommandRouter(registry, nil, func(context.Context, string, string) error { return nil }, nil, nil)
	router.admission = &commandAdmissionPolicy{limiter: &stubCommandRateLimiter{err: errors.New("cache down")}}

	err := router.Execute(t.Context(), &domain.CommandContext{Room: "room-1", UserID: "user-1"}, domain.CommandBroadcastHistory, nil)
	if !errors.Is(err, errCommandAdmissionUnavailable) {
		t.Fatalf("Execute() error = %v, want admission unavailable", err)
	}
	if handler.executed != 0 {
		t.Fatalf("handler executions = %d, want 0", handler.executed)
	}
}

func TestCommandRouterDoesNotRateLimitUnrelatedCommands(t *testing.T) {
	registry := command.NewRegistry()
	handler := &trackedRouterCommand{name: "help"}
	registry.Register(handler)
	router := NewCommandRouter(registry, nil, func(context.Context, string, string) error { return nil }, nil, nil)

	err := router.Execute(t.Context(), &domain.CommandContext{}, domain.CommandHelp, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if handler.executed != 1 {
		t.Fatalf("handler executions = %d, want 1", handler.executed)
	}
}
