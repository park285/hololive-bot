package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/park285/iris-client-go/v5/webhooksign"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type bodyTimeoutWebhookEffects struct {
	nonces atomic.Int32
	admits atomic.Int32
}

func (s *bodyTimeoutWebhookEffects) IsDuplicate(context.Context, string, time.Duration) (bool, error) {
	s.nonces.Add(1)

	return false, nil
}

func (*bodyTimeoutWebhookEffects) SetOnceNonce() {}

func (s *bodyTimeoutWebhookEffects) AdmitMessage(context.Context, *webhook.Message, []byte) error {
	s.admits.Add(1)

	return nil
}

func TestBotHTTP3SlowWebhookBodyExitsBeforeAuthentication(t *testing.T) {
	certFile, keyFile := writeLocalhostCertificate(t)
	cfg := &apiconfig.BotPlaneConfig{Server: settings.ServerConfig{
		H3Addr: testLoopbackAddr, H3CertFile: certFile, H3KeyFile: keyFile,
	}}

	synctest.Test(t, func(t *testing.T) {
		effects := &bodyTimeoutWebhookEffects{}

		handler, err := webhook.NewDurableHandler(effects,
			webhook.WithContext(t.Context()),
			webhook.WithWebhookToken("body-timeout-test-server"),
			webhook.WithNonceStore(effects),
			webhook.WithWebhookLogger(slog.New(slog.DiscardHandler)),
		)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { require.NoError(t, handler.Close()) }()

		server, _, err := BuildBotHTTP3Server(t.Context(), cfg, handler, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}

		reader, writer := io.Pipe()

		defer func() { require.NoError(t, reader.Close()); require.NoError(t, writer.Close()) }()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://localhost/webhook/iris", reader)
		req.Header.Set(webhook.HeaderIrisMessageID, "slow-body-test")

		if err := webhooksign.SignRequest(req, "untrusted-test-sender", nil); err != nil {
			t.Fatal(err)
		}

		req.Body = reader
		req.ContentLength = -1
		req.GetBody = nil

		recorder := httptest.NewRecorder()
		done := make(chan struct{})

		go func() {
			defer close(done)

			server.Handler.ServeHTTP(recorder, req)
		}()

		synctest.Wait()
		// 가상 시계만 진행하고 FIN은 보내지 않아 실제 본문 읽기의 종료를 확인한다.
		time.Sleep(time.Minute)
		synctest.Wait()

		select {
		case <-done:
		default:
			require.NoError(t, writer.Close())

			<-done
			t.Fatal("webhook handler remained blocked waiting for body FIN")
		}

		if recorder.Code != http.StatusRequestTimeout {
			t.Fatalf("status = %d, want 408", recorder.Code)
		}

		if effects.nonces.Load() != 0 || effects.admits.Load() != 0 {
			t.Fatalf("pre-auth side effects: nonces=%d admits=%d", effects.nonces.Load(), effects.admits.Load())
		}
	})
}
