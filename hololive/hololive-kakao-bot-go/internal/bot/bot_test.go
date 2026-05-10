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

package bot

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/iris"
)

type fakeIrisClient struct {
	failCount int
	pingCalls int
}

func (f *fakeIrisClient) SendMessage(context.Context, string, string, ...iris.SendOption) error {
	return nil
}
func (f *fakeIrisClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}
func (f *fakeIrisClient) SendImage(context.Context, string, []byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (f *fakeIrisClient) SendMultipleImages(context.Context, string, [][]byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (f *fakeIrisClient) SendMarkdown(_ context.Context, _, _ string, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (f *fakeIrisClient) GetReplyStatus(_ context.Context, _ string) (*iris.ReplyStatusSnapshot, error) {
	return nil, nil
}
func (f *fakeIrisClient) GetConfig(context.Context) (*iris.ConfigResponse, error) {
	return &iris.ConfigResponse{}, nil
}
func (f *fakeIrisClient) Decrypt(context.Context, string) (string, error) { return "", nil }
func (f *fakeIrisClient) Ping(context.Context) bool {
	f.pingCalls++
	if f.failCount > 0 {
		f.failCount--
		return false
	}

	return true
}

func TestWaitUntilIrisReady_SucceedsAfterRetry(t *testing.T) {
	t.Parallel()

	client := &fakeIrisClient{failCount: 2}
	b := &Bot{
		irisClient: client,
		logger:     slog.New(slog.DiscardHandler),
	}

	err := b.waitUntilIrisReady(t.Context(), 300*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("waitUntilIrisReady failed: %v", err)
	}

	if client.pingCalls < 3 {
		t.Fatalf("expected at least 3 ping attempts, got %d", client.pingCalls)
	}
}

func TestWaitUntilIrisReady_TimesOut(t *testing.T) {
	t.Parallel()

	client := &fakeIrisClient{failCount: 1000}
	b := &Bot{
		irisClient: client,
		logger:     slog.New(slog.DiscardHandler),
	}

	err := b.waitUntilIrisReady(t.Context(), 70*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}
