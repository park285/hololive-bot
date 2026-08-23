package youtubejs

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type RPC struct {
	http      *http.Client
	endpoint  string
	limiter   *ratelimiter.RateLimiter
	bodyLimit int64
}

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

func (c *RPC) FetchCommunity(ctx context.Context, request CommunityRequest) (CommunityResult, error) {
	request.ProtocolVersion = ProtocolVersion
	limit, err := c.successLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return CommunityResult{}, err
	}
	request.MaxSuccessResponseBytes = limit
	var result CommunityResult
	if err := c.doJSON(ctx, "/v1/community", &request, &result, int64(request.MaxSuccessResponseBytes)); err != nil {
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
	request.ProtocolVersion = ProtocolVersion
	limit, err := c.successLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return ContentResult{}, err
	}
	request.MaxSuccessResponseBytes = limit
	var result ContentResult
	if err := c.doJSON(ctx, "/v1/content", &request, &result, int64(request.MaxSuccessResponseBytes)); err != nil {
		return ContentResult{}, err
	}
	return result, nil
}

func (c *RPC) FetchChannel(ctx context.Context, request ChannelRequest) (ChannelResult, error) {
	request.ProtocolVersion = ProtocolVersion
	limit, err := c.successLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return ChannelResult{}, err
	}
	request.MaxSuccessResponseBytes = limit
	var result ChannelResult
	if err := c.doJSON(ctx, "/v1/channel", &request, &result, int64(request.MaxSuccessResponseBytes)); err != nil {
		return ChannelResult{}, err
	}
	return result, nil
}

func (c *RPC) FetchViewer(ctx context.Context, request ViewerRequest) (ViewerResult, error) {
	request.ProtocolVersion = ProtocolVersion
	limit, err := c.successLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return ViewerResult{}, err
	}
	request.MaxSuccessResponseBytes = limit
	var result ViewerResult
	if err := c.doJSON(ctx, "/v1/viewer", &request, &result, int64(request.MaxSuccessResponseBytes)); err != nil {
		return ViewerResult{}, err
	}
	return result, nil
}

func (c *RPC) successLimit(requested int) (int, error) {
	configured := defaultHelperBodyLimit
	if c.bodyLimit > 0 {
		configured = c.bodyLimit
	}
	if requested <= 0 {
		return int(configured), nil
	}
	if int64(requested) > configured {
		return 0, protocolMismatch(fmt.Errorf("youtube.js helper success response limit exceeds bootstrap limit"))
	}
	return requested, nil
}

func (c *RPC) doJSON(ctx context.Context, path string, request, response any, successLimit int64) error {
	if c == nil || c.http == nil {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js client is not configured")
	}
	if successLimit < minimumSuccessResponseBytes(path) {
		return collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "youtube.js helper success response metadata exceeds requested limit")
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
		return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "youtube.js helper response is nil")
	}
	return decodeHelperResponse(resp, successLimit, response)
}

func minimumSuccessResponseBytes(path string) int64 {
	switch path {
	case "/v1/community", "/v1/content":
		return 124
	case "/v1/channel":
		return 171
	case "/v1/viewer":
		return 161
	default:
		return 1
	}
}

func (c *RPC) newJSONRequest(ctx context.Context, path string, request any) (*http.Request, error) {
	raw, err := jsonv2.Marshal(request)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, fmt.Errorf("marshal youtube.js helper request: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, fmt.Errorf("build youtube.js helper request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func decodeHelperResponse(resp *http.Response, limit int64, response any) error {
	if resp == nil || resp.Body == nil {
		return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "youtube.js helper response is nil")
	}
	if err := validateJSONContentType(resp.Header.Get("Content-Type")); err != nil {
		return errors.Join(protocolMismatch(err), closeHTTPResponse(resp))
	}
	bodyLimit := helperResponseLimit(resp.StatusCode, limit)
	payload, err := readHelperBody(resp, bodyLimit)
	if err != nil {
		return err
	}
	if int64(len(payload)) > bodyLimit {
		return oversizedHelperResponse(resp.StatusCode)
	}
	if resp.StatusCode == http.StatusOK {
		return decodeHelperSuccess(payload, response)
	}
	return helperStatusError(resp.StatusCode, payload)
}

func helperResponseLimit(status int, requested int64) int64 {
	if status != http.StatusOK {
		return 8 << 10
	}
	if requested <= 0 {
		return defaultHelperBodyLimit
	}
	return requested
}

func readHelperBody(resp *http.Response, limit int64) ([]byte, error) {
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err := errors.Join(readErr, resp.Body.Close()); err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read youtube.js helper: %w", err))
	}
	return payload, nil
}

func oversizedHelperResponse(status int) error {
	if status == http.StatusOK {
		return collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "youtube.js helper success response exceeds body limit")
	}
	return protocolMismatch(fmt.Errorf("youtube.js helper error response exceeds body limit"))
}

func decodeHelperSuccess(payload []byte, response any) error {
	if err := strictDecode(payload, response); err != nil {
		return protocolMismatch(fmt.Errorf("decode youtube.js helper success response: %w", err))
	}
	meta, ok := protocolMetaOf(response)
	if !ok || meta.ProtocolVersion != ProtocolVersion {
		return protocolMismatch(fmt.Errorf("youtube.js helper success protocol version mismatch"))
	}
	pagination, ok := paginationOf(response)
	if !ok {
		return protocolMismatch(fmt.Errorf("youtube.js helper success pagination is missing"))
	}
	if err := pagination.Validate(); err != nil {
		return protocolMismatch(err)
	}
	return nil
}

func closeHTTPResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	return resp.Body.Close()
}

func strictDecode(payload []byte, dst any) error {
	return jsonv2.Unmarshal(payload, dst, jsonv2.RejectUnknownMembers(true))
}

func protocolMetaOf(response any) (ProtocolMeta, bool) {
	value, ok := response.(interface{ protocolMetadata() ProtocolMeta })
	if !ok {
		return ProtocolMeta{}, false
	}
	return value.protocolMetadata(), true
}

func paginationOf(response any) (Pagination, bool) {
	value, ok := response.(interface{ pagination() Pagination })
	if !ok {
		return Pagination{}, false
	}
	return value.pagination(), true
}

func validateJSONContentType(raw string) error {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("youtube.js helper response content type is not application/json")
	}
	return nil
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
