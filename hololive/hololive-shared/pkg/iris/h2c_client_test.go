package iris

import (
	"context"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(handler)
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
	okMux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	okSrv := newTestServer(t, okMux)
	okClient := NewH2CClient(okSrv.URL, token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !okClient.Ping(ctx) {
		t.Fatalf("Ping() = false, want true")
	}

	failMux := http.NewServeMux()
	failMux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
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

	transport, ok := client.client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http.Transport", client.client.Transport)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, 90*time.Second)
	}
	if transport.TLSHandshakeTimeout != 5*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, 5*time.Second)
	}
	if transport.ResponseHeaderTimeout != 5*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 5*time.Second)
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
	})
	if client == nil || client.client == nil {
		t.Fatalf("client or http client is nil")
	}
	if client.client.Timeout != 20*time.Second {
		t.Fatalf("Timeout = %v, want %v", client.client.Timeout, 20*time.Second)
	}

	transport, ok := client.client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport type = %T, want *http.Transport", client.client.Transport)
	}
	if transport.IdleConnTimeout != 70*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, 70*time.Second)
	}
	if transport.TLSHandshakeTimeout != 8*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, 8*time.Second)
	}
	if transport.ResponseHeaderTimeout != 9*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 9*time.Second)
	}
}
