package botruntime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/iris-client-go/v5/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

// acceptingIrisClient는 응답 송신을 request id와 함께 수락해 명령 결과가 outcome_unknown으로 끝나지 않게 한다.
type acceptingIrisClient struct{ stubIrisClient }

func (*acceptingIrisClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{Success: true, RequestID: "reply-1"}, nil
}

type recordingAlarm struct {
	testAlarmCRUD

	mu    sync.Mutex
	rooms []string
}

func (r *recordingAlarm) ListRoomAlarmsView(_ context.Context, roomID string) ([]domain.AlarmListView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rooms = append(r.rooms, roomID)

	return []domain.AlarmListView{}, nil
}

func (r *recordingAlarm) listedRooms() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.rooms)
}

func newInjectedDependencyBot(t *testing.T, alarm *recordingAlarm) (*orchestration.Bot, *acl.Service) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	aclService, err := acl.NewACLService(t.Context(), postgres, logger, true, acl.ACLModeWhitelist, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot, err := orchestration.NewBot(&orchestration.Dependencies{
		Logger:           logger,
		Client:           &acceptingIrisClient{},
		ReplyOutbox:      dispatchingReplyOutbox{client: &acceptingIrisClient{}},
		MessageAdapter:   messaging.NewMessageAdapter("!"),
		Formatter:        formatter.NewResponseFormatter("!", nil),
		Cache:            sharedtestutil.NewTestCacheService(t.Context(), t),
		Postgres:         postgres,
		Holodex:          &holodexprovider.Service{},
		Alarm:            alarm,
		Matcher:          &matcher.Matcher{},
		MembersData:      &stubMemberDataProvider{},
		MemberRepository: member.NewMemberRepository(postgres),
		ACL:              aclService,
	})
	if err != nil {
		t.Fatal(err)
	}

	return bot, aclService
}

type dispatchingReplyOutbox struct {
	client transport.ReplyClient
}

func (o dispatchingReplyOutbox) RecordReply(ctx context.Context, entry *transport.ReplyOutboxEntry) error {
	if _, err := transport.DispatchStoredReply(ctx, o.client, entry.Room, []byte(entry.Payload), entry.ClientRequestID); err != nil {
		return fmt.Errorf("dispatch stored reply: %w", err)
	}

	return nil
}

const injectedTestRoom = "3001"

func sendInjectedTestMessage(t *testing.T, bot *orchestration.Bot, id, text string) {
	t.Helper()

	sender := "user"
	message := &webhook.Message{
		Msg:    text,
		Room:   injectedTestRoom,
		Sender: &sender,
		JSON:   &webhook.MessageJSON{UserID: "4001", MessageID: id},
	}

	if err := bot.ProcessMessage(t.Context(), message); err != nil {
		t.Fatalf("ProcessMessage(%q) error = %v", text, err)
	}
}

func TestBotUsesInjectedPlaneDependencies(t *testing.T) {
	alarm := &recordingAlarm{}
	bot, aclService := newInjectedDependencyBot(t, alarm)

	sendInjectedTestMessage(t, bot, "m-1", "!알람 목록")

	if rooms := alarm.listedRooms(); len(rooms) != 0 {
		t.Fatalf("command in non-whitelisted room reached alarm service: %v", rooms)
	}

	if _, err := aclService.AddRoom(t.Context(), acl.ACLModeWhitelist, injectedTestRoom); err != nil {
		t.Fatal(err)
	}

	sendInjectedTestMessage(t, bot, "m-2", "!알람 목록")

	if rooms := alarm.listedRooms(); len(rooms) != 1 || rooms[0] != injectedTestRoom {
		t.Fatalf("alarm list calls = %v, want one for %s", rooms, injectedTestRoom)
	}
}
