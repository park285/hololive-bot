package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type xSpaceSessionResponse struct {
	Status     string         `json:"status"`
	Connection xspaces.Status `json:"connection"`
	Accepted   bool           `json:"accepted"`
}

// GetXSpaceSession은 인증 값 없이 연결 상태와 마지막 성공 시각을 조회한다.
func (h *Handler) GetXSpaceSession(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	status, err := h.xSpaceSessions.Status(ctx)
	if err != nil {
		sharedserver.RespondError(c, 503, "X session status unavailable", nil)

		return
	}

	ginjson.Respond(c, 200, xSpaceSessionResponse{Status: "ok", Connection: status})
}

// SubmitXSpaceSession은 후보를 암호화하여 저장한다. 검증과 실제 교체는 worker가 수행한다.
// Accepted=false이면 비활성 또는 다른 제출과의 충돌이며 기존 세션을 덮어쓰지 않는다.
func (h *Handler) SubmitXSpaceSession(c *gin.Context) {
	var request struct {
		AuthToken        string `json:"authToken"`
		CSRFToken        string `json:"csrfToken"`
		ExpectedRevision string `json:"expectedRevision"`
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)

	if err := bindJSON(c, &request); err != nil {
		sharedserver.RespondError(c, 400, "invalid X session request", nil)

		return
	}

	cookies := xspaces.Cookies{AuthToken: request.AuthToken, CSRFToken: request.CSRFToken}
	if err := cookies.Validate(); err != nil {
		sharedserver.RespondError(c, 400, "invalid X session cookies", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)

	defer cancel()

	accepted := false

	if h.xSpaceSessions != nil {
		var err error

		accepted, err = h.xSpaceSessions.Submit(ctx, cookies, request.ExpectedRevision)
		if err != nil {
			sharedserver.RespondError(c, 503, "X session submission outcome unconfirmed; read status", nil)

			return
		}
	}

	status, err := h.xSpaceSessions.Status(ctx)
	if err != nil {
		sharedserver.RespondError(c, 503, "X session submission outcome unconfirmed; read status", nil)

		return
	}

	ginjson.Respond(c, 200, xSpaceSessionResponse{Status: "ok", Connection: status, Accepted: accepted})
}
