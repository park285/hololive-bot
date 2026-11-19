package orchestration

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func testBotLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func callNewBotSafely(deps *Dependencies) (created *Bot, recovered any, err error) {
	defer func() {
		recovered = recover()
	}()

	created, err = NewBot(deps)
	if err != nil {
		return nil, nil, err
	}

	return created, recovered, nil
}

func TestNewBot_FailFastOnNilDependencies(t *testing.T) {
	postgres := databasemocks.NewUnconnectedClient(t)

	tests := []struct {
		name    string
		deps    *Dependencies
		wantErr string
	}{
		{
			name:    "nil dependencies",
			deps:    nil,
			wantErr: "bot dependencies are required",
		},
		{
			name:    "nil logger",
			deps:    &Dependencies{},
			wantErr: "logger dependency is required",
		},
		{
			name: "nil iris client",
			deps: &Dependencies{
				Logger: testBotLogger(),
			},
			wantErr: "iris client dependency is required",
		},
		{
			name: "nil reply outbox",
			deps: &Dependencies{
				Logger: testBotLogger(),
				Client: &fakeIrisClient{},
			},
			wantErr: "reply outbox dependency is required",
		},
		{
			name: "nil message adapter",
			deps: &Dependencies{
				Logger:      testBotLogger(),
				Client:      &fakeIrisClient{},
				ReplyOutbox: dispatchingReplyOutbox{},
			},
			wantErr: "message adapter dependency is required",
		},
		{
			name: "nil holodex stream provider",
			deps: &Dependencies{
				Logger:         testBotLogger(),
				Client:         &fakeIrisClient{},
				ReplyOutbox:    dispatchingReplyOutbox{},
				MessageAdapter: &messaging.MessageAdapter{},
				Formatter:      &formatter.ResponseFormatter{},
				Cache:          &cache.Service{},
				Postgres:       postgres,
			},
			wantErr: "holodex dependency is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			created, recovered, err := callNewBotSafely(tc.deps)
			if recovered != nil {
				t.Fatalf("NewBot panic = %v", recovered)
			}

			if err == nil {
				t.Fatalf("NewBot() error = nil, want %q", tc.wantErr)
			}

			if err.Error() != tc.wantErr {
				t.Fatalf("NewBot() error = %q, want %q", err.Error(), tc.wantErr)
			}

			if created != nil {
				t.Fatal("NewBot() result must be nil on fail-fast error")
			}
		})
	}
}

func TestNewBot_FailFastOnPostgresWithoutPool(t *testing.T) {
	created, err := NewBot(&Dependencies{
		Logger:         testBotLogger(),
		Client:         &fakeIrisClient{},
		ReplyOutbox:    dispatchingReplyOutbox{},
		MessageAdapter: &messaging.MessageAdapter{},
		Formatter:      &formatter.ResponseFormatter{},
		Cache:          &cache.Service{},
		Postgres:       &database.PostgresService{},
	})
	if err == nil || err.Error() != "postgres pool is required" || created != nil {
		t.Fatalf("NewBot() = %v, %v; want nil, postgres pool is required", created, err)
	}
}

func TestNewBot_FailFastOnMissingMemberRepository(t *testing.T) {
	created, err := NewBot(&Dependencies{
		Logger:         testBotLogger(),
		Client:         &fakeIrisClient{},
		ReplyOutbox:    dispatchingReplyOutbox{},
		MessageAdapter: &messaging.MessageAdapter{},
		Formatter:      &formatter.ResponseFormatter{},
		Cache:          &cache.Service{},
		Postgres:       databasemocks.NewUnconnectedClient(t),
		Holodex:        struct{ domain.StreamProvider }{},
		Alarm:          struct{ handlercore.AlarmService }{},
		Matcher:        &matcher.Matcher{},
		MembersData:    struct{ domain.MemberDataProvider }{},
	})
	if err == nil || err.Error() != "member repository dependency is required" || created != nil {
		t.Fatalf("NewBot() = %v, %v; want nil, member repository dependency is required", created, err)
	}
}
