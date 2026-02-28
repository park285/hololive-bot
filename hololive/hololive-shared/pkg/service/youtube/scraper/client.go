package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/ctxutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/retry"
	"golang.org/x/net/proxy"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

// ErrRateLimited: 레이트 리밋 초과 에러
var ErrRateLimited = errors.New("rate limited by YouTube (429)")

// ErrForbidden: 접근 차단 에러
var ErrForbidden = errors.New("forbidden by YouTube (403)")

// ErrChannelNotFound: 채널이 존재하지 않음 (삭제/비활성화)
var ErrChannelNotFound = errors.New("channel does not exist")

// ErrChannelUnavailable: 채널이 일시적으로 사용 불가
var ErrChannelUnavailable = errors.New("channel is unavailable")

// httpStatusError: HTTP 상태 코드 기반 에러 (재시도 판단용)
type httpStatusError struct {
	code int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status code: %d", e.code)
}

func extractHTTPStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	return statusErr.code, true
}

func isRetryableStatusError(err error) bool {
	statusCode, ok := extractHTTPStatusCode(err)
	return ok && isRetryable5xx(statusCode)
}

func isRetryableVideoPageError(err error) bool {
	return isRetryableStatusError(err) || isRetryableTransportError(err)
}

// isRetryable5xx: 5xx 서버 에러인지 확인 (재시도 대상)
func isRetryable5xx(code int) bool {
	switch code {
	case 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// isRetryableTransportError: 네트워크/프록시 계층 일시 장애인지 확인
func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}

	// 호출자 컨텍스트 취소는 재시도하지 않는다.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// 호출자 deadline 초과는 재시도하지 않는다.
	// 단, http.Client 자체 타임아웃은 문자열 시그니처로 구분하여 재시도 허용.
	if errors.Is(err, context.DeadlineExceeded) {
		return hasTransientTransportSignature(err.Error())
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if isTimeoutOrTemporaryError(urlErr) {
			return true
		}
		if urlErr.Err == nil {
			return false
		}
		if isTimeoutOrTemporaryError(urlErr.Err) {
			return true
		}
		return hasTransientTransportSignature(urlErr.Err.Error())
	}

	if isTimeoutOrTemporaryError(err) {
		return true
	}

	return hasTransientTransportSignature(err.Error())
}

type temporaryError interface {
	Temporary() bool
}

func isTimeoutOrTemporaryError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var tempErr temporaryError
	return errors.As(err, &tempErr) && tempErr.Temporary()
}

func hasTransientTransportSignature(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "connection reset by peer") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "http2: timeout awaiting response headers") ||
		strings.Contains(lower, "timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "unexpected eof")
}

// ProxyConfig: 프록시 설정
type ProxyConfig struct {
	Enabled bool   // 프록시 사용 여부
	URL     string // SOCKS5 프록시 URL (예: socks5://user:pass@host:1080)
}

type stateStore interface {
	Get(ctx context.Context, key string, dest any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}

// cacheState: 채널별 boolean 캐시 상태 (in-memory + stateStore 2계층)
type cacheState struct {
	mu    sync.RWMutex
	until map[string]time.Time
	store stateStore
	ttl   time.Duration
	label string // 로그용 라벨
}

func newCacheState(store stateStore, ttl time.Duration, label string) *cacheState {
	return &cacheState{
		until: make(map[string]time.Time),
		store: store,
		ttl:   ttl,
		label: label,
	}
}

func (cs *cacheState) isSet(ctx context.Context, key, stateKey string) bool {
	now := time.Now()
	cs.mu.RLock()
	until, ok := cs.until[key]
	cs.mu.RUnlock()
	if ok {
		if now.Before(until) {
			return true
		}
		cs.mu.Lock()
		latest, exists := cs.until[key]
		if exists && !time.Now().Before(latest) {
			delete(cs.until, key)
		}
		cs.mu.Unlock()
	}

	if cs.store == nil {
		return false
	}

	var marker bool
	if err := cs.store.Get(ctx, stateKey, &marker); err != nil {
		slog.Warn("failed to read "+cs.label+" state",
			"channel_id", key,
			"error", err)
		return false
	}
	if marker {
		cs.mu.Lock()
		cs.until[key] = time.Now().Add(cs.ttl)
		cs.mu.Unlock()
		return true
	}
	return false
}

func (cs *cacheState) mark(ctx context.Context, key, stateKey string) {
	cs.mu.Lock()
	cs.until[key] = time.Now().Add(cs.ttl)
	cs.mu.Unlock()

	if cs.store == nil {
		return
	}
	if err := cs.store.Set(ctx, stateKey, true, cs.ttl); err != nil {
		slog.Warn("failed to persist "+cs.label+" state",
			"channel_id", key,
			"error", err)
	}
}

func (cs *cacheState) clear(ctx context.Context, key, stateKey string) {
	cs.mu.Lock()
	delete(cs.until, key)
	cs.mu.Unlock()

	if cs.store == nil {
		return
	}
	if err := cs.store.Del(ctx, stateKey); err != nil {
		slog.Warn("failed to clear "+cs.label+" state",
			"channel_id", key,
			"error", err)
	}
}

// Client: YouTube HTML 스크래퍼 클라이언트
type Client struct {
	httpClient       *http.Client // 테스트/특수 경로용 고정 클라이언트
	directHTTPClient *http.Client
	proxyHTTPClient  *http.Client
	activeHTTPClient atomic.Pointer[http.Client]
	proxyEnabled     atomic.Bool
	uaProvider       ua.Provider
	rateLimiter      *RateLimiter
	backoffState     *BackoffState
	proxyConfig      ProxyConfig
	stateStore       stateStore

	communityMissing *cacheState
	videoRSSBackoff  *cacheState
}

type distributedLimiter interface {
	Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error)
}

