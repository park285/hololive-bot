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
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type RPC struct {
	http      *http.Client
	endpoint  string
	limiter   *ratelimiter.RateLimiter
	bodyLimit int64
	metrics   *rpcMetrics
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
		return CommunityResult{}, fmt.Errorf("success limit: %w", err)
	}

	request.MaxSuccessResponseBytes = limit

	result, err := c.doJSON[CommunityResult](ctx, "/v1/community", &request, int64(request.MaxSuccessResponseBytes))
	if err != nil {
		return CommunityResult{}, err
	}

	normalizeCommunityPosts(result.Posts)

	return *result, nil
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
		return ContentResult{}, fmt.Errorf("success limit: %w", err)
	}

	request.MaxSuccessResponseBytes = limit

	result, err := c.doJSON[ContentResult](ctx, "/v1/content", &request, int64(request.MaxSuccessResponseBytes))
	if err != nil {
		return ContentResult{}, err
	}

	return *result, nil
}

// FetchChannel은 필수 Kind에 지정된 범위만 조회하며 접근 제한 목록을 정상 sessions와 구분해 반환합니다.
func (c *RPC) FetchChannel(ctx context.Context, request ChannelRequest) (ChannelResult, error) {
	request.ProtocolVersion = ProtocolVersion

	limit, err := c.successLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return ChannelResult{}, fmt.Errorf("success limit: %w", err)
	}

	request.MaxSuccessResponseBytes = limit

	result, err := c.doJSON[ChannelResult](ctx, "/v1/channel", &request, int64(request.MaxSuccessResponseBytes))
	if err != nil {
		return ChannelResult{}, err
	}

	return *result, nil
}

// FetchChannelLiveCheck는 채널 /live 확인을 한 번 요청합니다. 응답 예산은 MaxLiveCheckResponseBytes 이하로 제한하며
// 반환 channel_id와 요청 subject의 대조는 호출자가 소유합니다.
func (c *RPC) FetchChannelLiveCheck(ctx context.Context, request ChannelLiveCheckRequest) (ChannelLiveCheckResult, error) {
	request.ProtocolVersion = ProtocolVersion

	limit, err := c.liveCheckSuccessLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return ChannelLiveCheckResult{}, fmt.Errorf("live check success limit: %w", err)
	}

	request.MaxSuccessResponseBytes = limit

	result, err := c.doJSON[ChannelLiveCheckResult](ctx, "/v1/channel_live_check", &request, int64(limit))
	if err != nil {
		return ChannelLiveCheckResult{}, err
	}

	return *result, nil
}

// FetchVideoLiveCheck는 영상 player 확인을 한 번 요청합니다. 반환 video_id와 요청 subject의 대조는 호출자가 소유합니다.
func (c *RPC) FetchVideoLiveCheck(ctx context.Context, request VideoLiveCheckRequest) (VideoLiveCheckResult, error) {
	request.ProtocolVersion = ProtocolVersion

	limit, err := c.liveCheckSuccessLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return VideoLiveCheckResult{}, fmt.Errorf("live check success limit: %w", err)
	}

	request.MaxSuccessResponseBytes = limit

	result, err := c.doJSON[VideoLiveCheckResult](ctx, "/v1/video_live_check", &request, int64(limit))
	if err != nil {
		return VideoLiveCheckResult{}, err
	}

	return *result, nil
}

func (c *RPC) liveCheckSuccessLimit(requested int) (int, error) {
	limit, err := c.successLimit(requested)
	if err != nil {
		return 0, fmt.Errorf("success limit: %w", err)
	}

	return min(limit, MaxLiveCheckResponseBytes), nil
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
		return 0, protocolMismatch(errors.New("youtube.js helper success response limit exceeds bootstrap limit"))
	}

	return requested, nil
}

func (c *RPC) doJSON[T any](ctx context.Context, path string, request any, successLimit int64) (result *T, resultErr error) {
	if c == nil || c.http == nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js client is not configured")
	}

	if successLimit < minimumSuccessResponseBytes(path) {
		return nil, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "youtube.js helper success response metadata exceeds requested limit")
	}

	if err := c.observeLimiterWait(ctx, path); err != nil {
		return nil, fmt.Errorf("wait limiter: %w", err)
	}

	req, err := c.newJSONRequest(ctx, path, request)
	if err != nil {
		return nil, fmt.Errorf("JSON request: %w", err)
	}

	started := time.Now()

	c.metrics.begin(path, "helper")

	defer func() { c.metrics.end(path, "helper", started, resultErr) }()

	resp, err := c.http.Do(req)
	if err != nil {
		closeErr := closeHTTPResponse(resp)
		return nil, errors.Join(collecterr.FromContext(fmt.Errorf("youtube.js helper: %w", err)), closeErr)
	}

	if invalidHelperHTTPResponse(resp) {
		return nil, collecterr.New(collecterr.Failed, collecterr.ClassProtocol, "youtube.js helper response is nil")
	}

	response := new(T)
	if err := decodeHelperResponse(resp, successLimit, response); err != nil {
		return nil, fmt.Errorf("decode helper response: %w", err)
	}

	return response, nil
}

func (c *RPC) observeLimiterWait(ctx context.Context, path string) (err error) {
	started := time.Now()

	c.metrics.begin(path, "rate_limit")

	defer func() { c.metrics.end(path, "rate_limit", started, err) }()

	return c.waitLimiter(ctx)
}

func invalidHelperHTTPResponse(resp *http.Response) bool {
	return resp == nil || resp.Body == nil
}

func minimumSuccessResponseBytes(path string) int64 {
	switch path {
	case "/v1/community", "/v1/content":
		return 124
	case "/v1/channel":
		return 171
	case "/v1/channel_live_check":
		return 97
	case "/v1/video_live_check":
		return 139
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
		return fmt.Errorf("read helper body: %w", err)
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

	return protocolMismatch(errors.New("youtube.js helper error response exceeds body limit"))
}

// helperSuccess는 성공 응답 유형마다 자신의 결과 계약을 검증하게 합니다.
// 목록 RPC는 pagination을, 확인 RPC는 평탄한 판정 어휘를 검증합니다.
type helperSuccess interface {
	protocolMetadata() ProtocolMeta
	validateSuccess() error
}

func decodeHelperSuccess(payload []byte, response any) error {
	if err := strictDecode(payload, response); err != nil {
		return protocolMismatch(fmt.Errorf("decode youtube.js helper success response: %w", err))
	}

	success, ok := response.(helperSuccess)
	if !ok {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "youtube.js helper success response type has no result contract")
	}

	if success.protocolMetadata().ProtocolVersion != ProtocolVersion {
		return protocolMismatch(errors.New("youtube.js helper success protocol version mismatch"))
	}

	if err := success.validateSuccess(); err != nil {
		return protocolMismatch(err)
	}

	return nil
}

func closeHTTPResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}

	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

func strictDecode(payload []byte, dst any) error {
	if err := jsonv2.Unmarshal(payload, dst, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}

func validateJSONContentType(raw string) error {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil || mediaType != "application/json" {
		return errors.New("youtube.js helper response content type is not application/json")
	}

	return nil
}

func (c *RPC) waitLimiter(ctx context.Context) error {
	if c.limiter == nil {
		return nil
	}

	if err := c.limiter.Wait(ctx); err != nil {
		if fromContextErr := collecterr.FromContext(fmt.Errorf("wait for youtube.js rate limiter: %w", err)); fromContextErr != nil {
			return fmt.Errorf("from context: %w", fromContextErr)
		}

		return nil
	}

	return nil
}
