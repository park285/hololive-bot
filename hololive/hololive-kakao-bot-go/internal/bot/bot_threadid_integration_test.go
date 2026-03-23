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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/iris-client-go/iris"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

func TestBotHandleMessage_PreservesThreadIDForReply(t *testing.T) {
	t.Parallel()

	reqCh := make(chan iris.ReplyRequest, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		var req iris.ReplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		select {
		case reqCh <- req:
		default:
		}

		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewUnstartedServer(sharedserver.WrapH2C(mux))
	srv.Start()
	t.Cleanup(srv.Close)

	irisClient := iris.NewH2CClient(srv.URL, "bot-token")
	b := &Bot{
		logger:          newBotTestLogger(),
		commandRegistry: command.NewRegistry(),
		messageAdapter:  adapter.NewMessageAdapter("!", ""),
		irisClient:      irisClient,
		formatter:       adapter.NewResponseFormatter("!", nil),
	}

	threadID := "12345"
	sender := "user"
	b.HandleMessage(t.Context(), &iris.Message{
		Msg:    "!help",
		Room:   "room-name",
		Sender: &sender,
		JSON: &iris.MessageJSON{
			UserID:   "user-1",
			ChatID:   "room-1",
			ThreadID: &threadID,
		},
	})

	select {
	case req := <-reqCh:
		require.NotNil(t, req.ThreadID)
		require.Equal(t, threadID, *req.ThreadID)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive /reply request in time")
	}
}