// ClientOption: Client 생성 옵션
type ClientOption func(*Client)

const (
	communityMissingKeyPrefix = "youtube:scraper:community-missing:"
	videoRSSBackoffKeyPrefix  = "youtube:scraper:video-rss-backoff:"
)

// WithHTTPClient: 커스텀 HTTP 클라이언트 설정
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithUAProvider: 커스텀 UA Provider 설정
func WithUAProvider(provider ua.Provider) ClientOption {
	return func(c *Client) {
		c.uaProvider = provider
	}
}

// WithRateLimiter: 커스텀 레이트 리미터 설정
func WithRateLimiter(rl *RateLimiter) ClientOption {
	return func(c *Client) {
		c.rateLimiter = rl
	}
}

// WithProxy: SOCKS5 프록시 설정
func WithProxy(cfg ProxyConfig) ClientOption {
	return func(c *Client) {
		c.proxyConfig = cfg
	}
}

// WithStateStore: scraper 상태(백오프/미지원 채널) 저장소를 주입합니다.
func WithStateStore(store stateStore) ClientOption {
	return func(c *Client) {
		c.stateStore = store
	}
}

// NewClient: 새 스크래퍼 클라이언트 생성
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		uaProvider:   ua.NewRotatingProvider(ua.StrategySessionTTL, 45*time.Minute),
		rateLimiter:  NewRateLimiter(3 * time.Second),
		backoffState: NewBackoffState(),
	}

	// 옵션 적용 (프록시 설정 포함)
	for _, opt := range opts {
		opt(c)
	}

	// stateStore 주입 후 cacheState 초기화
	c.communityMissing = newCacheState(c.stateStore, constants.YouTubeConfig.CommunityMissingTTL, "community missing")
	c.videoRSSBackoff = newCacheState(c.stateStore, constants.YouTubeConfig.VideoRSSBackoffTTL, "video rss backoff")

	// 커스텀 클라이언트가 주입되면 해당 클라이언트만 사용
	if c.httpClient != nil {
		c.activeHTTPClient.Store(c.httpClient)
		c.proxyEnabled.Store(false)
		return c
	}

	directClient, err := createHTTPClient(ProxyConfig{})
	if err != nil {
		slog.Error("Failed to create direct scraper client, using fallback default transport",
			"error", err)
		directClient = &http.Client{Timeout: constants.YouTubeConfig.ScraperHTTPTimeout}
	}
	c.directHTTPClient = directClient

	if c.proxyConfig.URL != "" {
		proxyClient, proxyErr := createHTTPClient(ProxyConfig{Enabled: true, URL: c.proxyConfig.URL})
		if proxyErr != nil {
			slog.Error("Failed to create proxy scraper client, proxy toggle unavailable until restart",
				"error", proxyErr)
		} else {
			c.proxyHTTPClient = proxyClient
		}
	}

	if c.proxyConfig.Enabled && c.proxyHTTPClient != nil {
		c.activeHTTPClient.Store(c.proxyHTTPClient)
		c.proxyEnabled.Store(true)
	} else {
		c.activeHTTPClient.Store(c.directHTTPClient)
		c.proxyEnabled.Store(false)
		if c.proxyConfig.Enabled && c.proxyHTTPClient == nil {
			slog.Warn("Scraper proxy requested but unavailable, starting in direct mode")
		}
	}

	return c
}

