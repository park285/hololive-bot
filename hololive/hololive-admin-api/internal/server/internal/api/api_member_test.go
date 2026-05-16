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
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/service/member"

	"github.com/kapu/hololive-shared/pkg/service/activity"
)

func TestHandleAliasOperation_NormalizesAliasBeforeRepoCall(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := newAliasOperationTestHandler(t)
	ctx, rec := newAliasOperationTestContext(http.MethodPost, `{"type":"ko","alias":"  새   별칭\t테스트  "}`)

	var gotAlias string

	called := false

	handler.handleAliasOperation(ctx, func(_ context.Context, memberID int, aliasType, alias string) error {
		called = true

		if memberID != 1 {
			t.Fatalf("memberID = %d, want 1", memberID)
		}

		if aliasType != "ko" {
			t.Fatalf("aliasType = %q, want %q", aliasType, "ko")
		}

		gotAlias = alias

		return nil
	}, "add")

	if !called {
		t.Fatal("repoFunc was not called")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if gotAlias != "새 별칭 테스트" {
		t.Fatalf("alias = %q, want %q", gotAlias, "새 별칭 테스트")
	}
}

func TestHandleAliasOperation_RejectsEmptyAliasAfterNormalization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := newAliasOperationTestHandler(t)
	ctx, rec := newAliasOperationTestContext(http.MethodPost, `{"type":"ko","alias":"  \t  "}`)

	called := false

	handler.handleAliasOperation(ctx, func(context.Context, int, string, string) error {
		called = true
		return nil
	}, "add")

	if called {
		t.Fatal("repoFunc should not be called for empty alias")
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleAliasOperation_RejectsTooLongAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := newAliasOperationTestHandler(t)
	longAlias := strings.Repeat("가", aliasMaxLength+1)
	ctx, rec := newAliasOperationTestContext(
		http.MethodPost,
		fmt.Sprintf(`{"type":"ko","alias":"  %s  "}`, longAlias),
	)

	called := false

	handler.handleAliasOperation(ctx, func(context.Context, int, string, string) error {
		called = true
		return nil
	}, "add")

	if called {
		t.Fatal("repoFunc should not be called for too-long alias")
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func newAliasOperationTestHandler(t *testing.T) *MemberAPIHandler {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)

	memberCache, err := member.NewMemberCache(t.Context(), nil, nil, logger, member.CacheConfig{})
	if err != nil {
		t.Fatalf("failed to create member cache: %v", err)
	}

	activityLogger := activity.NewActivityLogger(filepath.Join(t.TempDir(), "activity.log"), logger)

	return &MemberAPIHandler{
		APIHandler: &APIHandler{
			memberCache: memberCache,
			activity:    activityLogger,
			logger:      logger,
		},
	}
}

func newAliasOperationTestContext(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequestWithContext(context.Background(), method, "/api/holo/members/1/aliases", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	ctx.Request = req
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	return ctx, rec
}
