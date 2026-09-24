package youtubedispatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/claim"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func TestNewDispatcherRejectsTypedNilDatabase(t *testing.T) {
	var pool *pgxpool.Pool

	dispatcher, err := NewDispatcher(Dependencies{DB: pool}, nil, nil)
	require.ErrorContains(t, err, "db is required")
	require.Nil(t, dispatcher)
}

func TestNewDispatcherRejectsPlainNilDatabase(t *testing.T) {
	dispatcher, err := NewDispatcher(Dependencies{}, nil, nil)
	require.ErrorContains(t, err, "db is required")
	require.Nil(t, dispatcher)
}

func newDispatcherForTest(
	tb testing.TB,
	db *pgxpool.Pool,
	cacheClient cache.Client,
	sender delivery.MessageSender,
	renderer *template.Renderer,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *Dispatcher {
	tb.Helper()

	unitTransition := db == nil
	if unitTransition {
		db = newDeliveryPool(tb)
	}

	deps := Dependencies{DB: db, Cache: cacheClient, Sender: sender, Renderer: renderer}

	deps.MessageStrings = messagestrings.NewStore(db, logger)

	if deps.Renderer == nil {
		deps.Renderer = template.NewRenderer(db, logger)
	}

	dispatcher, err := NewDispatcher(deps, logger, config)
	require.NoError(tb, err)

	if unitTransition {
		dispatcher.send.transition = &lifecycleTransitionSpy{complete: store.ApplyResult{Outcome: store.ApplyApplied}}
		dispatcher.send.claims = unitClaimResolver{logger: dispatcher.logger}
		dispatcher.audit.telemetry = nil
	}

	return dispatcher
}

// unitClaimResolver는 순수 발송 단위 테스트에서 claim 입출력만 명시적으로 제공한다.
// 영속 claim과 version fence는 실제 PostgreSQL 통합 테스트가 검증한다.
type unitClaimResolver struct{ logger *slog.Logger }

func (resolver unitClaimResolver) selectClaimedDeliveries(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	_ claim.DecisionCache,
) deliveryClaimSelection {
	selection := deliveryClaimSelection{}

	for i := range min(len(rows), len(outboxes)) {
		row, outbox := rows[i], outboxes[i]

		if err := validateDeliveryLogicalIdentity(&row, &outbox); err != nil {
			resolver.logger.Log(ctx, slog.LevelWarn, "Failed to resolve delivery logical identity before send",
				deliveryClaimLogAttrs(&row, &outbox, slog.Any("error", err))...)

			selection.retryRows = append(selection.retryRows, row)
			selection.retryOutboxes = append(selection.retryOutboxes, outbox)
			selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, row.ID)
			selection.retryOutboxIDs = append(selection.retryOutboxIDs, outbox.ID)

			continue
		}

		token := dispatchstate.ClaimToken{Kind: outbox.Kind, PostID: outbox.ContentID, AuthorizedAt: time.Now().UTC()}

		selection.sendRows = append(selection.sendRows, row)
		selection.sendOutboxes = append(selection.sendOutboxes, outbox)
		selection.claimTokens = append(selection.claimTokens, token)
		selection.rowClaimTokens = append(selection.rowClaimTokens, []dispatchstate.ClaimToken{token})
	}

	return selection
}

func (unitClaimResolver) releaseDeliveryClaims(context.Context, []dispatchstate.ClaimToken) error {
	return nil
}

func (unitClaimResolver) releaseDeliveryClaimsWithWarning(context.Context, []dispatchstate.ClaimToken, string, ...any) {
}
