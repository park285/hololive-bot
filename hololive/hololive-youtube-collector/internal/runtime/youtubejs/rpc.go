package youtubejs

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type RPC struct {
	http      *http.Client
	endpoint  string
	limiter   *RateLimiter
	bodyLimit int64
	metrics   *rpcMetrics
}

func NewRPC(httpClient *http.Client, endpoint string, limiter *RateLimiter) *RPC {
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

func normalizeCommunityPosts(posts []*CommunityPost) {
	for _, post := range posts {
		if post == nil || post.PublishedAt != nil || post.PublishedText == "" {
			continue
		}

		if publishedAt, ok := yttimestamp.ParsePublishedAt(post.PublishedText); ok {
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

func (c *RPC) FetchChannel(ctx context.Context, request ChannelRequest) (ChannelResult, error) {
	request.ProtocolVersion = ProtocolVersion

	limit, err := c.successLimit(request.MaxSuccessResponseBytes)
	if err != nil {
		return ChannelResult{}, fmt.Errorf("success limit: %w", err)
	}

	request.MaxSuccessResponseBytes = limit

	request.MaxScheduleLookups = 0
	if request.Kind == "live" {
		request.MaxScheduleLookups = ChannelLiveMaxScheduleLookups
	} else {
		request.ScheduledFor = nil
	}

	var sent atomic.Bool

	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			sent.Store(true)
		}
	}})

	result, err := c.doJSON[ChannelResult](traced, "/v1/channel", &request, int64(request.MaxSuccessResponseBytes))
	if err != nil {
		return ChannelResult{}, c.payFailedScheduleLookups(ctx, err, request.MaxScheduleLookups, sent.Load())
	}

	lookups := 0

	if result.ScheduleLookups != nil {
		lookups = *result.ScheduleLookups
	}

	if lookups > request.MaxScheduleLookups {
		c.reserveLimiter(request.MaxScheduleLookups)

		return ChannelResult{}, protocolMismatch(errors.New("youtube.js helper schedule lookups exceed the requested budget"))
	}

	if err := c.payScheduleLookups(ctx, lookups); err != nil {
		return ChannelResult{}, err
	}

	return *result, nil
}

func (c *RPC) payFailedScheduleLookups(ctx context.Context, err error, budget int, sent bool) error {
	failure, ok := errors.AsType[*scheduleLookupError](err)
	if !ok {
		if sent {
			c.reserveLimiter(budget)
		}

		return err
	}

	if failure.lookups > budget {
		c.reserveLimiter(budget)

		return protocolMismatch(errors.New("youtube.js helper schedule lookups exceed the requested budget"))
	}

	if payErr := c.payScheduleLookups(ctx, failure.lookups); payErr != nil {
		return errors.Join(err, payErr)
	}

	return err
}

func (c *RPC) payScheduleLookups(ctx context.Context, lookups int) error {
	for paid := range lookups {
		if err := c.observeLimiterWait(ctx, "/v1/channel"); err != nil {
			c.reserveLimiter(lookups - paid)

			return fmt.Errorf("pay schedule lookup: %w", err)
		}
	}

	return nil
}

func (c *RPC) reserveLimiter(calls int) {
	if c.limiter != nil {
		c.limiter.Reserve(calls)
	}
}

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
		return nil, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "youtube.js client is not configured")
	}

	if successLimit < minimumSuccessResponseBytes(path) {
		return nil, collecterr.New(contract.ErrorResponseTooLarge, contract.ClassResourceLimit, "youtube.js helper success response metadata exceeds requested limit")
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
		return nil, collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, "youtube.js helper response is nil")
	}

	response := new(T)
	if err := decodeHelperResponse(resp, successLimit, response); err != nil {
		if failure, ok := errors.AsType[*scheduleLookupError](err); ok && failure.lookups > 0 && path != "/v1/channel" {
			return nil, protocolMismatch(errors.New("youtube.js helper reported schedule lookups outside the channel endpoint"))
		}

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
	if size, ok := minimumSuccessBytes[path]; ok {
		return size
	}

	return 1
}

