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

package durability

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inboxMessage(messageID, roomID, orderingKey string) InboxMessage {
	return InboxMessage{
		MessageID:   messageID,
		RoomID:      roomID,
		OrderingKey: orderingKey,
		Payload:     []byte(`{"body":"x"}`),
	}
}

func TestReplyOutboxSettleRejectsPreDispatchAfterAcceptance(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	t.Run("accepted rows never return to the claim queue as pre-dispatch", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-accepted", 0, `{"body":"x"}`)
		claim := claimOne(ctx, t, repo, entry)

		accepted, err := repo.MarkAccepted(ctx, claim.ID, "token-a", "iris-req-1")
		require.NoError(t, err)
		require.True(t, accepted)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxRetryablePreDispatch,
			LastError:  "settled after dispatch",
		})
		require.NoError(t, err)
		assert.False(t, applied, "Iris가 이미 수리한 행은 pre-dispatch로 정산될 수 없다")

		status, _, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, "accepted", status)

		reclaimed, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, reclaimed, "발송된 행이 claim 큐로 돌아오면 TTL 만료 후 중복 발화가 된다")
	})

	t.Run("submitting rows still settle as pre-dispatch", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-submitting", 0, `{"body":"x"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxRetryablePreDispatch,
			LastError:  "transport reset",
		})
		require.NoError(t, err)
		require.True(t, applied)

		reclaimed, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, reclaimed)
		assert.Equal(t, int32(2), reclaimed.Attempts)
	})

	t.Run("terminal settlements still apply from accepted", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-terminal", 0, `{"body":"x"}`)
		claim := claimOne(ctx, t, repo, entry)

		accepted, err := repo.MarkAccepted(ctx, claim.ID, "token-a", "iris-req-1")
		require.NoError(t, err)
		require.True(t, accepted)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxHandoffCompleted,
		})
		require.NoError(t, err)
		assert.True(t, applied)
	})
}

func TestOversizedDiagnosticsAreTruncatedInsteadOfStrandingTheRow(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	oversized := strings.Repeat("e", 9000)

	t.Run("inbox release truncates last_error and reaches retry", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		repo := NewInboxRepository(pool)
		msg := inboxMessage("message:m-clamp", "room-1", "room:room-1")
		admitOne(ctx, t, repo, msg)
		_, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)

		outcome, err := repo.Release(ctx, msg.MessageID, "token-a", 3, time.Minute, oversized)
		require.NoError(t, err)
		require.Equal(t, InboxReleaseRetried, outcome)

		status, _, _, _ := inboxRow(ctx, t, pool, msg.MessageID)
		assert.Equal(t, "retry", status)

		var stored string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT last_error FROM bot_webhook_inbox WHERE message_id = $1", msg.MessageID).Scan(&stored))
		assert.LessOrEqual(t, len(stored), 8192)
		assert.Contains(t, stored, "truncated from 9000 bytes")
	})

	t.Run("outbox settle truncates last_error and reaches the terminal status", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		repo := NewReplyOutboxRepository(pool)
		entry := newReplyOutboxEntry("message:m-clamp", 0, `{"body":"x"}`)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxOutcomeUnknown,
			LastError:  oversized,
		})
		require.NoError(t, err)
		require.True(t, applied)

		status, _, _, token := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, ReplyOutboxOutcomeUnknown, status)
		assert.Nil(t, token)
	})

	t.Run("command execution truncates result_summary and reaches terminal", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		repo := NewCommandExecutionRepository(pool)
		claimed, err := repo.Claim(ctx, "message:m-clamp", "alarm", "token-a")
		require.NoError(t, err)
		require.True(t, claimed)

		applied, err := repo.Complete(ctx, "message:m-clamp", "token-a", CommandExecutionFailed, strings.Repeat("s", 3000))
		require.NoError(t, err)
		require.True(t, applied)

		var status, summary string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, result_summary FROM bot_command_executions WHERE message_id = $1",
			"message:m-clamp").Scan(&status, &summary))
		assert.Equal(t, CommandExecutionFailed, status)
		assert.LessOrEqual(t, len(summary), 2048)
		assert.Contains(t, summary, "truncated from 3000 bytes")
	})

	t.Run("truncation keeps the head valid utf-8", func(t *testing.T) {
		clamped := clampColumnText(strings.Repeat("가", 4000), 64)
		assert.LessOrEqual(t, len(clamped), 64)
		assert.True(t, isValidUTF8(clamped))
	})
}

