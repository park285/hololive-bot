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

package iris

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(h2c.NewHandler(handler, &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestH2CClient_SendMessage_WithoutThreadID(t *testing.T) {
	token := "bot-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Bot-Token"); got != token {
			t.Fatalf("X-Bot-Token = %q, want %q", got, token)
		}

		var req ReplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.Type != "text" {
			t.Fatalf("type = %q, want %q", req.Type, "text")
		}
		if req.Room != "room1" {
			t.Fatalf("room = %q, want %q", req.Room, "room1")
		}
		if req.Data != "hello" {
			t.Fatalf("data = %q, want %q", req.Data, "hello")
		}
		if req.ThreadID != nil {
			t.Fatalf("threadId = %v, want nil", *req.ThreadID)
		}

		w.WriteHeader(http.StatusOK)
	})

	srv := newTestServer(t, mux)
	c := NewH2CClient(srv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.SendMessage(ctx, "room1", "hello"); err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
}

func TestH2CClient_SendMessage_WithThreadID(t *testing.T) {
	token := "bot-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}

		var req ReplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.ThreadID == nil || *req.ThreadID != "thread-1" {
			if req.ThreadID == nil {
				t.Fatalf("threadId = nil, want %q", "thread-1")
			}
			t.Fatalf("threadId = %q, want %q", *req.ThreadID, "thread-1")
		}

		w.WriteHeader(http.StatusOK)
	})

	srv := newTestServer(t, mux)
	c := NewH2CClient(srv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.SendMessage(ctx, "room1", "hello", WithThreadID("thread-1")); err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
}

func TestH2CClient_SendImage_SendsExpectedSchema(t *testing.T) {
	token := "bot-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}

		var req ReplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.Type != "image" {
			t.Fatalf("type = %q, want %q", req.Type, "image")
		}
		if req.Room != "room1" {
			t.Fatalf("room = %q, want %q", req.Room, "room1")
		}
		if req.Data != "b64" {
			t.Fatalf("data = %q, want %q", req.Data, "b64")
		}
		if req.ThreadID != nil {
			t.Fatalf("threadId = %q, want nil", *req.ThreadID)
		}

		w.WriteHeader(http.StatusOK)
	})

	srv := newTestServer(t, mux)
	c := NewH2CClient(srv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.SendImage(ctx, "room1", "b64"); err != nil {
		t.Fatalf("SendImage error: %v", err)
	}
}

func TestH2CClient_GetConfig_ParsesResponse(t *testing.T) {
	token := "bot-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bot_name":"hololive","bot_http_port":3000,"db_polling_rate":10,"message_send_rate":5,"bot_id":123}`))
	})

	srv := newTestServer(t, mux)
	c := NewH2CClient(srv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg, err := c.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig error: %v", err)
	}
	if cfg.BotName != "hololive" {
		t.Fatalf("BotName = %q, want %q", cfg.BotName, "hololive")
	}
	if cfg.BotHTTPPort != 3000 {
		t.Fatalf("BotHTTPPort = %d, want %d", cfg.BotHTTPPort, 3000)
	}
	if cfg.DBPollingRate != 10 {
		t.Fatalf("DBPollingRate = %d, want %d", cfg.DBPollingRate, 10)
	}
	if cfg.MessageSendRate != 5 {
		t.Fatalf("MessageSendRate = %d, want %d", cfg.MessageSendRate, 5)
	}
	if cfg.BotID != 123 {
		t.Fatalf("BotID = %d, want %d", cfg.BotID, 123)
	}
}

func TestH2CClient_Ping_ReturnsStatus(t *testing.T) {
	token := "bot-token"

	okMux := http.NewServeMux()
	okMux.HandleFunc(sharedirisx.PathReady, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	okSrv := newTestServer(t, okMux)
	okClient := NewH2CClient(okSrv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !okClient.Ping(ctx) {
		t.Fatalf("Ping() = false, want true")
	}

	failMux := http.NewServeMux()
	failMux.HandleFunc(sharedirisx.PathReady, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	failSrv := newTestServer(t, failMux)
	failClient := NewH2CClient(failSrv.URL, token, nil)

	if failClient.Ping(ctx) {
		t.Fatalf("Ping() = true, want false")
	}
}

func TestH2CClient_Decrypt_SendsExpectedSchema(t *testing.T) {
	token := "bot-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/decrypt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", got, "application/json")
		}

		var req DecryptRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.B64Ciphertext != "cipher" {
			t.Fatalf("b64_ciphertext = %q, want %q", req.B64Ciphertext, "cipher")
		}
		if req.Enc != 0 {
			t.Fatalf("enc = %d, want %d", req.Enc, 0)
		}
		if req.UserID != nil {
			t.Fatalf("user_id = %d, want nil", *req.UserID)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plain_text":"hello"}`))
	})

	srv := newTestServer(t, mux)
	c := NewH2CClient(srv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	plain, err := c.Decrypt(ctx, "cipher")
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if plain != "hello" {
		t.Fatalf("Decrypt() = %q, want %q", plain, "hello")
	}
}

