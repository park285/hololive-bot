package api

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestMemberCreatePreservesBasicInfoOnReadback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	cache, err := member.NewMemberCache(t.Context(), repo, nil, logger, member.CacheConfig{})
	if err != nil {
		t.Fatal(err)
	}

	handler := &MemberHandler{Handler: &Handler{repository: repo, memberCache: cache, logger: logger}}
	body := `{"name":"Created Info Member","nameKo":"등록 멤버","org":"New Org","units":["Gen 1","GAMERS"],"officialUrl":"https://example.com/member","birthday":"2000-02-29T00:00:00Z","debutDate":"2025-10-15T00:00:00Z","shortKoreanName":"등록","chzzkChannelId":"registered-chzzk","aliases":{"ko":["등록"],"ja":[]}}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/holo/members", bytes.NewBufferString(body))
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

	var response memberListResponse

	if err = jsonv2.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	for _, actual := range response.Members {
		if actual.Name == "Created Info Member" {
			assertCreatedBasicInfo(t, actual)

			return
		}
	}

	t.Fatal("created member absent")
}

func assertCreatedBasicInfo(t *testing.T, actual *domain.Member) {
	t.Helper()

	if !slices.Equal(actual.Units, []string{"Gen 1", "GAMERS"}) || actual.OfficialURL != "https://example.com/member" {
		t.Fatalf("units/link lost: %+v", actual)
	}

	if actual.Birthday == nil || actual.DebutDate == nil {
		t.Fatal("dates lost")
	}

	if actual.Birthday.Format(time.DateOnly) != "2000-02-29" || actual.DebutDate.Format(time.DateOnly) != "2025-10-15" {
		t.Fatal("dates changed")
	}

	if actual.Org != "New Org" || actual.ShortKoreanName != "등록" || actual.ChzzkChannelID != "registered-chzzk" {
		t.Fatalf("basic info lost: %+v", actual)
	}
}
