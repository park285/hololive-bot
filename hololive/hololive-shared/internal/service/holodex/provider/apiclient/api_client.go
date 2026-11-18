package apiclient

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	ratelimitvalkey "github.com/kapu/hololive-shared/pkg/service/ratelimit/valkey"
)

type Requester interface {
	DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error)
	IsCircuitOpen() bool
}

type APIClient struct {
	maxRetryAttempts     int
	cooldownMu           sync.Mutex
	cooldownUntil        time.Time
	httpClient           *http.Client
	baseURL              string
	apiKey               string
	logger               *slog.Logger
	breaker              *circuitBreaker
	admissionGate        chan struct{}
	requestDelay         time.Duration
	nextRequestAt        time.Time
	semaphore            chan struct{}
	distributed          distributedRateLimiter
	shared               sharedState
	perAttemptTimeout    time.Duration
	maxResponseBodyBytes int64
	distributedRLCfg     settings.DistributedRateLimitConfig
}

type holodexRequestRetryState struct {
	maxAttempts       int
	maxTimeoutRetries int
	timeoutCount      int
	lastErr           error
}

type distributedRateLimiter interface {
	Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimitvalkey.Decision, error)
}

func NewHolodexAPIClient(
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	logger *slog.Logger,
	distributed distributedRateLimiter,
	shared sharedState,
	holodexCfg *settings.HolodexConfig,
) (*APIClient, error) {
	if holodexCfg == nil {
		return nil, stdErrors.New("holodex config is nil")
	}

	if err := settings.ValidateHolodexRequestConfig(holodexCfg); err != nil {
		return nil, fmt.Errorf("validate holodex request config: %w", err)
	}

	configureHolodexHTTP2(httpClient)

	if reflectutil.IsNil(shared) {
		shared = nil
	}

	maxBody := settings.DefaultMaxResponseBodyBytes

	return &APIClient{
		maxRetryAttempts: holodexCfg.MaxRetryAttempts,
		httpClient:       httpClient,
		baseURL:          baseURL,
		apiKey:           apiKey,
		logger:           logger,
		breaker: newCircuitBreaker(
			circuitFailureThreshold,
			circuitResetTimeout,
		),
		admissionGate:        make(chan struct{}, 1),
		requestDelay:         holodexCfg.Concurrency.RequestDelay,
		semaphore:            make(chan struct{}, holodexCfg.Concurrency.MaxConcurrentRequests),
		distributed:          distributed,
		shared:               shared,
		perAttemptTimeout:    holodexCfg.PerAttemptTimeout,
		maxResponseBodyBytes: maxBody,
		distributedRLCfg:     holodexCfg.DistributedRateLimit,
	}, nil
}

func configureHolodexHTTP2(client *http.Client) {
	transport, ok := client.Transport.(*http.Transport)

	if !ok {
		return
	}

	protocols := new(http.Protocols)

	protocols.SetHTTP2(true)

	transport.Protocols = protocols
}
