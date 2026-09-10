package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/observations"
)

func (r *API) newStreams(timing observations.StreamTiming) (*observations.Streams, error) {
	streams, err := observations.NewStreams(r.statsHub, r.logger, observations.StreamPolicy{
		AllowedOrigins: r.cfg.Security.AllowedOrigins, OriginMode: r.cfg.Security.WSOriginMode,
		Authorize:    r.authorizeStream,
		FamilyActive: func(ctx context.Context, family string) (bool, error) { return r.sessions.FamilyActive(ctx, family) },
		Reject:       httpx.Error,
	}, timing)
	if err != nil {
		return nil, fmt.Errorf("create administrator streams: %w", err)
	}

	return streams, nil
}

func (r *API) authorizeStream(request *http.Request) (string, error) {
	_, sess, err := r.resolveSession(request)
	if err != nil {
		return "", err
	}

	if sess == nil || sess.FamilyID == "" {
		return "", contract.Unauthorized()
	}

	return sess.FamilyID, nil
}

func (r *API) handleSystemStatsWS(c *gin.Context) { r.streams.ServeHTTP(c.Writer, c.Request) }
