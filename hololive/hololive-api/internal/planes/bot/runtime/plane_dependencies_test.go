package botruntime

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/iris-client-go/v4/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	membernewsmodel "github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

// acceptingIrisClient는 응답 송신을 request id와 함께 수락해 명령 결과가 outcome_unknown으로 끝나지 않게 한다.
type acceptingIrisClient struct{ stubIrisClient }

func (*acceptingIrisClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{Success: true, RequestID: "reply-1"}, nil
}

// recordingMemberNews는 llm plane이 넘기는 membernews.Service 자리에 들어가 bot 명령이 실제로 호출한 방을 기록한다.
type recordingMemberNews struct {
	mu         sync.Mutex
	subscribed []string
	digests    []string
}

func (r *recordingMemberNews) GenerateRoomDigest(_ context.Context, roomID string, _ membernewsmodel.Period) (*membernewsmodel.Digest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.digests = append(r.digests, roomID)

	return &membernewsmodel.Digest{}, nil
}

func (r *recordingMemberNews) SubscribeRoom(_ context.Context, roomID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.subscribed = append(r.subscribed, roomID)

	return nil
}

func (*recordingMemberNews) UnsubscribeRoom(context.Context, string) error { return nil }

func (*recordingMemberNews) IsRoomSubscribed(context.Context, string) (bool, error) {
	return false, nil
}

func (r *recordingMemberNews) calls() (subscribed, digests []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.subscribed...), append([]string(nil), r.digests...)
}

type recordingMajorEvent struct {
	mu         sync.Mutex
	subscribed []string
}

func (*recordingMajorEvent) IsSubscribed(context.Context, string) (bool, error) { return false, nil }

func (r *recordingMajorEvent) Subscribe(_ context.Context, roomID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.subscribed = append(r.subscribed, roomID)

	return nil
}

func (*recordingMajorEvent) Unsubscribe(context.Context, string) error { return nil }

// newInjectedDependencyBot은 process 조립과 같은 방식으로 ACL·구독·digest 인스턴스를 넘겨 bot을 만든다.
func newInjectedDependencyBot(t *testing.T, memberNews *recordingMemberNews, majorEvent *recordingMajorEvent) (*orchestration.Bot, *acl.Service) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	aclService, err := acl.NewACLService(t.Context(), postgres, logger, true, acl.ACLModeWhitelist, nil)
	if err != nil {
		t.Fatal(err)
	}

	bot, err := orchestration.NewBot(&orchestration.Dependencies{
		Logger:               logger,
		Client:               &acceptingIrisClient{},
		MessageAdapter:       messaging.NewMessageAdapter("!", ""),
		Formatter:            formatter.NewResponseFormatter("!", nil),
		Cache:                sharedtestutil.NewTestCacheService(t.Context(), t),
		Postgres:             postgres,
		Holodex:              &holodexprovider.Service{},
		Alarm:                testAlarmCRUD{},
		Matcher:              &matcher.Matcher{},
		MembersData:          &stubMemberDataProvider{},
		ACL:                  aclService,
		MajorEventRepository: majorEvent,
		MemberNews:           memberNews,
	})
	if err != nil {
		t.Fatal(err)
	}

	return bot, aclService
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

// process 조립은 llm plane의 구독·digest 서비스와 process 하나뿐인 ACL Service를 bot에 직접 넘긴다.
// 봇 명령은 loopback HTTP 없이 주입된 인스턴스를 호출하고, 같은 ACL 인스턴스에 대한 관리 변경은
// 다음 메시지의 ingress 판정에 바로 보여야 한다.
func TestBotUsesInjectedPlaneDependencies(t *testing.T) {
	memberNews := &recordingMemberNews{}
	majorEvent := &recordingMajorEvent{}
	bot, aclService := newInjectedDependencyBot(t, memberNews, majorEvent)

	sendInjectedTestMessage(t, bot, "m-1", "!뉴스구독 켜기")

	if subscribed, _ := memberNews.calls(); len(subscribed) != 0 {
		t.Fatalf("command in non-whitelisted room reached member news: %v", subscribed)
	}

	if _, err := aclService.AddRoom(t.Context(), acl.ACLModeWhitelist, injectedTestRoom); err != nil {
		t.Fatal(err)
	}

	sendInjectedTestMessage(t, bot, "m-2", "!뉴스구독 켜기")
	sendInjectedTestMessage(t, bot, "m-3", "!뉴스")
	sendInjectedTestMessage(t, bot, "m-4", "!행사알림 켜기")

	subscribed, digests := memberNews.calls()
	if len(subscribed) != 1 || subscribed[0] != injectedTestRoom || len(digests) != 1 || digests[0] != injectedTestRoom {
		t.Fatalf("member news calls = subscribed %v digests %v, want one call each for %s", subscribed, digests, injectedTestRoom)
	}

	majorEvent.mu.Lock()
	defer majorEvent.mu.Unlock()

	if len(majorEvent.subscribed) != 1 || majorEvent.subscribed[0] != injectedTestRoom {
		t.Fatalf("major event subscriptions = %v, want one for %s", majorEvent.subscribed, injectedTestRoom)
	}
}
