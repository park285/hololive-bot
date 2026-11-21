package eventgrade

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

//go:embed testdata/quarantine_delivery.sql
var quarantineDeliverySQL string

//go:embed testdata/dispatch_states.sql
var dispatchStatesSQL string

func receiptFixture(t *testing.T, n int) (*Repository, dispatchoutbox.Writer, []dispatchoutbox.AlarmQueueEnvelope) {
	t.Helper()

	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	outbox, err := dispatchoutbox.NewPgxRepository(dbtest.WrapPool(r.pool), nil)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("expired", now.Add(-26*time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	envelopes := make([]dispatchoutbox.AlarmQueueEnvelope, n)
	for i := range envelopes {
		room := strconv.Itoa(100 + i)
		p := &domain.EventGradeDispatchPayload{Region: "jp", Score: 34, Outcome: "unavailable"}
		err := r.pool.QueryRow(ctx, seedRequestSQL, room, strconv.Itoa(i+1), p.Score, p.Region, c.EventID, c.ChapterID).Scan(&p.RequestID)
		require.NoError(t, err)

		envelopes[i] = dispatchoutbox.AlarmQueueEnvelope{
			Version:      contract.QueueEnvelopeVersionV1,
			Notification: domain.AlarmNotification{RoomID: room, AlarmType: domain.AlarmTypeEventGrade},
			SourceKind:   domain.AlarmDispatchSourceKindEventGrade,
			EventGrade:   p,
		}
	}

	return r, outbox, envelopes
}

func requireDispatchStates(t *testing.T, r *Repository, published, terminal, unresolved []string) {
	t.Helper()

	var gotPublished, gotTerminal, gotUnresolved []string

	require.NoError(t, r.pool.QueryRow(t.Context(), dispatchStatesSQL, "jp").Scan(&gotPublished, &gotTerminal, &gotUnresolved))
	require.Equal(t, published, gotPublished)
	require.Equal(t, terminal, gotTerminal)
	require.Equal(t, unresolved, gotUnresolved)
}

func TestFinalizeSettlesRejectedAndAcceptedReceiptsWithoutReplay(t *testing.T) {
	r, outbox, envelopes := receiptFixture(t, 4)
	ctx := t.Context()
	envelopes[0].EventGrade.Score++

	_, err := outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes[:3]})
	require.NoError(t, err)

	_, err = r.pool.Exec(ctx, quarantineDeliverySQL, envelopes[1].Notification.RoomID)
	require.NoError(t, err)

	err = r.Finalize(ctx, writerPublisher{outbox})
	require.ErrorContains(t, err, "rejected_collision")
	require.ErrorContains(t, err, "rejected_terminal")
	requireDispatchStates(t, r, []string{"3", "4"}, []string{"1:rejected_collision", "2:rejected_terminal"}, []string{})
	require.NoError(t, r.Finalize(ctx, writerPublisher{outbox}))
}

type corruptedReceipts struct {
	outbox dispatchoutbox.Writer
	cause  error
}

func (w corruptedReceipts) PublishDispatchBatch(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	result, err := w.outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes})
	if err != nil {
		return result, fmt.Errorf("publish before corrupting receipts: %w", err)
	}

	if len(result.Receipts) != 5 {
		return result, errors.New("expected five receipts")
	}

	for i := range result.Receipts {
		receipt := &result.Receipts[i]
		switch receipt.Ordinal {
		case 0:
			receipt.Ordinal = len(envelopes)
		case 1:
			receipt.Ordinal = 2
		case 3:
			receipt.Outcome = "unknown"
		}
	}

	return result, w.cause
}

func TestFinalizeKeepsAmbiguousReceiptsPendingAndHonorsConfirmedReceiptsOnError(t *testing.T) {
	r, outbox, _ := receiptFixture(t, 5)
	cause := errors.New("commit response lost")
	err := r.Finalize(t.Context(), corruptedReceipts{outbox, cause})
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "invalid event grade receipt ordinal")
	require.ErrorContains(t, err, "missing event grade receipt ordinal")
	require.ErrorContains(t, err, "duplicate event grade receipt ordinal")
	require.ErrorContains(t, err, "unknown event grade receipt outcome")
	requireDispatchStates(t, r, []string{"5"}, []string{}, []string{"1", "2", "3", "4"})
	require.NoError(t, r.Finalize(t.Context(), writerPublisher{outbox}))
	requireDispatchStates(t, r, []string{"1", "2", "3", "4", "5"}, []string{}, []string{})
}

func TestFinalizeRegionsDecidesOnlyNamedRegions(t *testing.T) {
	r, outbox, _ := receiptFixture(t, 1)
	publisher := writerPublisher{outbox}

	require.NoError(t, r.FinalizeRegions(t.Context(), publisher, []string{"us"}))
	requireDispatchStates(t, r, []string{}, []string{}, []string{"1"})

	require.NoError(t, r.FinalizeRegions(t.Context(), publisher, []string{"jp"}))
	requireDispatchStates(t, r, []string{"1"}, []string{}, []string{})
}
