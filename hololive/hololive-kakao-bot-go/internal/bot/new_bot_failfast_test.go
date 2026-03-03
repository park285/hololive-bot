package bot

import (
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func testBotLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func callNewBotSafely(deps *Dependencies) (_ *Bot, _ error, recovered any) {
	defer func() {
		recovered = recover()
	}()
	created, err := NewBot(deps)
	return created, err, nil
}

func TestNewBot_FailFastOnNilDependencies(t *testing.T) {
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
			name: "nil message adapter",
			deps: &Dependencies{
				Logger: testBotLogger(),
				Client: &fakeIrisClient{},
			},
			wantErr: "message adapter dependency is required",
		},
		{
			name: "nil holodex stream provider",
			deps: &Dependencies{
				Logger:         testBotLogger(),
				Client:         &fakeIrisClient{},
				MessageAdapter: &adapter.MessageAdapter{},
				Formatter:      &adapter.ResponseFormatter{},
				Cache:          &cache.Service{},
				Postgres:       &database.PostgresService{},
			},
			wantErr: "holodex dependency is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			created, err, recovered := callNewBotSafely(tc.deps)
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
				t.Fatalf("NewBot() result must be nil on fail-fast error")
			}
		})
	}
}
