package durability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/contracttest"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/config/configtest"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

var (
	errContractSettleNotApplied = errors.New("irisdurable contract: settle was not applied for the claim token")
	errContractUnsupportedState = errors.New("irisdurable contract: unsupported settle status")
	errContractClaimMismatch    = errors.New("irisdurable contract: claimed a different reply than requested")
	contractCounter             atomic.Uint64
)

func TestIrisDurableContract(t *testing.T) {
	terminalRetention, automaticReplayHorizon := contractProfileRetention(t)

	contracttest.Run(t, contracttest.Suite{
		Admitter: func(t *testing.T) irisdurable.Admitter {
			t.Helper()

			return contractAdmitter{inbox: NewInboxRepository(newDurabilityPool(t))}
		},
		ReplyOutbox: func(t *testing.T) contracttest.ReplyOutboxFixture {
			t.Helper()

			pool := newReplyDelayPool(t)

			return &contractReplyOutbox{pool: pool, repo: newTestReplyOutboxRepository(pool)}
		},
		Reissue: &contracttest.ReissueFixture{
			Ladder: irisdurable.ReissueLadder{
				MaxGenerations: iris.ReplyReissueMaxGenerations,
				Derive:         iris.ReissuedClientRequestID,
			},
			PreHandoffConflict:    iris.IsPreHandoffClientRequestIDConflict,
			NewPreHandoffConflict: func() error { return contractIrisConflict(t, iris.HTTPErrorCodeClientRequestIDFailed) },
			NewTerminalConflict:   func() error { return contractIrisConflict(t, iris.HTTPErrorCodeClientRequestIDOutcomeUnknown) },
		},
		Retention: &contracttest.RetentionFixture{
			IrisAdmissionRetention:     iris.ReplyAdmissionRetention,
			IrisAutomaticReplayHorizon: iris.ReplyAutomaticReplayHorizon,
			ReplyOutboxRetention:       terminalRetention,
			AutomaticReplayHorizon:     automaticReplayHorizon,
			InboxTerminalRetention:     terminalRetention,
		},
	})
}

func contractProfileRetention(t *testing.T) (terminalRetention, automaticReplayHorizon time.Duration) {
	t.Helper()

	fixture := configtest.APIProfileFixture(t)

	t.Setenv(workercontract.ProfileFileEnv, fixture)

	profile, err := apiconfig.LoadAPIWorkerProfile()
	require.NoError(t, err)
	require.Positive(t, profile.BotWebhookInbox.TerminalRetentionMS)
	require.Positive(t, profile.BotReplyOutbox.AutomaticReplayHorizonMS)

	return time.Duration(profile.BotWebhookInbox.TerminalRetentionMS) * time.Millisecond,
		time.Duration(profile.BotReplyOutbox.AutomaticReplayHorizonMS) * time.Millisecond
}

func contractIrisConflict(t *testing.T, code string) error {
	t.Helper()

	return fmt.Errorf("iris contract conflict: %w", testutil.IrisHTTPError(t.Context(), t, http.StatusConflict, fmt.Sprintf(`{"code":%q}`, code)))
}

func contractUniqueID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixNano(), contractCounter.Add(1))
}

type contractAdmitter struct {
	inbox *InboxRepository
}

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
		applied, err = o.repo.SettleAccepted(ctx, id, attempt.ClaimToken, outcome.IrisRequestID, time.Minute)
		if err != nil {
			return fmt.Errorf("settle accepted: %w", err)
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
		return makeReplyRetryDue(ctx, o.pool, id, max(outcome.RetryAfter.Truncate(time.Millisecond), time.Millisecond))
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
