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
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/contracttest"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

var (
	errContractSettleNotApplied = errors.New("irisdurable contract: settle was not applied for the claim token")
	errContractUnsupportedState = errors.New("irisdurable contract: unsupported settle status")
	errContractClaimMismatch    = errors.New("irisdurable contract: claimed a different reply than requested")
	contractCounter             atomic.Uint64
)

// TestIrisDurableContract는 hololive의 PostgreSQL inbox·reply outbox 원장이 스택 공통 irisdurable
// 계약을 지키는지 검증한다. NonceStore는 nil이다 — hololive의 HMAC nonce store는 iris-client-go의
// Valkey valkeydedup.NewNonceStore(internal/app/bootstrap/bot_webhook.go)라 PostgreSQL 원장이 아니다.
func TestIrisDurableContract(t *testing.T) {
	terminalRetention := contractTerminalRetention(t)

	contracttest.Run(t, contracttest.Suite{
		Admitter: func(t *testing.T) irisdurable.Admitter {
			t.Helper()

			return contractAdmitter{inbox: NewInboxRepository(newDurabilityPool(t))}
		},
		ReplyOutbox: func(t *testing.T) contracttest.ReplyOutboxFixture {
			t.Helper()

			pool := newDurabilityPool(t)

			return &contractReplyOutbox{pool: pool, repo: NewReplyOutboxRepository(pool)}
		},
		Reissue: &contracttest.ReissueFixture{
			Ladder: irisdurable.ReissueLadder{
				MaxGenerations: iris.ReplyReissueMaxGenerations,
				Derive:         iris.ReissuedClientRequestID,
			},
			PreHandoffConflict:    iris.IsPreHandoffClientRequestIDConflict,
			NewPreHandoffConflict: func() error { return contractIrisConflict(iris.HTTPErrorCodeClientRequestIDFailed) },
			NewTerminalConflict:   func() error { return contractIrisConflict(iris.HTTPErrorCodeClientRequestIDOutcomeUnknown) },
		},
		Retention: &contracttest.RetentionFixture{
			ReplyOutboxRetention:   terminalRetention,
			AutomaticReplayHorizon: ReplyOutboxAutomaticReplayHorizon,
			InboxTerminalRetention: terminalRetention,
		},
	})
}

// contractTerminalRetention은 runtime이 bot_webhook_inbox.terminal_retention_ms로 받는 종단 보존을
// API worker profile fixture에서 settings loader를 거쳐 읽는다. DurableLedgerRepository.Maintain은
// inbox·command·reply outbox 종단 행을 같은 terminalRetention으로 정리한다.
func contractTerminalRetention(t *testing.T) time.Duration {
	t.Helper()

	fixture, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "..", "..",
		"hololive-shared", "pkg", "config", "settings", "testdata", "stack-worker-profile-api.json",
	))
	require.NoError(t, err)

	t.Setenv(workercontract.ProfileFileEnv, fixture)

	profile, err := settings.LoadAPIWorkerProfile()
	require.NoError(t, err)
	require.Positive(t, profile.BotWebhookInbox.TerminalRetentionMS)

	return time.Duration(profile.BotWebhookInbox.TerminalRetentionMS) * time.Millisecond
}

func contractIrisConflict(code string) error {
	return &iris.HTTPError{
		StatusCode: http.StatusConflict,
		URL:        "https://iris/reply",
		Body:       fmt.Sprintf(`{"code":%q}`, code),
	}
}

func contractUniqueID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixNano(), contractCounter.Add(1))
}

type contractAdmitter struct {
	inbox *InboxRepository
}

// Admit은 계약의 OrderingKey를 hololive의 room id로 보고, 실제 ordering key는 runtime과 같이
// "room:"+roomID로 만든다.
func (a contractAdmitter) Admit(ctx context.Context, input irisdurable.AdmissionInput) (workercontract.AdmissionResult, error) {
	result, err := a.inbox.AdmitResult(ctx, InboxMessage{
		MessageID:   MessageIdentity(input.MessageID),
		RoomID:      input.OrderingKey,
		OrderingKey: "room:" + input.OrderingKey,
		Payload:     input.Payload,
	})
	if err != nil {
		return result, fmt.Errorf("admit result: %w", err)
	}

	return result, nil
}

