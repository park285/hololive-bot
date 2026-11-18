package api

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestGetMembersAlwaysReturnsAliasesObject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }})

	cache, err := member.NewMemberCache(t.Context(), repo, sharedtestutil.NewTestCacheService(t.Context(), t), logger, member.CacheConfig{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(cache.Close)

	handler := newMemberHandler(Deps{Repository: repo, MemberCache: cache, Logger: logger})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/holo/members",
		bytes.NewBufferString(`{"name":"Alias Contract Member","channelId":"UC_alias_contract","isGraduated":false}`))
	handler.AddMember(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", nil)
	handler.GetMembers(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("read status=%d", recorder.Code)
	}

	var response struct {
		Members []map[string]any `json:"members"`
	}

	if err := jsonv2.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	for _, actual := range response.Members {
		if actual["name"] != "Alias Contract Member" {
			continue
		}

		want := map[string]any{"ko": []any{}, "ja": []any{}}
		if !reflect.DeepEqual(actual["aliases"], want) {
			t.Fatalf("aliases = %#v, want empty ko/ja arrays", actual["aliases"])
		}

		return
	}

	t.Fatal("created member absent")
}