// SetProxyEnabled: 런타임에 프록시 사용 여부를 토글합니다.
// proxy client가 준비되지 않았으면 true 요청은 적용되지 않고 direct 모드로 유지됩니다.
func (c *Client) SetProxyEnabled(enabled bool) bool {
	if c == nil {
		return false
	}
	if c.httpClient != nil {
		// 외부 주입 클라이언트는 런타임 토글 대상이 아님
		return false
	}

	if enabled {
		if c.proxyHTTPClient == nil {
			c.proxyEnabled.Store(false)
			if c.directHTTPClient != nil {
				c.activeHTTPClient.Store(c.directHTTPClient)
			}
			return false
		}
		c.activeHTTPClient.Store(c.proxyHTTPClient)
		c.proxyEnabled.Store(true)
		return true
	}

	if c.directHTTPClient == nil {
		return false
	}
	c.activeHTTPClient.Store(c.directHTTPClient)
	c.proxyEnabled.Store(false)
	return true
}

// ProxyEnabled: 현재 런타임 프록시 활성 상태를 반환합니다.
func (c *Client) ProxyEnabled() bool {
	if c == nil {
		return false
	}
	return c.proxyEnabled.Load()
}

func (c *Client) currentHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	if active := c.activeHTTPClient.Load(); active != nil {
		return active
	}
	if c.directHTTPClient != nil {
		return c.directHTTPClient
	}
	return &http.Client{Timeout: constants.YouTubeConfig.ScraperHTTPTimeout}
}

func (c *Client) closeIdleConnections() {
	clients := []*http.Client{
		c.httpClient,
		c.directHTTPClient,
		c.proxyHTTPClient,
		c.activeHTTPClient.Load(),
	}
	seen := make(map[*http.Transport]struct{})
	for _, client := range clients {
		if client == nil {
			continue
		}
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport == nil {
			continue
		}
		if _, exists := seen[transport]; exists {
			continue
		}
		seen[transport] = struct{}{}
		transport.CloseIdleConnections()
	}
}

func (c *Client) communityMissingStateKey(channelID string) string {
	return communityMissingKeyPrefix + strings.TrimSpace(channelID)
}

func (c *Client) videoRSSBackoffStateKey(channelID string) string {
	return videoRSSBackoffKeyPrefix + strings.TrimSpace(channelID)
}

func (c *Client) isCommunityMissing(ctx context.Context, channelID string) bool {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return false
	}
	return c.communityMissing.isSet(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) markCommunityMissing(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}
	c.communityMissing.mark(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) clearCommunityMissing(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}
	c.communityMissing.clear(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) isVideoRSSBackoff(ctx context.Context, channelID string) bool {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return false
	}
	return c.videoRSSBackoff.isSet(ctx, key, c.videoRSSBackoffStateKey(key))
}

func (c *Client) markVideoRSSBackoff(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}
	c.videoRSSBackoff.mark(ctx, key, c.videoRSSBackoffStateKey(key))
}

func (c *Client) clearVideoRSSBackoff(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}
	c.videoRSSBackoff.clear(ctx, key, c.videoRSSBackoffStateKey(key))
}