type contractReplyOutbox struct {
	pool *pgxpool.Pool
	repo *ReplyOutboxRepository
}

func (o *contractReplyOutbox) NewRecord(t *testing.T, payload []byte) irisdurable.ReplyRecord {
	t.Helper()

	id := contractUniqueID("contract")
	messageID := MessageIdentity(id)

	return irisdurable.ReplyRecord{
		MessageID:       messageID,
		Phase:           transport.ReplyPhase,
		Ordinal:         0,
		RoomID:          "room-" + id,
		ClientRequestID: transport.ReplyClientRequestID(messageID, 0),
		Payload:         payload,
	}
}

func (o *contractReplyOutbox) Stage(ctx context.Context, record irisdurable.ReplyRecord) (irisdurable.ReplyStageOutcome, error) {
	ordinal, err := contractOrdinal(record.Ordinal)
	if err != nil {
		return "", err
	}

	outcome, err := o.repo.Insert(ctx, &ReplyOutboxEntry{
		MessageID:       record.MessageID,
		Phase:           record.Phase,
		Ordinal:         ordinal,
		RoomID:          record.RoomID,
		Payload:         record.Payload,
		ClientRequestID: record.ClientRequestID,
	})
	if err != nil {
		return "", fmt.Errorf("insert reply outbox entry: %w", err)
	}

	switch outcome {
	case ReplyOutboxInserted:
		return irisdurable.ReplyStaged, nil
	case ReplyOutboxAlreadyRecorded:
		return irisdurable.ReplyAlreadyStaged, nil
	case ReplyOutboxPayloadDiverged:
		return irisdurable.ReplyPayloadDiverged, nil
	default:
		return "", fmt.Errorf("%w: insert outcome %s", errContractUnsupportedState, outcome)
	}
}

// BeginAttempt는 방 단위 FIFO를 지키는 Claim에 기댄다. Fixture마다 격리된 DB를 쓰므로 claim 가능한
// 행은 요청한 identity뿐이고, 다른 행이 잡히면 상태를 조작하지 않고 오류로 돌려준다.
func (o *contractReplyOutbox) BeginAttempt(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyAttempt, error) {
	ordinal, err := contractOrdinal(identity.Ordinal)
	if err != nil {
		return irisdurable.ReplyAttempt{}, err
	}

	token := contractUniqueID("claim")

	claim, err := o.repo.Claim(ctx, token, durabilityTestLease)
	if err != nil {
		return irisdurable.ReplyAttempt{}, fmt.Errorf("claim reply outbox row: %w", err)
	}

	if claim == nil {
		return irisdurable.ReplyAttempt{}, fmt.Errorf("%w: %+v", irisdurable.ErrReplyNotClaimable, identity)
	}

	if claim.MessageID != identity.MessageID || claim.Phase != identity.Phase || claim.Ordinal != ordinal {
		return irisdurable.ReplyAttempt{}, fmt.Errorf("%w: got id=%d want %+v", errContractClaimMismatch, claim.ID, identity)
	}

	return irisdurable.ReplyAttempt{
		ReplyIdentity:   identity,
		ClaimToken:      token,
		Attempt:         int(claim.Attempts),
		ClientRequestID: claim.ClientRequestID,
	}, nil
}

