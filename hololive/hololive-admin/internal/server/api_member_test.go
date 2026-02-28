package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-admin/internal/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/member"
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	memberCache, err := member.NewMemberCache(context.Background(), nil, nil, logger, member.CacheConfig{})
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
	req := httptest.NewRequest(method, "/api/holo/members/1/aliases", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	return ctx, rec
}