// createHTTPClient: 프록시 설정에 따라 HTTP 클라이언트 생성
func createHTTPClient(proxyCfg ProxyConfig) (*http.Client, error) {
	baseTransport := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   constants.YouTubeConfig.ScraperDialTimeout,
		ResponseHeaderTimeout: constants.YouTubeConfig.ScraperHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if !proxyCfg.Enabled || proxyCfg.URL == "" {
		slog.Info("Scraper using direct connection (no proxy)")
		dialer := &net.Dialer{
			Timeout:   constants.YouTubeConfig.ScraperDialTimeout,
			KeepAlive: 30 * time.Second,
		}
		baseTransport.DialContext = dialer.DialContext
		return &http.Client{
			Transport: baseTransport,
			Timeout:   constants.YouTubeConfig.ScraperHTTPTimeout,
		}, nil
	}

	parsedURL, err := url.Parse(proxyCfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	// SOCKS5 인증 정보 추출
	var auth *proxy.Auth
	if parsedURL.User != nil {
		password, _ := parsedURL.User.Password()
		auth = &proxy.Auth{
			User:     parsedURL.User.Username(),
			Password: password,
		}
	}

	forwardDialer := &net.Dialer{
		Timeout:   constants.YouTubeConfig.ScraperDialTimeout,
		KeepAlive: 30 * time.Second,
	}

	// SOCKS5 다이얼러 생성
	dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, forwardDialer)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	// Transport에 SOCKS5 다이얼러 설정
	// SOCKS5 경유 시 HTTP/2 비활성화: 단일 터널 위 멀티플렉싱은
	// 프록시 불안정 시 전체 요청 연쇄 실패를 유발한다.
	transport := &http.Transport{
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          baseTransport.MaxIdleConns,
		MaxIdleConnsPerHost:   baseTransport.MaxIdleConnsPerHost,
		IdleConnTimeout:       baseTransport.IdleConnTimeout,
		TLSHandshakeTimeout:   baseTransport.TLSHandshakeTimeout,
		ResponseHeaderTimeout: baseTransport.ResponseHeaderTimeout,
		ExpectContinueTimeout: baseTransport.ExpectContinueTimeout,
	}

	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := contextDialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, fmt.Errorf("proxy dial failed: %w", err)
			}
			if ctx.Err() != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
			}
			return conn, nil
		}
	} else {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialSOCKS5WithContextFallback(ctx, dialer, network, addr)
		}
	}

	slog.Info("Scraper using SOCKS5 proxy",
		"host", parsedURL.Host,
		"has_auth", auth != nil)

	return &http.Client{
		Transport: transport,
		Timeout:   constants.YouTubeConfig.ScraperHTTPTimeout,
	}, nil
}

type dialResult struct {
	conn net.Conn
	err  error
}

func dialSOCKS5WithContextFallback(ctx context.Context, dialer proxy.Dialer, network, addr string) (net.Conn, error) {
	done := make(chan dialResult, 1)

	go func() {
		conn, err := dialer.Dial(network, addr)
		if ctx.Err() != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}

		select {
		case done <- dialResult{conn: conn, err: err}:
		default:
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		// 경합(race)으로 결과가 이미 도착했을 수 있으므로 드레인 후 정리
		select {
		case result := <-done:
			if result.conn != nil {
				_ = result.conn.Close()
			}
		default:
		}
		return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
	case result := <-done:
		if result.err != nil {
			return nil, fmt.Errorf("proxy dial failed: %w", result.err)
		}
		if ctx.Err() != nil {
			if result.conn != nil {
				_ = result.conn.Close()
			}
			return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
		}
		return result.conn, nil
	}
}

// fetchPage: YouTube 페이지 HTML 가져오기 (5xx 에러 시 재시도 포함)
func (c *Client) fetchPage(ctx context.Context, pageURL string) (string, error) {
	// transient cooldown 대기 (호출 간 감속, 내부 재시도와 독립)
	if wait := c.backoffState.TransientCooldownRemaining(); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("transient cooldown wait canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	var result string

	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   2 * time.Second,
		Jitter:      1500 * time.Millisecond,
		ShouldRetry: func(err error) bool {
			if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrForbidden) {
				return false
			}
			var statusErr *httpStatusError
			if errors.As(err, &statusErr) {
				return isRetryable5xx(statusErr.code)
			}
			return isRetryableTransportError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			if isRetryableTransportError(err) {
				c.closeIdleConnections()
			}
			slog.Debug("Scraper retry",
				"url", pageURL,
				"attempt", attempt,
				"delay", delay.Round(time.Millisecond),
				"error", err)
		},
	}, func(ctx context.Context) error {
		var err error
		result, err = c.fetchPageOnce(ctx, pageURL)
		return err
	})

	if err != nil {
		// context 취소/타임아웃 시 transient 에러 기록 스킵 (셧다운 시 불필요한 cooldown 방지)
		// retry 모두 소진된 경우에만 transient 에러 기록 (내부 retry 교차 오염 방지)
		if statusCode, ok := extractHTTPStatusCode(err); ctx.Err() == nil && ok && isRetryable5xx(statusCode) {
			c.backoffState.RecordTransientError()
		}
		return "", fmt.Errorf("fetchPage failed after retries: %w", err)
	}
	return result, nil
}

