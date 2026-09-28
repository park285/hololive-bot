package api

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

// 관리 plane의 ACL 변경이 PG에는 커밋됐지만 봇 plane Reload가 실패하면, 응답은 저장 실패와 구분되는
// acl_bot_resync_failed 5xx다. 원인을 치운 뒤 같은 요청을 다시 보내면 원래 결과와 함께 봇 판정이 수렴한다.
func TestRoomHandlerACLBotApplyFailureReturns5xxAndRetryConverges(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []aclBotApplyRetryCase{
		{
			name:        "set enabled",
			body:        `{"enabled":false}`,
			call:        (*RoomHandler).SetACL,
			retryStatus: http.StatusOK,
			botApplied: func(s *acl.Service) bool {
				enabled, _, _ := s.GetACLStatus()

				return !enabled
			},
		},
		{
			name:        "add room",
			body:        `{"room":"3001"}`,
			call:        (*RoomHandler).AddRoom,
			retryStatus: http.StatusConflict,
			botApplied: func(s *acl.Service) bool {
				return s.IsRoomAllowed("3001")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runACLBotApplyRetryCase(t, tc)
		})
	}
}

type aclBotApplyRetryCase struct {
	name        string
	body        string
	call        func(*RoomHandler, *gin.Context)
	retryStatus int
	botApplied  func(*acl.Service) bool
}

func runACLBotApplyRetryCase(t *testing.T, tc aclBotApplyRetryCase) {
	t.Helper()

	pool := dbtest.NewPool(t)
	client := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	logger := newDiscardLogger()

	adminACL, err := acl.NewACLService(t.Context(), client, logger, true, acl.ACLModeWhitelist, nil)
	if err != nil {
		t.Fatalf("admin NewACLService: %v", err)
	}

	botACL, err := acl.NewACLService(t.Context(), client, logger, true, acl.ACLModeWhitelist, nil)
	if err != nil {
		t.Fatalf("bot NewACLService: %v", err)
	}

	botACL.Follow(adminACL)

	handler := &RoomHandler{Handler: &Handler{acl: adminACL, logger: logger}}

	// 봇 plane Reload만 실패시킨다: 관리 plane은 mode를 이미 메모리에 들고 있어 저장은 성공한다.
	setStoredMode(t, pool, "corrupt")

	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/rooms", []byte(tc.body))
	tc.call(handler, ctx)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=500 body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Error string `json:"error"`
	}

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload.Error != aclBotResyncFailedCode {
		t.Fatalf("error code=%q (unmarshal err %v), want %q", payload.Error, err, aclBotResyncFailedCode)
	}

	if tc.botApplied(botACL) {
		t.Fatal("bot plane must keep its previous snapshot while reload fails")
	}

	setStoredMode(t, pool, string(acl.ACLModeWhitelist))

	ctx, rec = newAPITestContext(http.MethodPost, "/api/holo/rooms", []byte(tc.body))
	tc.call(handler, ctx)

	if rec.Code != tc.retryStatus {
		t.Fatalf("retry status=%d want=%d body=%s", rec.Code, tc.retryStatus, rec.Body.String())
	}

	if !tc.botApplied(botACL) {
		t.Fatal("bot plane must converge after the same request is retried")
	}
}

func setStoredMode(t *testing.T, pool *pgxpool.Pool, mode string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), "UPDATE acl_settings SET value = $1 WHERE key = 'mode'", mode); err != nil {
		t.Fatalf("set stored ACL mode: %v", err)
	}
}
