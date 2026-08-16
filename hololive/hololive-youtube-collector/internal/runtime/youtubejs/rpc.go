package youtubejs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kapu/hololive-shared/pkg/httpbody"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func NewRPC(httpClient *http.Client, endpoint string, limiter *ratelimiter.RateLimiter) *RPC {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHelperTimeout}
	}
	return &RPC{
		http:      httpClient,
		endpoint:  strings.TrimRight(endpoint, "/"),
		limiter:   limiter,
		bodyLimit: defaultHelperBodyLimit,
	}
}

func (c *RPC) SetProxyEnabled(enabled bool) bool {
	if c == nil {
		return false
	}
	c.proxyOn.Store(enabled)
	return true
}

func (c *RPC) ProxyEnabled() bool {
	return c != nil && c.proxyOn.Load()
}

func (c *RPC) SetProxyURL(proxyURL string) {
	if c == nil {
		return
	}
	c.proxyURL = strings.TrimSpace(proxyURL)
}

func (c *RPC) FetchCommunity(ctx context.Context, request CommunityRequest) (CommunityResult, error) {
	var result CommunityResult
	if err := c.doJSON(ctx, "/v1/community", &request, &result); err != nil {
		return CommunityResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return CommunityResult{}, err
	}
	normalizeCommunityPosts(result.Posts)
	return result, nil
}

func normalizeCommunityPosts(posts []*parser.CommunityPost) {
	for _, post := range posts {
		if post == nil || post.PublishedAt != nil || post.PublishedText == "" {
			continue
		}
		if publishedAt, ok := parser.NormalizePublishedAtCandidate(post.PublishedText); ok {
			post.PublishedAt = publishedAt
		}
	}
}

func (c *RPC) FetchContent(ctx context.Context, request ContentRequest) (ContentResult, error) {
	var result ContentResult
	if err := c.doJSON(ctx, "/v1/content", &request, &result); err != nil {
		return ContentResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return ContentResult{}, err
	}
	return result, nil
}

func (c *RPC) FetchChannel(ctx context.Context, request ChannelRequest) (ChannelResult, error) {
	var result ChannelResult
	if err := c.doJSON(ctx, "/v1/channel", &request, &result); err != nil {
		return ChannelResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return ChannelResult{}, err
	}
	return result, nil
}

func (c *RPC) FetchViewer(ctx context.Context, request ViewerRequest) (ViewerResult, error) {
	var result ViewerResult
	if err := c.doJSON(ctx, "/v1/viewer", &request, &result); err != nil {
		return ViewerResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return ViewerResult{}, err
	}
	return result, nil
}

func resultError(message string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	return collecterr.New(collecterr.Failed, "youtube.js helper: "+message)
}

func (c *RPC) doJSON(ctx context.Context, path string, request, response any) error {
	if c == nil || c.http == nil {
		return collecterr.New(collecterr.Failed, "youtube.js client is not configured")
	}
	if err := c.waitLimiter(ctx); err != nil {
		return err
	}
	req, err := c.newJSONRequest(ctx, path, request)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		closeErr := closeHTTPResponse(resp)
		return errors.Join(collecterr.FromContext(fmt.Errorf("youtube.js helper: %w", err)), closeErr)
	}
	if resp == nil || resp.Body == nil {
		return collecterr.New(collecterr.Failed, "youtube.js helper response is nil")
	}
	return decodeHelperResponse(resp, c.bodyLimit, response)
}

func (c *RPC) newJSONRequest(ctx context.Context, path string, request any) (*http.Request, error) {
	if setter, ok := request.(proxySetter); ok && c.ProxyEnabled() {
		setter.setProxyURL(c.proxyURL)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("marshal youtube.js helper request: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("build youtube.js helper request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func decodeHelperResponse(resp *http.Response, limit int64, response any) error {
	if resp == nil || resp.Body == nil {
		return collecterr.New(collecterr.Failed, "youtube.js helper response is nil")
	}
	if limit <= 0 {
		limit = defaultHelperBodyLimit
	}
	payload, readErr := httpbody.ReadAllAndDrain(resp.Body, limit)
	closeErr := resp.Body.Close()
	err := errors.Join(readErr, closeErr)
	if err != nil {
		if errors.Is(err, httpbody.ErrTooLarge) {
			return errors.Join(collecterr.New(collecterr.ParserDrift, "youtube.js helper response exceeds body limit"), err)
		}
		return collecterr.FromContext(fmt.Errorf("read youtube.js helper: %w", err))
	}
	if err := json.Unmarshal(payload, response); err != nil {
		return collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode youtube.js helper: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		return helperStatusError(resp.StatusCode, payload)
	}
	return nil
}

func closeHTTPResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	drainErr := httpbody.Drain(resp.Body, httpbody.DefaultDrainLimit)
	return errors.Join(drainErr, resp.Body.Close())
}

func helperStatusError(status int, payload []byte) error {
	var decoded struct {
		ErrorClass string `json:"error_class"`
		Error      string `json:"error"`
		Code       string `json:"error_code"`
	}
	decodeErr := json.Unmarshal(payload, &decoded)
	errText := strings.TrimSpace(decoded.Error)
	if errText == "" {
		errText = strings.TrimSpace(string(payload))
	}
	code := decoded.Code
	if code == "" {
		code = collecterr.Failed
	}
	statusErr := collecterr.WrapClass(code, decoded.ErrorClass, fmt.Errorf("youtube.js helper status %d: %s", status, errText))
	if decodeErr != nil {
		return errors.Join(statusErr, fmt.Errorf("decode youtube.js helper error response: %w", decodeErr))
	}
	return statusErr
}

func (c *RPC) waitLimiter(ctx context.Context) error {
	if c.limiter == nil {
		return nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return collecterr.FromContext(fmt.Errorf("wait for youtube.js rate limiter: %w", err))
	}
	return nil
}

type proxySetter interface {
	setProxyURL(string)
}

func (r *CommunityRequest) setProxyURL(value string) { r.ProxyURL = value }
func (r *ContentRequest) setProxyURL(value string)   { r.ProxyURL = value }
func (r *ChannelRequest) setProxyURL(value string)   { r.ProxyURL = value }
func (r *ViewerRequest) setProxyURL(value string)    { r.ProxyURL = value }