// fetchPageOnce: 단일 HTTP 요청 수행 (재시도 없음)
func (c *Client) fetchPageOnce(ctx context.Context, pageURL string) (string, error) {
	// 불변식: hard cooldown만 차단 (transient는 재시도 허용)
	if cooldownRemaining := c.backoffState.HardCooldownRemaining(); cooldownRemaining > 0 {
		return "", fmt.Errorf("in cooldown for %v: %w", cooldownRemaining.Round(time.Second), ErrRateLimited)
	}

	if err := c.rateLimiter.WaitWithBucket(ctx, distributedBucketFromURL(pageURL)); err != nil {
		return "", fmt.Errorf("rate limiter wait failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 헤더 스냅샷 기반 설정
	snap := c.uaProvider.Headers(ctx)
	req.Header.Set("User-Agent", snap.UserAgent)
	if snap.SecChUA != "" {
		req.Header.Set("Sec-CH-UA", snap.SecChUA)
		req.Header.Set("Sec-CH-UA-Mobile", "?0")
		req.Header.Set("Sec-CH-UA-Platform", snap.SecChUAPlatform)
	}

	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Accept", snap.Accept)
	req.Header.Set("Cookie", "SOCS=CAI")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")

	httpClient := c.currentHTTPClient()
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch page: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		c.backoffState.RecordError()
		cooldown := c.backoffState.HardCooldownRemaining()
		slog.Warn("YouTube rate limit hit, entering cooldown",
			"url", pageURL,
			"cooldown", cooldown.Round(time.Second))
		return "", fmt.Errorf("status %d: %w", resp.StatusCode, ErrRateLimited)

	case http.StatusForbidden:
		c.backoffState.RecordError()
		slog.Warn("YouTube access forbidden", "url", pageURL)
		return "", fmt.Errorf("status %d: %w", resp.StatusCode, ErrForbidden)

	case http.StatusOK:
		// body 읽기 성공 후에 RecordSuccess 호출

	default:
		return "", &httpStatusError{code: resp.StatusCode}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.YouTubeConfig.MaxPageBodyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	c.backoffState.RecordSuccess()
	return string(body), nil
}

// RateLimiter: 간격 기반 레이트 리미터 (slot 예약 패턴, 취소 시 rollback)
type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	lastTime time.Time
	seq      uint64

	distributed       distributedLimiter
	distributedLimit  int
	distributedWindow time.Duration
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{interval: interval}
}

// ConfigureDistributed: 분산 레이트 리미터를 설정합니다.
func (r *RateLimiter) ConfigureDistributed(limiter distributedLimiter, limit int, window time.Duration) error {
	if limiter == nil {
		return fmt.Errorf("configure distributed limiter: limiter must not be nil")
	}
	if limit <= 0 {
		return fmt.Errorf("configure distributed limiter: limit must be greater than zero")
	}
	if window <= 0 {
		return fmt.Errorf("configure distributed limiter: window must be greater than zero")
	}
	r.distributed = limiter
	r.distributedLimit = limit
	r.distributedWindow = window
	return nil
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	return r.WaitWithBucket(ctx, "default")
}

// WaitWithBucket: 로컬 + 분산 레이트 리미터를 함께 적용합니다.
func (r *RateLimiter) WaitWithBucket(ctx context.Context, bucket string) error {
	if bucket == "" {
		bucket = "default"
	}
	if err := r.waitLocal(ctx); err != nil {
		return err
	}
	return r.waitDistributed(ctx, bucket)
}

func (r *RateLimiter) waitLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	r.mu.Lock()
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	now := time.Now()
	if r.lastTime.IsZero() {
		r.lastTime = now
		r.seq++
		r.mu.Unlock()
		return nil
	}
	nextAllowed := r.lastTime.Add(r.interval)
	if now.After(nextAllowed) || now.Equal(nextAllowed) {
		r.lastTime = now
		r.seq++
		r.mu.Unlock()
		return nil
	}
	prevLastTime := r.lastTime // rollback용 저장
	r.lastTime = nextAllowed   // slot 예약
	r.seq++
	reservedSeq := r.seq
	waitTime := nextAllowed.Sub(now)
	r.mu.Unlock()

	timer := time.NewTimer(waitTime)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		// slot rollback: 취소 시 예약을 되돌림
		r.mu.Lock()
		if r.seq == reservedSeq {
			r.lastTime = prevLastTime
			r.seq++
		}
		r.mu.Unlock()
		return fmt.Errorf("rate limiter wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *RateLimiter) waitDistributed(ctx context.Context, bucket string) error {
	if r.distributed == nil {
		return nil
	}

	for {
		decision, err := r.distributed.Allow(ctx, bucket, r.distributedLimit, r.distributedWindow)
		if err != nil {
			return fmt.Errorf("distributed rate limiter allow failed: %w", err)
		}
		if decision.Allowed {
			return nil
		}
		if decision.RetryAfter <= 0 {
			return fmt.Errorf("distributed rate limiter denied without retry_after")
		}
		if !ctxutil.SleepWithContext(ctx, decision.RetryAfter) {
			return fmt.Errorf("distributed rate limiter wait canceled: %w", ctx.Err())
		}
	}
}

func distributedBucketFromURL(pageURL string) string {
	base := constants.YouTubeScraperDistributedRateLimitConfig.BucketBase
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return base + ":unknown"
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		path = "root"
	}
	path = strings.ReplaceAll(path, "/", ":")
	return base + ":" + path
}

