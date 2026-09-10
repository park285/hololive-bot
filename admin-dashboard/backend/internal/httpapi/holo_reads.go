package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/adapters/holo"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

func holoRead[T any](read func(context.Context) (T, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := requireQuery(c.Request); err != nil {
			httpx.Abort(c, err)

			return
		}

		response, err := read(c.Request.Context())
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		httpx.Respond(c, http.StatusOK, &response)
	}
}

func holoStreamRead(read func(context.Context, *string) (holo.StreamsResponse, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		query, err := requireQuery(c.Request, "org")
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		var org *string

		if values, provided := query["org"]; provided {
			org = &values[0]
		}

		response, err := read(c.Request.Context(), org)
		if err != nil {
			httpx.Abort(c, err)

			return
		}

		httpx.Respond(c, http.StatusOK, response)
	}
}

func requireQuery(req *http.Request, keys ...string) (url.Values, error) {
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return nil, contract.BadRequest("invalid query")
	}

	for key, values := range query {
		if !slices.Contains(keys, key) || len(values) != 1 {
			return nil, contract.BadRequest("unsupported or repeated query parameter")
		}
	}

	return query, nil
}

func (r *API) handleHoloCalendar(c *gin.Context) {
	query, err := requireQuery(c.Request, "month", "year")
	if err != nil {
		httpx.Abort(c, err)

		return
	}

	params := holo.CalendarQuery{}

	for key := range query {
		value, parseErr := strconv.Atoi(query.Get(key))
		if parseErr != nil {
			httpx.Abort(c, contract.BadRequest("query must be an integer"))

			return
		}

		switch key {
		case "month":
			params.Month = &value
		case "year":
			params.Year = &value
		default:
			httpx.Abort(c, contract.BadRequest("unsupported query"))

			return
		}
	}

	response, err := r.holo.GetCalendar(c.Request.Context(), params)
	if err != nil {
		httpx.Abort(c, err)

		return
	}

	httpx.Respond(c, http.StatusOK, response)
}