func TestNewH2CClient_DefaultTimeouts(t *testing.T) {
	t.Parallel()

	client := NewH2CClient("http://iris.local", "bot-token", nil)
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}
	if client.client.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %v, want %v", client.client.Timeout, 10*time.Second)
	}

	transport, ok := client.client.Transport.(*http2.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http2.Transport", client.client.Transport)
	}
	if !transport.AllowHTTP {
		t.Fatalf("AllowHTTP = false, want true")
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, 90*time.Second)
	}
	if transport.ReadIdleTimeout != 30*time.Second {
		t.Fatalf("ReadIdleTimeout = %v, want %v", transport.ReadIdleTimeout, 30*time.Second)
	}
	if transport.PingTimeout != 15*time.Second {
		t.Fatalf("PingTimeout = %v, want %v", transport.PingTimeout, 15*time.Second)
	}
	if transport.WriteByteTimeout != 10*time.Second {
		t.Fatalf("WriteByteTimeout = %v, want %v", transport.WriteByteTimeout, 10*time.Second)
	}
	if transport.DialTLSContext == nil {
		t.Fatalf("DialTLSContext is nil")
	}
}

func TestNewH2CClient_CustomTimeouts(t *testing.T) {
	t.Parallel()

	client := NewH2CClient("http://iris.local", "bot-token", nil, H2CClientOptions{
		Timeout:               20 * time.Second,
		DialTimeout:           7 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 9 * time.Second,
		IdleConnTimeout:       70 * time.Second,
		MaxIdleConns:          24,
		MaxIdleConnsPerHost:   18,
		ReadIdleTimeout:       31 * time.Second,
		PingTimeout:           11 * time.Second,
		WriteByteTimeout:      12 * time.Second,
	})
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}
	if client.client.Timeout != 20*time.Second {
		t.Fatalf("Timeout = %v, want %v", client.client.Timeout, 20*time.Second)
	}

	transport, ok := client.client.Transport.(*http2.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http2.Transport", client.client.Transport)
	}
	if transport.IdleConnTimeout != 70*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, 70*time.Second)
	}
	if transport.ReadIdleTimeout != 31*time.Second {
		t.Fatalf("ReadIdleTimeout = %v, want %v", transport.ReadIdleTimeout, 31*time.Second)
	}
	if transport.PingTimeout != 11*time.Second {
		t.Fatalf("PingTimeout = %v, want %v", transport.PingTimeout, 11*time.Second)
	}
	if transport.WriteByteTimeout != 12*time.Second {
		t.Fatalf("WriteByteTimeout = %v, want %v", transport.WriteByteTimeout, 12*time.Second)
	}
	if transport.DialTLSContext == nil {
		t.Fatalf("DialTLSContext is nil")
	}
}

func TestH2CClient_UsesHTTP2ForHTTPBaseURL(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var gotProtoMajor int

	mux := http.NewServeMux()
	mux.HandleFunc(sharedirisx.PathReady, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotProtoMajor = r.ProtoMajor
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	srv := newTestServer(t, mux)
	client := NewH2CClient(srv.URL, "bot-token", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !client.Ping(ctx) {
		t.Fatalf("Ping() = false, want true")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotProtoMajor != 2 {
		t.Fatalf("request proto major = %d, want 2", gotProtoMajor)
	}
}

func TestNewH2CClient_HTTPS_UsesHTTPTransport(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	client := NewH2CClient(srv.URL, "bot-token", nil)
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}

	transport, ok := client.client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http.Transport", client.client.Transport)
	}
	if transport.TLSHandshakeTimeout != 5*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, 5*time.Second)
	}
	if transport.ResponseHeaderTimeout != 5*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 5*time.Second)
	}
	if transport.MaxIdleConns != 10 {
		t.Fatalf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, 10)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, 10)
	}

	// tls server와 통신할 수 있어야 합니다.
	client.client.Transport = transport.Clone()
	client.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- test-only.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !client.Ping(ctx) {
		t.Fatalf("Ping() = false, want true")
	}
}

func TestNewH2CClient_HTTPTransportOverride_UsesCustomIdlePoolSettings(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	client := NewH2CClient("http://iris.local", "bot-token", nil, H2CClientOptions{
		MaxIdleConns:        21,
		MaxIdleConnsPerHost: 13,
		IdleConnTimeout:     44 * time.Second,
	})
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}

	transport, ok := client.client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http.Transport", client.client.Transport)
	}
	if transport.MaxIdleConns != 21 {
		t.Fatalf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, 21)
	}
	if transport.MaxIdleConnsPerHost != 13 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, 13)
	}
	if transport.IdleConnTimeout != 44*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, 44*time.Second)
	}
}

func TestNewH2CClient_HTTPTransportOverride_UsesHTTP1ForHTTPBaseURL(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	var mu sync.Mutex
	var gotProtoMajor int

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotProtoMajor = r.ProtoMajor
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := NewH2CClient(srv.URL, "bot-token", nil)
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}

	transport, ok := client.client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http.Transport", client.client.Transport)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !client.Ping(ctx) {
		t.Fatalf("Ping() = false, want true")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotProtoMajor != 1 {
		t.Fatalf("request proto major = %d, want 1", gotProtoMajor)
	}
}

func TestNewH2CClient_UnknownTransportOverride_FallsBackToH2C(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "weird-mode")

	client := NewH2CClient("http://iris.local", "bot-token", nil)
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}

	if _, ok := client.client.Transport.(*http2.Transport); !ok {
		t.Fatalf("transport type = %T, want *http2.Transport", client.client.Transport)
	}
}