// BackoffState: 듀얼 상태 지수 백오프 관리 (hard: 429/403, transient: 5xx)
type BackoffState struct {
	mu sync.Mutex

	// hard: 429/403 전용 (장기 쿨다운)
	hardErrors   int
	hardCooldown time.Time

	// transient: 5xx 전용 (경량 쿨다운)
	transientErrors   int
	transientCooldown time.Time
}

func NewBackoffState() *BackoffState {
	return &BackoffState{}
}

// RecordError: hard 에러 기록 (429/403 전용, 장기 쿨다운)
func (b *BackoffState) RecordError() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.hardErrors++

	var cooldown time.Duration
	switch {
	case b.hardErrors >= 5:
		cooldown = 6 * time.Hour
	case b.hardErrors >= 4:
		cooldown = 4 * time.Hour
	case b.hardErrors >= 3:
		cooldown = 2 * time.Hour
	case b.hardErrors >= 2:
		cooldown = 1 * time.Hour
	default:
		cooldown = 30 * time.Minute
	}

	b.hardCooldown = time.Now().Add(cooldown)
	slog.Warn("Hard backoff activated",
		"consecutive_errors", b.hardErrors,
		"cooldown", cooldown)
}

// RecordTransientError: transient 에러 기록 (5xx 전용, 경량 쿨다운)
func (b *BackoffState) RecordTransientError() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.transientErrors++

	var cooldown time.Duration
	switch {
	case b.transientErrors >= 3:
		cooldown = 10 * time.Minute
	case b.transientErrors >= 2:
		cooldown = 3 * time.Minute
	default:
		cooldown = 30 * time.Second
	}

	b.transientCooldown = time.Now().Add(cooldown)
	slog.Warn("Transient backoff activated",
		"consecutive_transient_errors", b.transientErrors,
		"cooldown", cooldown)
}

// RecordSuccess: 양쪽 상태 모두 리셋
func (b *BackoffState) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hardErrors > 0 || b.transientErrors > 0 {
		slog.Info("Backoff reset after success",
			"previous_hard_errors", b.hardErrors,
			"previous_transient_errors", b.transientErrors)
	}

	b.hardErrors = 0
	b.hardCooldown = time.Time{}
	b.transientErrors = 0
	b.transientCooldown = time.Time{}
}

// HardCooldownRemaining: hard 쿨다운 잔여 시간 (fetchPageOnce 전용)
func (b *BackoffState) HardCooldownRemaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hardCooldown.IsZero() {
		return 0
	}

	remaining := time.Until(b.hardCooldown)
	if remaining <= 0 {
		b.hardCooldown = time.Time{}
		b.hardErrors = 0
		return 0
	}

	return remaining
}

// TransientCooldownRemaining: transient 쿨다운 잔여 시간 (fetchPage 진입 전용)
func (b *BackoffState) TransientCooldownRemaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.transientCooldown.IsZero() {
		return 0
	}

	remaining := time.Until(b.transientCooldown)
	if remaining <= 0 {
		b.transientCooldown = time.Time{}
		b.transientErrors = 0
		return 0
	}

	return remaining
}

// CooldownRemaining: max(hard, transient) 쿨다운 반환
func (b *BackoffState) CooldownRemaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	var hard, transient time.Duration

	if !b.hardCooldown.IsZero() {
		hard = time.Until(b.hardCooldown)
		if hard <= 0 {
			b.hardCooldown = time.Time{}
			b.hardErrors = 0
			hard = 0
		}
	}

	if !b.transientCooldown.IsZero() {
		transient = time.Until(b.transientCooldown)
		if transient <= 0 {
			b.transientCooldown = time.Time{}
			b.transientErrors = 0
			transient = 0
		}
	}

	if hard > transient {
		return hard
	}
	return transient
}

// IsInCooldown: 쿨다운 중인지 확인
func (b *BackoffState) IsInCooldown() bool {
	return b.CooldownRemaining() > 0
}
