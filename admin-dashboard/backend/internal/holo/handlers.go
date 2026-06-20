package holo

import (
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/admin-dashboard/internal/httpx"
)

const MaxChannelStatsLimit = 500

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

func (h Handler) ChannelStats(c *gin.Context) {
	query := c.Request.URL.Query()
	limit, err := validateChannelStatsLimit(query.Get("limit"))
	if err != nil {
		httpx.Abort(c, err)
		return
	}
	query.Del("limit")
	resp, err := h.Client.Proxy(c.Request.Context(), http.MethodGet, "/api/holo/stats/channels", query, nil)
	if err != nil {
		httpx.Abort(c, err)
		return
	}
	if limit > 0 {
		resp.Body, err = TrimChannelStats(resp.Body, limit)
		if err != nil {
			httpx.Abort(c, err)
			return
		}
	}
	writeProxyJSON(c, resp)
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

func validateChannelStatsLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, httpx.BadRequest("invalid channel stats limit")
	}
	if limit > MaxChannelStatsLimit {
		return 0, httpx.BadRequest("channel stats limit is too large")
	}
	return limit, nil
}

func TrimChannelStats(body []byte, limit int) ([]byte, error) {
	if limit <= 0 {
		return body, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, httpx.BadGateway()
	}
	statsRaw, ok := raw["stats"]
	if !ok {
		return body, nil
	}
	var stats map[string]json.RawMessage
	if err := json.Unmarshal(statsRaw, &stats); err != nil {
		return nil, httpx.BadGateway()
	}
	if len(stats) <= limit {
		return body, nil
	}
	encoded, err := json.Marshal(topChannelStats(stats, limit))
	if err != nil {
		return nil, err
	}
	raw["stats"] = encoded
	return json.Marshal(raw)
}

func topChannelStats(stats map[string]json.RawMessage, limit int) map[string]json.RawMessage {
	type item struct {
		key string
		sub int64
	}
	items := make([]item, 0, len(stats))
	for key, payload := range stats {
		var value struct {
			SubscriberCount int64 `json:"subscriberCount"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			continue
		}
		items = append(items, item{key: key, sub: value.SubscriberCount})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].sub == items[j].sub {
			return items[i].key < items[j].key
		}
		return items[i].sub > items[j].sub
	})
	trimmed := make(map[string]json.RawMessage, limit)
	for _, item := range items[:limit] {
		trimmed[item.key] = stats[item.key]
	}
	return trimmed
}

func closeRequestBody(body interface{ Close() error }) {
	if err := body.Close(); err != nil {
		return
	}
}
