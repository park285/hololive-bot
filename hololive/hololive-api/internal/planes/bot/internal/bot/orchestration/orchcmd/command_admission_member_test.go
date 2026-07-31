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
	"errors"
	"strconv"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCommandAdmissionMemberIsDerivedFromMessageID(t *testing.T) {
	first, err := commandAdmissionMember("message:m-1")
	if err != nil {
		t.Fatalf("commandAdmissionMember() error = %v", err)
	}
	second, err := commandAdmissionMember("  message:m-1  ")
	if err != nil {
		t.Fatalf("commandAdmissionMember() error = %v", err)
	}
	if first != second {
		t.Fatalf("member = %q and %q, want a single stable value per message id", first, second)
	}

	other, err := commandAdmissionMember("message:m-2")
	if err != nil {
		t.Fatalf("commandAdmissionMember() error = %v", err)
	}
	if other == first {
		t.Fatalf("distinct message ids share member %q", first)
	}
	if first == "message:m-1" {
		t.Fatalf("member exposes the raw message id: %q", first)
	}
}

func TestCommandAdmissionMemberFailsClosedWithoutMessageID(t *testing.T) {
	for _, messageID := range []string{"", "   "} {
		member, err := commandAdmissionMember(messageID)
		if err == nil {
			t.Fatalf("commandAdmissionMember(%q) = %q, want an error instead of a non-deterministic member", messageID, member)
		}
		if member != "" {
			t.Fatalf("commandAdmissionMember(%q) = %q, want an empty member on failure", messageID, member)
		}
	}
}

func TestCommandAdmissionRejectsExpensiveCommandWithoutMessageID(t *testing.T) {
	policy, _, _ := newTestCommandAdmissionPolicy(t)
	cmdCtx := &domain.CommandContext{Room: "room-1", UserID: "user-1"}

	err := policy.Admit(t.Context(), cmdCtx, "broadcast_history")
	if !errors.Is(err, errCommandAdmissionUnavailable) {
		t.Fatalf("Admit() error = %v, want %v", err, errCommandAdmissionUnavailable)
	}
}

func TestCommandAdmissionReplayDoesNotConsumeQuotaTwice(t *testing.T) {
	policy, limiter, mini := newTestCommandAdmissionPolicy(t)
	ctx := t.Context()
	cmdCtx := &domain.CommandContext{Room: "room-1", UserID: "user-1", MessageID: "message:m-1"}

	for range expensiveHistoryUserLimit + 2 {
		if err := policy.Admit(ctx, cmdCtx, "broadcast_history"); err != nil {
			t.Fatalf("replayed admission error = %v, want allowed", err)
		}
	}

	roomKey := limiter.cacheKey(commandAdmissionBucket("history:room", cmdCtx.Room))
	userKey := limiter.cacheKey(commandAdmissionBucket("history:user", cmdCtx.UserID))
	assertSortedSetSize(t, mini, roomKey, 1)
	assertSortedSetSize(t, mini, userKey, 1)
}

func TestCommandAdmissionDistinctMessagesStillConsumeQuota(t *testing.T) {
	policy, _, _ := newTestCommandAdmissionPolicy(t)
	ctx := t.Context()

	for i := range expensiveHistoryUserLimit {
		cmdCtx := &domain.CommandContext{Room: "room-1", UserID: "user-1", MessageID: messageIDForIndex(i)}
		if err := policy.Admit(ctx, cmdCtx, "broadcast_history"); err != nil {
			t.Fatalf("admission %d error = %v", i, err)
		}
	}

	overLimit := &domain.CommandContext{Room: "room-1", UserID: "user-1", MessageID: "message:overflow"}
	if err := policy.Admit(ctx, overLimit, "broadcast_history"); !errors.Is(err, errCommandRateLimited) {
		t.Fatalf("over-limit admission error = %v, want rate limit", err)
	}
}

func TestCommandAdmissionPassesTheDerivedMemberToTheLimiter(t *testing.T) {
	limiter := &stubCommandRateLimiter{}
	policy := &commandAdmissionPolicy{limiter: limiter}
	cmdCtx := &domain.CommandContext{Room: "room-1", UserID: "user-1", MessageID: "message:m-1"}

	if err := policy.Admit(t.Context(), cmdCtx, "broadcast_history"); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := policy.Admit(t.Context(), cmdCtx, "broadcast_history"); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	if len(limiter.members) != 2 {
		t.Fatalf("limiter members = %v, want 2 calls", limiter.members)
	}
	if limiter.members[0] != limiter.members[1] {
		t.Fatalf("limiter members = %v, want one stable member per inbound message", limiter.members)
	}

	want, err := commandAdmissionMember(cmdCtx.MessageID)
	if err != nil {
		t.Fatalf("commandAdmissionMember() error = %v", err)
	}
	if limiter.members[0] != want {
		t.Fatalf("limiter member = %q, want %q", limiter.members[0], want)
	}
}

func messageIDForIndex(index int) string {
	return "message:m-" + strconv.Itoa(index)
}