func (o *contractReplyOutbox) Settle(ctx context.Context, attempt irisdurable.ReplyAttempt, outcome irisdurable.ReplyOutcome) error {
	id, err := o.rowID(ctx, attempt.ReplyIdentity)
	if err != nil {
		return err
	}

	var applied bool

	switch outcome.Status {
	case irisdurable.ReplyStatusAccepted:
		applied, err = o.repo.MarkAccepted(ctx, id, attempt.ClaimToken, outcome.IrisRequestID)
		if err != nil {
			return fmt.Errorf("mark accepted: %w", err)
		}
	case irisdurable.ReplyStatusOutcomeUnknown, irisdurable.ReplyStatusRetryablePreDispatch,
		irisdurable.ReplyStatusDead, irisdurable.ReplyStatusPermanentConflict:
		applied, err = o.repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         id,
			ClaimToken: attempt.ClaimToken,
			Status:     string(outcome.Status),
			LastError:  "irisdurable contract " + string(outcome.Status),
			RetryAfter: outcome.RetryAfter,
		})
		if err != nil {
			return fmt.Errorf("settle: %w", err)
		}
	case irisdurable.ReplyStatusPending, irisdurable.ReplyStatusSubmitting:
		return fmt.Errorf("%w: %s", errContractUnsupportedState, outcome.Status)
	default:
		return fmt.Errorf("%w: %s", errContractUnsupportedState, outcome.Status)
	}

	if !applied {
		return fmt.Errorf("%w: id=%d status=%s", errContractSettleNotApplied, id, outcome.Status)
	}

	if outcome.Status.Resendable() {
		return o.waitUntilAvailable(ctx, id)
	}

	return nil
}

// hololive의 Settle은 재발송 가능 상태에 available_at = now + max(RetryAfter, 1ms)를 기록하고
// (replyOutboxRetryMilliseconds) Claim은 available_at <= clock_timestamp()만 후보로 본다. 계약
// 스위트는 outcome_unknown 정산 직후 다시 BeginAttempt하므로, 상태를 건드리지 않고 DB 시계 기준으로
// 그 durable backoff가 지나갈 때까지만 기다린다.
func (o *contractReplyOutbox) waitUntilAvailable(ctx context.Context, id int64) error {
	var remainingSeconds float64

	err := o.pool.QueryRow(ctx,
		"SELECT GREATEST(EXTRACT(EPOCH FROM (available_at - clock_timestamp())), 0)::float8 FROM bot_reply_outbox WHERE id = $1",
		id,
	).Scan(&remainingSeconds)
	if err != nil {
		return fmt.Errorf("read reply outbox available_at: %w", err)
	}

	if remainingSeconds > 0 {
		time.Sleep(time.Duration(remainingSeconds*float64(time.Second)) + time.Millisecond)
	}

	return nil
}

func (o *contractReplyOutbox) Inspect(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyState, error) {
	ordinal, err := contractOrdinal(identity.Ordinal)
	if err != nil {
		return irisdurable.ReplyState{}, err
	}

	var (
		state    irisdurable.ReplyState
		status   string
		attempts int32
	)

	err = o.pool.QueryRow(ctx,
		"SELECT status, client_request_id, attempts, payload IS NOT NULL FROM bot_reply_outbox WHERE message_id = $1 AND phase = $2 AND ordinal = $3",
		identity.MessageID, identity.Phase, ordinal,
	).Scan(&status, &state.ClientRequestID, &attempts, &state.PayloadPresent)
	if err != nil {
		return irisdurable.ReplyState{}, fmt.Errorf("inspect reply outbox row %+v: %w", identity, err)
	}

	state.Status = irisdurable.ReplyStatus(status)
	state.Attempts = int(attempts)

	return state, nil
}

func (o *contractReplyOutbox) rowID(ctx context.Context, identity irisdurable.ReplyIdentity) (int64, error) {
	ordinal, err := contractOrdinal(identity.Ordinal)
	if err != nil {
		return 0, err
	}

	var id int64

	err = o.pool.QueryRow(ctx,
		"SELECT id FROM bot_reply_outbox WHERE message_id = $1 AND phase = $2 AND ordinal = $3",
		identity.MessageID, identity.Phase, ordinal,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("resolve reply outbox row %+v: %w", identity, err)
	}

	return id, nil
}

func contractOrdinal(ordinal int) (uint64, error) {
	if ordinal < 0 {
		return 0, fmt.Errorf("%w: negative ordinal %d", ErrInvalidArgument, ordinal)
	}

	return uint64(ordinal), nil
}
