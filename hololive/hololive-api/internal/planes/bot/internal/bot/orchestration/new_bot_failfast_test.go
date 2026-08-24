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

package orchestration

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
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
		//nolint:wrapcheck // NewBot의 fail-fast 오류 문구를 그대로 대조하는 헬퍼라, 여기서 감싸면 검증 대상 계약이 사라진다.
		return nil, nil, err
	}

	return created, recovered, nil
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
				MessageAdapter: &messaging.MessageAdapter{},
				Formatter:      &formatter.ResponseFormatter{},
				Cache:          &cache.Service{},
				Postgres:       &database.PostgresService{},
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
