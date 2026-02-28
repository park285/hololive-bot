package twitch

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/errors"
)

// ClientConfig: Twitch 클라이언트 설정
type ClientConfig struct {
	ClientID     string
	ClientSecret string
}

// Client: Twitch Helix API 클라이언트
type Client struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
	logger       *slog.Logger

	// Token 관리 (atomic + mutex)
	token       atomic.Value // string
	tokenExpiry atomic.Value // time.Time
	tokenMu     sync.Mutex

	// Circuit Breaker
	circuitOpen     atomic.Bool
	circuitOpenedAt atomic.Value // time.Time
	failureCount    atomic.Int32
}

// NewClient: 새 Twitch 클라이언트를 생성합니다.
func NewClient(cfg ClientConfig, logger *slog.Logger) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: constants.TwitchConfig.Timeout,
		},
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		logger:       logger,
	}
	c.tokenExpiry.Store(time.Time{})
	c.circuitOpenedAt.Store(time.Time{})
	return c
}

// IsConfigured: 클라이언트가 올바르게 설정되었는지 확인
func (c *Client) IsConfigured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != ""
}

// GetStreams: 지정된 user_login들의 현재 라이브 스트림을 조회합니다.
// 최대 100개까지 한 번에 조회 가능
// user_login은 Twitch username (예: tokoyamitowa_holo)
func (c *Client) GetStreams(ctx context.Context, userLogins []string) (*StreamsResponse, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("twitch client not configured")
	}

	if len(userLogins) == 0 {
		return &StreamsResponse{Data: []StreamData{}}, nil
	}

	if c.isCircuitOpen() {
		return nil, &errors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: 503,
			Err:        fmt.Errorf("circuit breaker open"),
		}
	}

	// 토큰 확인 및 갱신
	if err := c.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("ensure token: %w", err)
	}

	// 요청 구성 (user_login 사용 - username으로 조회)
	params := url.Values{}
	for _, login := range userLogins {
		params.Add("user_login", login)
	}

	reqURL := constants.TwitchConfig.BaseURL + "/streams?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	token, _ := c.token.Load().(string)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Id", c.clientID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 401: 토큰 갱신 후 재시도
	if resp.StatusCode == http.StatusUnauthorized {
		c.invalidateToken()
		if refreshErr := c.refreshToken(ctx); refreshErr != nil {
			return nil, fmt.Errorf("refresh token after 401: %w", refreshErr)
		}
		// 재시도
		return c.GetStreams(ctx, userLogins)
	}

	// 429: Rate Limit
	if resp.StatusCode == http.StatusTooManyRequests {
		c.recordFailure()
		return nil, &errors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: 429,
			Err:        fmt.Errorf("rate limited"),
		}
	}

	// 5xx: 서버 에러
	if resp.StatusCode >= 500 {
		c.recordFailure()
		return nil, &errors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("server error"),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &errors.APIError{
			Operation:  "twitch_get_streams",
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("unexpected status"),
		}
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result StreamsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	c.recordSuccess()
	return &result, nil
}

// ensureValidToken: 유효한 토큰이 있는지 확인하고 필요시 갱신
func (c *Client) ensureValidToken(ctx context.Context) error {
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Now().Before(expiry.Add(-constants.TwitchConfig.TokenRefreshSkew)) {
		return nil
	}
	return c.refreshToken(ctx)
}

// refreshToken: App Access Token을 갱신합니다.
func (c *Client) refreshToken(ctx context.Context) error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double-check: 다른 고루틴이 이미 갱신했을 수 있음
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Now().Before(expiry.Add(-constants.TwitchConfig.TokenRefreshSkew)) {
		return nil
	}

	params := url.Values{}
	params.Set("client_id", c.clientID)
	params.Set("client_secret", c.clientSecret)
	params.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, constants.TwitchConfig.AuthURL, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed: status %d", resp.StatusCode)
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("unmarshal token response: %w", err)
	}

	c.token.Store(tokenResp.AccessToken)
	c.tokenExpiry.Store(time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second))

	c.logger.Info("Twitch token refreshed",
		slog.Int("expires_in_seconds", tokenResp.ExpiresIn))

	return nil
}

// invalidateToken: 토큰을 무효화합니다.
func (c *Client) invalidateToken() {
	c.tokenExpiry.Store(time.Time{})
}

// isCircuitOpen: Circuit Breaker 상태 확인 (내부용)
func (c *Client) isCircuitOpen() bool {
	if !c.circuitOpen.Load() {
		return false
	}
	openedAt, _ := c.circuitOpenedAt.Load().(time.Time)
	if time.Since(openedAt) > constants.CircuitBreakerConfig.ResetTimeout {
		c.circuitOpen.Store(false)
		c.failureCount.Store(0)
		c.logger.Info("Twitch circuit breaker reset")
		return false
	}
	return true
}

func (c *Client) recordFailure() {
	count := c.failureCount.Add(1)
	if count >= int32(constants.CircuitBreakerConfig.FailureThreshold) {
		c.circuitOpen.Store(true)
		c.circuitOpenedAt.Store(time.Now())
		c.logger.Warn("Twitch circuit breaker opened",
			slog.Int("failure_count", int(count)))
	}
}

func (c *Client) recordSuccess() {
	c.failureCount.Store(0)
}

// IsCircuitOpen: Circuit Breaker 상태 확인 (외부 노출용)
func (c *Client) IsCircuitOpen() bool {
	return c.isCircuitOpen()
}