func TestInboxReclaimExpiredLeases(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()

	t.Run("an expired lease returns to the queue and fences the old worker out", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		msg := inboxMessage("message:m-lease", "room-1", "room:room-1")
		admitOne(ctx, t, repo, msg)
		_, err := repo.Claim(ctx, "token-a", time.Millisecond)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		reclaim, err := repo.ReclaimExpired(ctx, 5, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), reclaim.Requeued)
		assert.Equal(t, int64(0), reclaim.Abandoned)

		applied, err := repo.Complete(ctx, msg.MessageID, "token-a")
		require.NoError(t, err)
		assert.False(t, applied, "lease를 잃은 워커는 정산에 성공하면 안 된다")

		claim, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim)
		assert.Equal(t, msg.MessageID, claim.MessageID)
	})

	t.Run("exhausted attempts become dead with a recorded reason", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		msg := inboxMessage("message:m-dead", "room-1", "room:room-1")
		admitOne(ctx, t, repo, msg)
		_, err := repo.Claim(ctx, "token-a", time.Millisecond)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		reclaim, err := repo.ReclaimExpired(ctx, 1, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), reclaim.Abandoned)

		var status, reason string
		var terminalAt *time.Time
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, terminal_reason, terminal_at FROM bot_webhook_inbox WHERE message_id = $1",
			msg.MessageID).Scan(&status, &reason, &terminalAt))
		assert.Equal(t, "dead", status)
		assert.NotEmpty(t, reason)
		assert.NotNil(t, terminalAt)

		claim, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, claim, "dead 행은 다시 claim되지 않는다")
	})

	t.Run("a live lease is left alone", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		msg := inboxMessage("message:m-live", "room-1", "room:room-1")
		admitOne(ctx, t, repo, msg)
		_, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)

		reclaim, err := repo.ReclaimExpired(ctx, 5, 100)
		require.NoError(t, err)
		assert.Equal(t, InboxReclaim{}, reclaim)
	})

	t.Run("invalid arguments are rejected before touching postgres", func(t *testing.T) {
		_, err := repo.ReclaimExpired(ctx, 0, 100)
		require.ErrorIs(t, err, ErrInvalidArgument)

		_, err = repo.ReclaimExpired(ctx, 5, 0)
		require.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestInboxClaimSerializesOneOrderingKey(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()

	t.Run("a second message of the same room waits for the first", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		admitOne(ctx, t, repo, inboxMessage("message:m-room1-a", "room-1", "room:room-1"))
		admitOne(ctx, t, repo, inboxMessage("message:m-room1-b", "room-1", "room:room-1"))

		first, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, first)
		assert.Equal(t, "message:m-room1-a", first.MessageID)

		second, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, second, "같은 방의 두 메시지가 동시에 처리되면 순서가 뒤집힌다")

		applied, err := repo.Complete(ctx, first.MessageID, "token-a")
		require.NoError(t, err)
		require.True(t, applied)

		next, err := repo.Claim(ctx, "token-c", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, next)
		assert.Equal(t, "message:m-room1-b", next.MessageID)
	})

	t.Run("distinct rooms stay concurrent", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		admitOne(ctx, t, repo, inboxMessage("message:m-room1", "room-1", "room:room-1"))
		admitOne(ctx, t, repo, inboxMessage("message:m-room2", "room-2", "room:room-2"))

		first, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, first)

		second, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, second)
		assert.NotEqual(t, first.MessageID, second.MessageID)
	})
}

func TestCommandExecutionClaimOwnership(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewCommandExecutionRepository(pool)
	ctx := context.Background()

	t.Run("a foreign process cannot complete another claim", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		claimed, err := repo.Claim(ctx, "message:m-own", "alarm", "token-a")
		require.NoError(t, err)
		require.True(t, claimed)

		applied, err := repo.Complete(ctx, "message:m-own", "token-stale", CommandExecutionSucceeded, "foreign")
		require.NoError(t, err)
		assert.False(t, applied)

		applied, err = repo.Complete(ctx, "message:m-own", "token-a", CommandExecutionSucceeded, "ok")
		require.NoError(t, err)
		assert.True(t, applied)
	})

	t.Run("a stale claim is closed terminally and never re-executes", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		claimed, err := repo.Claim(ctx, "message:m-stale", "alarm", "token-a")
		require.NoError(t, err)
		require.True(t, claimed)
		time.Sleep(20 * time.Millisecond)

		expired, err := repo.ExpireStaleClaims(ctx, 10*time.Millisecond, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), expired)

		var status, summary string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, result_summary FROM bot_command_executions WHERE message_id = $1",
			"message:m-stale").Scan(&status, &summary))
		assert.Equal(t, CommandExecutionFailed, status)
		assert.NotEmpty(t, summary)

		reclaimed, err := repo.Claim(ctx, "message:m-stale", "alarm", "token-b")
		require.NoError(t, err)
		assert.False(t, reclaimed, "결과를 모르는 실행을 되살리면 이미 나간 응답이 다시 나간다")

		applied, err := repo.Complete(ctx, "message:m-stale", "token-a", CommandExecutionSucceeded, "late")
		require.NoError(t, err)
		assert.False(t, applied)
	})

	t.Run("a fresh claim is left alone", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		_, err := repo.Claim(ctx, "message:m-fresh", "alarm", "token-a")
		require.NoError(t, err)

		expired, err := repo.ExpireStaleClaims(ctx, time.Hour, 100)
		require.NoError(t, err)
		assert.Equal(t, int64(0), expired)
	})
}

