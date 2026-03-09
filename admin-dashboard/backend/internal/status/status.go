// Package status: 통합 시스템 상태 수집 (멀티 서비스)
package status

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ServiceStatus: 개별 서비스 상태
type ServiceStatus struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	Version    string `json:"version,omitempty"`
	Uptime     string `json:"uptime,omitempty"`
	Goroutines int    `json:"goroutines"`
}

// AggregatedStatus: 통합 시스템 상태 응답
type AggregatedStatus struct {
	// Admin Dashboard 자체 상태
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	StartedAt int64  `json:"startedAt"`

	// 서비스별 상태
	Services []ServiceStatus `json:"services"`

	// 집계 통계
	TotalGoroutines   int `json:"totalGoroutines"`
	AvailableServices int `json:"availableServices"`
	TotalServices     int `json:"totalServices"`
	AdminGoroutines   int `json:"adminGoroutines"`
}

// ServiceEndpoint: 봇 서비스 엔드포인트 정보
type ServiceEndpoint struct {
	Name      string // 서비스 이름 (hololive-bot 등)
	HealthURL string // /health 엔드포인트 URL
	StatsURL  string // /api/holo/stats 등 상세 상태 URL (선택 사항)
}

// Collector: 멀티 서비스 상태 수집기
type Collector struct {
	httpClient *http.Client
	endpoints  []ServiceEndpoint
	logger     *slog.Logger
	startTime  time.Time
	version    string
}

// NewCollector: 상태 수집기 생성
func NewCollector(endpoints []ServiceEndpoint, version string, logger *slog.Logger) *Collector {
	return &Collector{
		httpClient: &http.Client{
			Timeout:   3 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		endpoints: endpoints,
		logger:    logger,
		startTime: time.Now(),
		version:   version,
	}
}

// GetAggregatedStatus: 모든 서비스의 통합 상태 수집
func (c *Collector) GetAggregatedStatus(ctx context.Context) *AggregatedStatus {
	now := time.Now()
	uptime := now.Sub(c.startTime)
	adminGoroutines := runtime.NumGoroutine()

	// 서비스 상태 병렬 수집
	services := c.fetchAllServiceStatus(ctx)

	// 집계 계산
	totalGoroutines := adminGoroutines
	availableCount := 0
	for _, svc := range services {
		if svc.Available {
			availableCount++
			totalGoroutines += svc.Goroutines
		}
	}

	// Admin Dashboard 자체를 services 목록에 추가
	adminStatus := ServiceStatus{
		Name:       "admin-dashboard",
		Available:  true,
		Version:    c.version,
		Uptime:     formatDuration(uptime),
		Goroutines: adminGoroutines,
	}
	allServices := append([]ServiceStatus{adminStatus}, services...)

	return &AggregatedStatus{
		Version:           c.version,
		Uptime:            formatDuration(uptime),
		StartedAt:         c.startTime.Unix(),
		Services:          allServices,
		TotalGoroutines:   totalGoroutines,
		AvailableServices: availableCount + 1, // +1 for admin-dashboard
		TotalServices:     len(allServices),
		AdminGoroutines:   adminGoroutines,
	}
}

// fetchAllServiceStatus: 모든 서비스 상태 병렬 수집
func (c *Collector) fetchAllServiceStatus(ctx context.Context) []ServiceStatus {
	if len(c.endpoints) == 0 {
		return nil
	}

	results := make([]ServiceStatus, len(c.endpoints))
	var wg sync.WaitGroup

	for i, ep := range c.endpoints {
		if ep.HealthURL == "" {
			results[i] = ServiceStatus{Name: ep.Name, Available: false}
			continue
		}

		wg.Add(1)
		go func(idx int, endpoint ServiceEndpoint) {
			defer wg.Done()
			results[idx] = c.fetchServiceStatus(ctx, endpoint)
		}(i, ep)
	}

	wg.Wait()
	return results
}

// healthResponse: /health 엔드포인트 응답 파싱용
type healthResponse struct {
	Status     string `json:"status"`
	Version    string `json:"version"`
	Uptime     string `json:"uptime"`
	Goroutines int    `json:"goroutines"`
	Components map[string]struct {
		Detail map[string]any `json:"detail"`
	} `json:"components"`
}

// statsResponse: /api/holo/stats 등 상세 상태 파싱용 (holo-bot 전용)
type statsResponse struct {
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

// fetchServiceStatus: 단일 서비스 상태 조회
func (c *Collector) fetchServiceStatus(ctx context.Context, endpoint ServiceEndpoint) ServiceStatus {
	status := ServiceStatus{
		Name:      endpoint.Name,
		Available: false,
	}

	// Health 체크
	healthResp, ok := c.fetchHealthResponse(ctx, endpoint.HealthURL)
	if !ok {
		return status
	}

	status.Available = true

	// version/uptime 직접 파싱 (game-bot, llm-server 새 형식)
	if healthResp.Version != "" {
		status.Version = healthResp.Version
	}
	if healthResp.Uptime != "" {
		status.Uptime = healthResp.Uptime
	}

	// Goroutines 파싱 (여러 형식 지원)
	if healthResp.Goroutines > 0 {
		status.Goroutines = healthResp.Goroutines
	} else if app, exists := healthResp.Components["app"]; exists {
		if gr, ok := app.Detail["goroutines"]; ok {
			switch v := gr.(type) {
			case float64:
				status.Goroutines = int(v)
			case int:
				status.Goroutines = v
			}
		}
		// LLM Server 구형 형식에서 version/uptime fallback
		if status.Version == "" {
			if v, ok := app.Detail["version"].(string); ok {
				status.Version = v
			}
		}
		if status.Uptime == "" {
			if u, ok := app.Detail["uptime"].(string); ok {
				status.Uptime = u
			}
		}
	}

	// 상세 통계 조회 (holo-bot 전용 - /api/holo/stats)
	if endpoint.StatsURL != "" && (status.Version == "" || status.Uptime == "") {
		if statsResp, ok := c.fetchStatsResponse(ctx, endpoint.StatsURL); ok {
			if status.Version == "" {
				status.Version = statsResp.Version
			}
			if status.Uptime == "" {
				status.Uptime = statsResp.Uptime
			}
		}
	}

	return status
}

// fetchHealthResponse: Health 응답 조회
func (c *Collector) fetchHealthResponse(ctx context.Context, url string) (healthResponse, bool) {
	var result healthResponse

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return result, false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return result, false
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, false
	}

	return result, true
}

// fetchStatsResponse: Stats 응답 조회
func (c *Collector) fetchStatsResponse(ctx context.Context, url string) (statsResponse, bool) {
	var result statsResponse

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return result, false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return result, false
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, false
	}

	return result, true
}

// formatDuration: time.Duration을 사람이 읽기 쉬운 형식으로 변환
func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
