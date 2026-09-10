package httpapi

import (
	"context"
	jsonv2 "encoding/json/v2"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

const maxHoloRequestBodyBytes = 2 << 20

func readHoloBody[T any](request *http.Request) (T, error) {
	var body T

	if request.Body == nil {
		return body, contract.BadRequest("request body is required")
	}

	defer func() { _ = request.Body.Close() }()

	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return body, contract.BadRequest("application/json is required")
	}

	data, err := io.ReadAll(io.LimitReader(request.Body, maxHoloRequestBodyBytes+1))
	if err != nil {
		return body, contract.BadRequest("cannot read request body")
	}

	if len(data) > maxHoloRequestBodyBytes {
		return body, contract.NewError(http.StatusRequestEntityTooLarge, "request body is too large")
	}

	if err := jsonv2.Unmarshal(data, &body, jsonv2.RejectUnknownMembers(true)); err != nil {
		return body, contract.BadRequest("invalid request body")
	}

	return body, nil
}

func holoMutation[In, Out any](mutate func(context.Context, In) (Out, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := requireQuery(c.Request); err != nil {
			httpx.Abort(c, err)

			return
		}

		input, err := readHoloBody[In](c.Request)
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		response, err := mutate(c.Request.Context(), input)
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		httpx.Respond(c, http.StatusOK, response)
	}
}

func holoMemberMutation[In, Out any](mutate func(context.Context, string, In) (Out, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := requireQuery(c.Request); err != nil {
			httpx.Abort(c, err)

			return
		}

		input, err := readHoloBody[In](c.Request)
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		response, err := mutate(c.Request.Context(), c.Param("id"), input)
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		httpx.Respond(c, http.StatusOK, response)
	}
}
