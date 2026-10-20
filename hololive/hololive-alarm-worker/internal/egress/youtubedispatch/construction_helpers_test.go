package youtubedispatch

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// testTransitionOwner는 시험이 직접 만든 전이 저장소의 claim 소유자 ID다.
const testTransitionOwner = "youtube-dispatcher:test"

// testIrisRequestPrefix는 시험 sender가 202 성공 때 돌려주는 Iris requestId의 접두어다.
const testIrisRequestPrefix = "iris-test:"

// testAdmission은 시험 sender의 발송 결과를 Iris 202 admission으로 바꾼다. 성공이면 client request ID에서 만든
// 비지 않은 requestId를, 실패면 빈 requestId와 그 오류를 돌려준다.
func testAdmission(clientRequestID string, err error) (string, error) {
	if err != nil {
		return "", err
	}

	return testIrisRequestPrefix + clientRequestID, nil
}

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
	sender delivery.PreparedMessageSender,
	renderer *template.Renderer,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *Dispatcher {
	tb.Helper()

	return newDispatcherWithDepsForTest(tb, db, Dependencies{Cache: cacheClient, Sender: sender, Renderer: renderer}, logger, config)
}

// newDispatcherWithDepsForTest는 db가 nil이면 실제 seed DB와 단위 transition 대역으로 dispatcher를 만든다.
func newDispatcherWithDepsForTest(
	tb testing.TB,
	db *pgxpool.Pool,
	deps Dependencies,
	logger *slog.Logger,
	config *dispatchstate.Config,
) *Dispatcher {
	tb.Helper()

	unitTransition := db == nil
	if unitTransition {
		db = newDeliveryPool(tb)
	}

	deps.DB = db

	if deps.MemberNames == nil {
		deps.MemberNames = staticMemberNames{}
	}

	// 운영 기동과 같이 message_strings를 한 번 적재해 둔다. 조회 시 lazy 적재는 없다.
	deps.MessageStrings = messagestrings.NewStore(db, logger)
	require.NoError(tb, deps.MessageStrings.Load(tb.Context()))

	if deps.Renderer == nil {
		deps.Renderer = template.NewRenderer(db, logger)
	}

	dispatcher, err := NewDispatcher(deps, logger, withTestDispatchConfigDefaults(config))
	require.NoError(tb, err)

	if unitTransition {
		dispatcher.send.transition = &lifecycleTransitionSpy{complete: store.ApplyResult{Outcome: store.ApplyApplied}}
		dispatcher.audit.telemetry = nil
	}

	return dispatcher
}
