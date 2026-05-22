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

package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestDomainHandlers_WiringAndNilReceiver(t *testing.T) {
	// nil receiver도 안전하게 동작해야 함
	var nilHandler *Handler

	got := nilHandler.DomainHandlers()
	if got == nil {
		t.Fatal("DomainHandlers returned nil")
	}

	if got.Member == nil || got.Alarm == nil || got.OAuth == nil {
		t.Fatal("domain sub-handlers must not be nil")
	}

	if got.Member.Handler == nil {
		t.Fatal("embedded Handler should be initialized")
	}

	if got.Member.streamState == nil {
		t.Fatal("embedded Handler streamState should be initialized")
	}

	if got.Member.startTime.IsZero() {
		t.Fatal("embedded Handler startTime should be initialized")
	}

	if got.Member.Handler != got.Alarm.Handler || got.Member.Handler != got.OAuth.Handler {
		t.Fatal("all domain handlers should share same Handler instance")
	}

	base := &Handler{}

	wired := base.DomainHandlers()
	if wired.Member.Handler != base || wired.Template.Handler != base {
		t.Fatal("expected all domain handlers to reference original Handler")
	}

	if wired.Member.streamState == nil {
		t.Fatal("zero-value Handler should gain streamState defaults")
	}

	if wired.Member.startTime.IsZero() {
		t.Fatal("zero-value Handler should gain startTime defaults")
	}
}

func TestNewHandler_BasicInitialization(t *testing.T) {
	h := NewHandler(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil,
		slog.New(slog.DiscardHandler),
	)
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}

	if h.streamState == nil {
		t.Fatal("streamState should be initialized")
	}

	if h.memberIndexLoader != nil {
		t.Fatal("memberIndexLoader should be nil when repository is nil")
	}

	if h.ensureStreamState() != h.streamState {
		t.Fatal("ensureStreamState should return same streamState pointer")
	}

	repoBacked := NewHandler(
		&member.Repository{}, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil,
		nil,
	)
	if repoBacked.memberIndexLoader == nil {
		t.Fatal("memberIndexLoader should be set when repository is provided")
	}
}

func TestHandler_EnsureDefaults_BackfillsDerivedFields(t *testing.T) {
	h := (&Handler{}).ensureDefaults()
	if h.streamState == nil {
		t.Fatal("streamState should be initialized")
	}

	if h.startTime.IsZero() {
		t.Fatal("startTime should be initialized")
	}

	repoBacked := (&Handler{repository: &member.Repository{}}).ensureDefaults()
	if repoBacked.memberIndexLoader == nil {
		t.Fatal("memberIndexLoader should be derived from repository")
	}
}

func TestHandler_RespondHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.DiscardHandler)

	t.Run("respondError", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

		sharedserver.RespondError(ctx, http.StatusBadRequest, "bad request", gin.H{"field": "email"})

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusBadRequest)
		}

		body := rec.Body.String()
		if !bytes.Contains([]byte(body), []byte(`"error":"bad request"`)) || !bytes.Contains([]byte(body), []byte(`"field":"email"`)) {
			t.Fatalf("unexpected body: %s", body)
		}
	})

	t.Run("respondInternalError", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

		sharedserver.RespondInternalError(logger, ctx, "internal", "log-message", io.EOF)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
		}

		if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"internal"`)) {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})
}

func TestOAuthCallbackHandler_HTMLResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	oauth := &OAuthHandler{}
	router.GET("/oauth/callback", oauth.OAuthCallbackHandler)

	t.Run("success path", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/oauth/callback?code=abc&state=xyz", http.NoBody)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusOK)
		}

		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Fatalf("content-type=%q", ct)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "window.location.href") || !strings.Contains(body, "code=abc") || !strings.Contains(body, "state=xyz") {
			t.Fatalf("expected deep-link script fields in body, got: %s", body)
		}
	})

	t.Run("error path", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/oauth/callback?error=access_denied&error_description=denied", http.NoBody)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "window.location.href") || !strings.Contains(body, "error=access_denied") || !strings.Contains(body, "error_description=denied") {
			t.Fatalf("expected error deep-link script fields in body, got: %s", body)
		}

		if !strings.Contains(body, "로그인 실패") {
			t.Fatalf("expected error page status label, got: %s", body)
		}
	})
}
