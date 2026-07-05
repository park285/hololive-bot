package holo

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/admin-dashboard/internal/httpx"
)

type Handler struct {
	Client *Client
}

func (h Handler) ProxyGet(path string, queryFilter func(url.Values) (url.Values, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Request.URL.Query()
		if queryFilter != nil {
			filtered, err := queryFilter(query)
			if err != nil {
				httpx.Abort(c, err)
				return
			}
			query = filtered
		}
		resp, err := h.Client.Proxy(c.Request.Context(), http.MethodGet, path, query, nil)
		if err != nil {
			httpx.Abort(c, err)
			return
		}
		writeProxyJSON(c, resp)
	}
}

func (h Handler) ProxyMutation(method, path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := readJSONBody(c.Request)
		if err != nil {
			httpx.Abort(c, err)
			return
		}
		resp, err := h.Client.Proxy(c.Request.Context(), method, path, nil, body)
		if err != nil {
			httpx.Abort(c, err)
			return
		}
		writeProxyJSON(c, resp)
	}
}

func (h Handler) ProxyMemberMutation(method, suffix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if strings.TrimSpace(id) == "" {
			httpx.Abort(c, httpx.BadRequest("missing member id"))
			return
		}
		body, err := readJSONBody(c.Request)
		if err != nil {
			httpx.Abort(c, err)
			return
		}
		resp, err := h.Client.Proxy(c.Request.Context(), method, "/api/holo/members/"+url.PathEscape(id)+suffix, nil, body)
		if err != nil {
			httpx.Abort(c, err)
			return
		}
		writeProxyJSON(c, resp)
	}
}

const maxRequestBodyBytes = 2 << 20

func readJSONBody(r *http.Request) ([]byte, error) {
	defer closeRequestBody(r.Body)
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	if err != nil {
		return nil, httpx.BadRequest("invalid json payload")
	}
	if len(data) > maxRequestBodyBytes {
		return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "request payload too large")
	}
	if strings.TrimSpace(string(data)) == "" {
		data = []byte("{}")
	}
	if !json.Valid(data) {
		return nil, httpx.BadRequest("invalid json payload")
	}
	return data, nil
}

func writeProxyJSON(c *gin.Context, resp ProxyResponse) {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Data(resp.StatusCode, contentType, resp.Body)
}

func PassOnly(keys ...string) func(url.Values) (url.Values, error) {
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
	}
	return func(query url.Values) (url.Values, error) {
		return filterQuery(query, allowed), nil
	}
}

func filterQuery(query url.Values, allowed map[string]struct{}) url.Values {
	filtered := url.Values{}
	for key, values := range query {
		if _, ok := allowed[key]; ok {
			filtered[key] = append([]string(nil), values...)
		}
	}
	return filtered
}

func closeRequestBody(body interface{ Close() error }) {
	if err := body.Close(); err != nil {
		return
	}
}