var minimumSuccessBytes = map[string]int64{
	"/v1/community": encodedSize(&CommunityResult{
		ProtocolVersion: ProtocolVersion, PageCount: 1, Exhausted: true,
		Continuity: string(contract.ContinuityContiguous), TerminationReason: TerminationExhausted,
		Posts: []*CommunityPost{},
	}),
	"/v1/content": encodedSize(&ContentResult{
		ProtocolVersion: ProtocolVersion, PageCount: 1, Exhausted: true,
		Continuity: string(contract.ContinuityContiguous), TerminationReason: TerminationExhausted,
		Items: []ContentItem{},
	}),
	"/v1/channel": encodedSize(&ChannelResult{
		ProtocolVersion: ProtocolVersion, PageCount: 1, Exhausted: true,
		Continuity: string(contract.ContinuityNotApplicable), TerminationReason: TerminationExhausted,
		LiveSessions: []LiveSessionItem{}, Photo: []ChannelPhotoVariant{},
	}),
	"/v1/channel_live_check": encodedSize(&ChannelLiveCheckResult{
		ProtocolVersion: ProtocolVersion, Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	}),
	"/v1/video_live_check": min(
		encodedSize(&VideoLiveCheckResult{
			ProtocolVersion: ProtocolVersion, Availability: contract.VideoAvailabilityUnknown,
			Method: contract.VideoAvailabilityMethodUnknown, UnknownReason: contract.LiveCheckReasonIdentityMissing,
		}),
		encodedSize(&VideoLiveCheckResult{
			ProtocolVersion: ProtocolVersion, IdentityConfirmed: true,
			Availability: contract.VideoAvailabilityMembersOnly, Method: contract.VideoAvailabilityMethodPlayerMembersOnly,
		}),
	),
}

func encodedSize(value any) int64 {
	raw, err := jsonv2.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode minimal youtube.js helper success response: %v", err))
	}

	return int64(len(raw))
}

func (c *RPC) newJSONRequest(ctx context.Context, path string, request any) (*http.Request, error) {
	raw, err := jsonv2.Marshal(request)
	if err != nil {
		return nil, collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassProtocol, fmt.Errorf("marshal youtube.js helper request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return nil, collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassProtocol, fmt.Errorf("build youtube.js helper request: %w", err))
	}

	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func decodeHelperResponse(resp *http.Response, limit int64, response any) error {
	if resp == nil || resp.Body == nil {
		return collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, "youtube.js helper response is nil")
	}

	if err := validateJSONContentType(resp.Header.Get("Content-Type")); err != nil {
		return errors.Join(protocolMismatch(err), closeHTTPResponse(resp))
	}

	bodyLimit := helperResponseLimit(resp.StatusCode, limit)

	payload, err := readHelperBody(resp, bodyLimit)
	if err != nil {
		return fmt.Errorf("read helper body: %w", err)
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
	payload, readErr := httputil.ReadAllLimited(resp.Body, limit)
	closeErr := resp.Body.Close()

	if errors.Is(readErr, httputil.ErrResponseBodyTooLarge) {
		return nil, oversizedHelperResponse(resp.StatusCode)
	}

	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, collecterr.FromContext(fmt.Errorf("read youtube.js helper: %w", err))
	}

	return payload, nil
}

func oversizedHelperResponse(status int) error {
	if status == http.StatusOK {
		return collecterr.New(contract.ErrorResponseTooLarge, contract.ClassResourceLimit, "youtube.js helper success response exceeds body limit")
	}

	return protocolMismatch(errors.New("youtube.js helper error response exceeds body limit"))
}

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
		return collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "youtube.js helper success response type has no result contract")
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
		return collecterr.FromContext(fmt.Errorf("wait for youtube.js rate limiter: %w", err))
	}

	return nil
}
