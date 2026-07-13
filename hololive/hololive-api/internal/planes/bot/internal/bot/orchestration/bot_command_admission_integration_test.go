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
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/park285/iris-client-go/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
)

func TestBotHandleMessageRejectsUnknownIngressUserForExpensiveCommand(t *testing.T) {
	cacheClient := sharedtestutil.NewTestCacheService(t, t.Context())
	executions := 0
	registry := command.NewRegistry()
	registry.Register(&testCommand{
		name: "broadcast_history",
		execute: func(context.Context, *domain.CommandContext, map[string]any) error {
			executions++
			return nil
		},
	})
	b := &Bot{
		logger:          newBotTestLogger(),
		commandRegistry: registry,
		messageAdapter:  adapter.NewMessageAdapter("!", ""),
		irisClient:      &testIrisClient{},
		formatter:       adapter.NewResponseFormatter("!", nil),
		cache:           cacheClient,
	}
	sender := "user"
	message := &webhook.Message{
		Msg:    "!방송이력",
		Room:   "room-name",
		Sender: &sender,
		JSON:   &webhook.MessageJSON{ChatID: "room-1"},
	}

	b.HandleMessage(t.Context(), message)
	if executions != 0 {
		t.Fatalf("handler executions with unknown ingress user = %d, want 0", executions)
	}

	message.JSON.UserID = "user-1"
	b.HandleMessage(t.Context(), message)
	if executions != 1 {
		t.Fatalf("handler executions after stable ingress user = %d, want 1", executions)
	}
}