func TestReplyOutboxInsertDistinguishesReplayFromDivergence(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	t.Run("an identical replay is an idempotent no-op", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-replay", 0, `{"body":"first"}`)
		outcome, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		require.Equal(t, ReplyOutboxInserted, outcome)

		outcome, err = repo.Insert(ctx, entry)
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxAlreadyRecorded, outcome)
	})

	t.Run("a divergent payload for the same slot is an explicit conflict", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-diverge", 0, `{"body":"first"}`)
		outcome, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		require.Equal(t, ReplyOutboxInserted, outcome)

		diverged := *entry
		diverged.Payload = []byte(`{"body":"rewritten"}`)
		outcome, err = repo.Insert(ctx, &diverged)
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxPayloadDiverged, outcome)

		_, payload, _, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.JSONEq(t, `{"body":"first"}`, string(payload))
	})
}

func TestReplyOutboxOrdinalSharesTheTransportDomain(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	_, err := repo.Insert(ctx, newReplyOutboxEntry("message:m-ordinal", ^uint64(0), `{"body":"x"}`))
	require.ErrorIs(t, err, ErrInvalidArgument, "BIGINT 정의역 밖 ordinal은 드라이버 인코딩 오류가 아니라 인자 오류다")

	entry := newReplyOutboxEntry("message:m-ordinal", 1<<62, `{"body":"x"}`)
	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Equal(t, entry.Ordinal, claim.Ordinal)
}

func TestReplyOutboxInsertDoesNotShareThePayloadBuffer(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	payload := []byte(`{"body":"first"}`)
	entry := newReplyOutboxEntry("message:m-buffer", 0, "")
	entry.Payload = payload

	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	copy(payload, `{"body":"XXXXX"}`)

	replayed := newReplyOutboxEntry("message:m-buffer", 0, `{"body":"first"}`)
	outcome, err = repo.Insert(ctx, replayed)
	require.NoError(t, err, "저장된 payload와 payload_hash는 호출자 버퍼 변형과 무관해야 한다")
	assert.Equal(t, ReplyOutboxAlreadyRecorded, outcome,
		"호출자 버퍼가 변해도 저장본과 동일 재삽입으로 분류돼야 한다")
}

func TestRoomIdentifierFollowsTheWebhookContract(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	admitted, err := repo.Admit(ctx, inboxMessage("message:m-longroom", strings.Repeat("방", 256), "room:long"))
	require.NoError(t, err, "SDK Room 계약(256 rune) 안의 방 제목이 admission에서 탈락하면 메시지가 유실된다")
	assert.True(t, admitted)

	_, err = repo.Admit(ctx, inboxMessage("message:m-toolong", strings.Repeat("방", 257), "room:toolong"))
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestLeaseBelowOneMillisecondIsRejected(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	admitOne(ctx, t, repo, inboxMessage("message:m-lease-floor", "room-1", "room:room-1"))

	_, err := repo.Claim(ctx, "token-a", 500*time.Microsecond)
	require.ErrorIs(t, err, ErrInvalidArgument, "밀리초 미만 lease는 태어날 때부터 만료다")

	_, err = repo.Release(ctx, "message:m-lease-floor", "token-a", 3, 500*time.Microsecond, "boom")
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestMessageIdentityIsOneKeySpaceAcrossTheThreeLedgers(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := context.Background()
	truncateDurabilityTables(ctx, t, pool)

	const raw = "m-shared"
	prefixed := MessageIdentity(raw)
	require.Equal(t, "message:m-shared", prefixed)
	require.Equal(t, prefixed, MessageIdentity(prefixed), "정규화는 멱등이어야 한다")

	inbox := NewInboxRepository(pool)
	admitted, err := inbox.Admit(ctx, inboxMessage(raw, "room-1", "room:room-1"))
	require.NoError(t, err)
	require.True(t, admitted)

	admitted, err = inbox.Admit(ctx, inboxMessage(prefixed, "room-1", "room:room-1"))
	require.NoError(t, err)
	assert.False(t, admitted, "raw id와 접두 id가 다른 행을 만들면 메시지당 실행 1회가 깨진다")

	executions := NewCommandExecutionRepository(pool)
	claimed, err := executions.Claim(ctx, raw, "alarm", "token-a")
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = executions.Claim(ctx, prefixed, "alarm", "token-b")
	require.NoError(t, err)
	assert.False(t, claimed)

	outbox := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry(prefixed, 0, `{"body":"x"}`)
	outcome, err := outbox.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	rawEntry := *entry
	rawEntry.MessageID = raw
	outcome, err = outbox.Insert(ctx, &rawEntry)
	require.NoError(t, err)
	assert.Equal(t, ReplyOutboxAlreadyRecorded, outcome)
}

func TestMessageIdentityNormalization(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                  "",
		"   ":               "",
		"m-1":               "message:m-1",
		"  m-1  ":           "message:m-1",
		"message:m-1":       "message:m-1",
		"  message:m-1  ":   "message:m-1",
		"message:message:x": "message:message:x",
	}

	for raw, want := range cases {
		assert.Equal(t, want, MessageIdentity(raw), "MessageIdentity(%q)", raw)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}

	return true
}
