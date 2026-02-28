package app

import (
	"context"
	"testing"
)

type testWebhookCloser struct {
	calls int
	err   error
}

func (c *testWebhookCloser) Close() error {
	c.calls++
	return c.err
}

func TestBotRuntimeShutdown_ClosesWebhookHandler(t *testing.T) {
	webhookCloser := &testWebhookCloser{}
	runtime := &BotRuntime{
		webhookHandlerCloser: webhookCloser,
	}

	runtime.Shutdown(context.Background())

	if webhookCloser.calls != 1 {
		t.Fatalf("webhook Close calls = %d, want %d", webhookCloser.calls, 1)
	}
}
