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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/iris-client-go/v2/webhook"
	json "github.com/park285/shared-go/pkg/json"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
)

func TestBotProcessMessage_PreservesThreadIDForReply(t *testing.T) {
	t.Parallel()

	b, reqCh := newReplyCaptureBot(t, 1)
	threadID := "12345"
	handleHelpMessage(t, b, "stable-message-1")

	select {
	case req := <-reqCh:
		require.NotNil(t, req.ThreadID)
		require.Equal(t, threadID, *req.ThreadID)
		require.NotNil(t, req.ClientRequestID)
		require.Equal(t, "hololive:v1:message:stable-message-1:reply:0", *req.ClientRequestID)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive /reply request in time")
	}
}

func TestBotProcessMessage_UsesInboundIDForThreadedReplyIdentity(t *testing.T) {
	t.Parallel()

	b, reqCh := newReplyCaptureBot(t, 2)
	threadID := "12345"
	handleHelpMessage(t, b, "stable-message-1")
	handleHelpMessage(t, b, "stable-message-2")

	first := receiveReplyRequest(t, reqCh)
	second := receiveReplyRequest(t, reqCh)
	require.NotNil(t, first.ThreadID)
	require.NotNil(t, second.ThreadID)
	require.Equal(t, threadID, *first.ThreadID)
	require.Equal(t, threadID, *second.ThreadID)
	require.Equal(t, first.Data, second.Data)
	require.NotNil(t, first.ClientRequestID)
	require.NotNil(t, second.ClientRequestID)
	require.Equal(t, "hololive:v1:message:stable-message-1:reply:0", *first.ClientRequestID)
	require.Equal(t, "hololive:v1:message:stable-message-2:reply:0", *second.ClientRequestID)
}

func TestBotProcessMessage_UsesStableInboundIDForThreadedReplyRetry(t *testing.T) {
	t.Parallel()

	b, reqCh := newReplyCaptureBot(t, 2)
	threadID := "12345"
	for range 2 {
		handleHelpMessage(t, b, "stable-message-1")
	}

	first := receiveReplyRequest(t, reqCh)
	second := receiveReplyRequest(t, reqCh)
	require.NotNil(t, first.ThreadID)
	require.NotNil(t, second.ThreadID)
	require.Equal(t, threadID, *first.ThreadID)
	require.Equal(t, threadID, *second.ThreadID)
	require.NotNil(t, first.ClientRequestID)
	require.NotNil(t, second.ClientRequestID)
	require.Equal(t, "hololive:v1:message:stable-message-1:reply:0", *first.ClientRequestID)
	require.Equal(t, *first.ClientRequestID, *second.ClientRequestID,
		"a redelivered inbound message must reuse the same clientRequestId so iris can dedup it")
}

func newReplyCaptureBot(t *testing.T, capacity int) (bot *Bot, replies <-chan iris.ReplyRequest) {
	t.Helper()

	reqCh := make(chan iris.ReplyRequest, capacity)

	mux := http.NewServeMux()
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		var req iris.ReplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		reqCh <- req
		if err := json.NewEncoder(w).Encode(iris.ReplyAcceptedResponse{
			RequestID: "reply-capture",
			Success:   true,
			Delivery:  "queued",
			Room:      req.Room,
			Type:      req.Type,
		}); err != nil {
			t.Fatalf("encode reply response: %v", err)
		}
	})
	mux.HandleFunc("/reply-status/reply-capture", func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(iris.ReplyStatusSnapshot{
			RequestID: "reply-capture",
			State:     "handoff_completed",
		}); err != nil {
			t.Fatalf("encode reply status response: %v", err)
		}
	})

	srv := httptest.NewUnstartedServer(mux)
	sharedserver.EnableH2C(srv.Config)
	srv.Start()
	t.Cleanup(srv.Close)

	irisClient := iris.NewH2CClient(srv.URL, "bot-token", iris.WithTransport("h2c"))
	b := &Bot{
		logger:          newBotTestLogger(),
		commandRegistry: command.NewRegistry(),
		messageAdapter:  messaging.NewMessageAdapter("!", ""),
		irisClient:      irisClient,
		formatter:       formatter.NewResponseFormatter("!", nil),
	}

	return b, reqCh
}

func handleHelpMessage(t *testing.T, b *Bot, messageID string) {
	t.Helper()

	threadID := "12345"
	sender := "user"
	require.NoError(t, b.ProcessMessage(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   "room-name",
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID:    "user-1",
			ChatID:    "room-1",
			MessageID: messageID,
			ThreadID:  &threadID,
		},
	}))
}

func receiveReplyRequest(t *testing.T, reqCh <-chan iris.ReplyRequest) iris.ReplyRequest {
	t.Helper()

	select {
	case req := <-reqCh:
		return req
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive /reply request in time")
	}

	return iris.ReplyRequest{}
}
