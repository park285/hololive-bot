package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
)

func (r *API) claimMutation() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values("X-Admin-Mutation-ID")
		if len(values) != 1 || !session.ValidMutationID(values[0]) {
			httpx.Abort(c, contract.BadRequest("A single UUIDv4 X-Admin-Mutation-ID is required"))

			return
		}

		sess, ok := sessionFrom(c)
		if !ok {
			httpx.Abort(c, contract.Unauthorized())

			return
		}

		claimed, err := r.sessions.ClaimMutation(c.Request.Context(), *sess, values[0])
		if errors.Is(err, session.ErrFamilyInactive) {
			httpx.Abort(c, contract.Unauthorized())

			return
		}

		if err != nil {
			httpx.Abort(c, &contract.AppError{Status: http.StatusServiceUnavailable, Body: contract.ErrorResponse{Code: "MUTATION_ADMISSION_UNAVAILABLE", Error: "Mutation admission unavailable"}})

			return
		}

		if !claimed {
			c.Set("admin-mutation-duplicate", true)
			httpx.Abort(c, &contract.AppError{Status: http.StatusConflict, Body: contract.ErrorResponse{Code: "MUTATION_ALREADY_ATTEMPTED", Error: "Mutation already attempted; outcome unknown"}})

			return
		}

		contract.MarkMutationClaimed(c.Request.Context(), values[0])
		c.Next()
	}
}
