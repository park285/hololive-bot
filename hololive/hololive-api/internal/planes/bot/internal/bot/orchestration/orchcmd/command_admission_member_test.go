package orchcmd

import (
	"errors"
	"strconv"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestCommandAdmissionMemberIsDerivedFromMessageID(t *testing.T) {
	first, err := commandAdmissionMember(testMessageIdentity)
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

	if first == testMessageIdentity {
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
	cmdCtx := &botcommand.CommandContext{Room: testRoomID, UserID: testUserID}

	err := policy.Admit(t.Context(), cmdCtx, "broadcast_history")
	if !errors.Is(err, errCommandAdmissionUnavailable) {
		t.Fatalf("Admit() error = %v, want %v", err, errCommandAdmissionUnavailable)
	}
}

func TestCommandAdmissionReplayDoesNotConsumeQuotaTwice(t *testing.T) {
	policy, limiter, mini := newTestCommandAdmissionPolicy(t)
	ctx := t.Context()
	cmdCtx := &botcommand.CommandContext{Room: testRoomID, UserID: testUserID, MessageID: testMessageIdentity}

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
		cmdCtx := &botcommand.CommandContext{Room: testRoomID, UserID: testUserID, MessageID: messageIDForIndex(i)}
		if err := policy.Admit(ctx, cmdCtx, "broadcast_history"); err != nil {
			t.Fatalf("admission %d error = %v", i, err)
		}
	}

	overLimit := &botcommand.CommandContext{Room: testRoomID, UserID: testUserID, MessageID: "message:overflow"}
	if err := policy.Admit(ctx, overLimit, "broadcast_history"); !errors.Is(err, errCommandRateLimited) {
		t.Fatalf("over-limit admission error = %v, want rate limit", err)
	}
}

func TestCommandAdmissionPassesTheDerivedMemberToTheLimiter(t *testing.T) {
	limiter := &stubCommandRateLimiter{}
	policy := &commandAdmissionPolicy{limiter: limiter}
	cmdCtx := &botcommand.CommandContext{Room: testRoomID, UserID: testUserID, MessageID: testMessageIdentity}

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
