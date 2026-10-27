// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package system

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type ServiceGoroutines struct {
	Name       string `json:"name"`
	Goroutines int    `json:"goroutines"`
	Available  bool   `json:"available"`
}

type SystemStats struct {
	CPUUsage          float64             `json:"cpuUsage"`          // CPU 사용률 (%)
	MemoryUsage       float64             `json:"memoryUsage"`       // 메모리 사용률 (%)
	MemoryTotal       uint64              `json:"memoryTotal"`       // 전체 메모리 (Bytes)
	MemoryUsed        uint64              `json:"memoryUsed"`        // 사용 중인 메모리 (Bytes)
	Goroutines        int                 `json:"goroutines"`        // 현재 프로세스 Go 루틴 개수
	TotalGoroutines   int                 `json:"totalGoroutines"`   // 전체 서비스 Go 루틴 합계
	ServiceGoroutines []ServiceGoroutines `json:"serviceGoroutines"` // 서비스별 Go 루틴 통계
}

type ServiceEndpoint struct {
	Name string
	URL  string
}

const (
	defaultLocalServiceName = "hololive-admin-api"
	currentStatsFlightKey   = "current"
)

type Collector struct {
	httpClient  *http.Client
	h3Client    *http.Client
	h3ClientErr error
	endpoints   []ServiceEndpoint
	cacheTTL    time.Duration
	serviceName string
	cacheMu     sync.RWMutex
	cachedAt    time.Time
	cached      *SystemStats
	flights     singleflight.Group
}

type CollectorOption func(*Collector)

func WithServiceName(name string) CollectorOption {
	return func(c *Collector) {
		name = strings.TrimSpace(name)
		if name != "" {
			c.serviceName = name
		}
	}
}

func NewCollector(endpoints []ServiceEndpoint, options sharedh3.ClientOptions, opts ...CollectorOption) *Collector {
	h3Client, h3ClientErr := internalhttp.NewClientForURLStrict("https://internal", 2*time.Second, options)
	collector := &Collector{
		httpClient:  httputil.NewInternalServiceClient(2 * time.Second),
		h3Client:    h3Client,
		h3ClientErr: h3ClientErr,
		endpoints:   endpoints,
		cacheTTL:    2 * time.Second,
		serviceName: defaultLocalServiceName,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(collector)
		}
	}

	return collector
}

// Close는 health 조회용 H3 transport의 연결을 닫는다. 관리 plane이 요청을 모두 끝낸 뒤 부른다.
func (c *Collector) Close() error {
	if c == nil {
		return nil
	}

	if err := internalhttp.CloseClient(c.h3Client); err != nil {
		return fmt.Errorf("close health H3 client: %w", err)
	}

	return nil
}

// GetCurrentStats는 캐시가 유효하면 복사본을 돌려주고, 아니면 동시 요청을 하나의 수집으로 합친다. 수집은 요청 취소와
// 분리되어 끝까지 진행하고 성공 결과만 캐시에 남기므로, 먼저 이탈한 요청이 있어도 뒤 요청은 같은 결과를 받는다. 원격
// 조회 상한은 health client의 요청 timeout이 정한다. 대기자는 실패를 공유하지만 실패는 캐시하지 않으므로 다음 호출이
// 새로 수집한다.
func (c *Collector) GetCurrentStats(ctx context.Context) (*SystemStats, error) {
	if stats := c.getCachedStats(); stats != nil {
		return cloneSystemStats(stats), nil
	}

	resultCh := c.flights.DoChan(currentStatsFlightKey, func() (any, error) {
		return c.refreshCurrentStats(context.WithoutCancel(ctx))
	})

	var result singleflight.Result

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for current stats refresh: %w", ctx.Err())
	case result = <-resultCh:
	}

	// 수집 완료와 취소가 겹치면 이 요청은 취소로 끝낸다. 수집 결과는 이미 캐시에 남았다.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for current stats refresh: %w", err)
	}

	if result.Err != nil {
		return nil, result.Err
	}

	stats, ok := result.Val.(*SystemStats)
	if !ok {
		return nil, fmt.Errorf("current stats refresh returned %T", result.Val)
	}

	return cloneSystemStats(stats), nil
}

func (c *Collector) refreshCurrentStats(ctx context.Context) (*SystemStats, error) {
	// 선행 cache miss 뒤 다른 flight가 끝났을 수 있으므로 수집 소유권을 얻은 뒤 다시 확인한다.
	if stats := c.getCachedStats(); stats != nil {
		return stats, nil
	}

	var (
		stats      *SystemStats
		collectErr error
	)

	// DoChan goroutine의 panic은 호출자의 복구 경계 밖이므로 공유 수집 안에서 오류로 바꾼다.
	if err := panicguard.RunE(nil, panicscope.BackgroundTask, "system-stats-refresh", func() error {
		stats, collectErr = c.collectCurrentStats(ctx)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("collect current stats: %w", err)
	}

	if collectErr != nil {
		return nil, fmt.Errorf("collect current stats: %w", collectErr)
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cached = cloneSystemStats(stats)
	c.cachedAt = time.Now()

	return stats, nil
}

func (c *Collector) collectCurrentStats(ctx context.Context) (*SystemStats, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get memory stats: %w", err)
	}

	// CPU 사용률 (즉시 반환)
	cpus, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return nil, fmt.Errorf("get cpu stats: %w", err)
	}

	var cpuUsage float64

	if len(cpus) > 0 {
		cpuUsage = cpus[0]
	}

	localGoroutines := runtime.NumGoroutine()

	// 외부 서비스 goroutine 수집
	serviceStats := c.fetchServiceGoroutines(ctx)

	// 합계 계산
	totalGoroutines := localGoroutines

	for _, service := range serviceStats {
		if service.Available {
			totalGoroutines += service.Goroutines
		}
	}

	// 현재 서비스도 목록에 추가
	allServices := append([]ServiceGoroutines{{
		Name:       c.serviceName,
		Goroutines: localGoroutines,
		Available:  true,
	}}, serviceStats...)

	return &SystemStats{
		CPUUsage:          cpuUsage,
		MemoryUsage:       v.UsedPercent,
		MemoryTotal:       v.Total,
		MemoryUsed:        v.Used,
		Goroutines:        localGoroutines,
		TotalGoroutines:   totalGoroutines,
		ServiceGoroutines: allServices,
	}, nil
}

// getCachedStats는 게시 후 불변인 snapshot을 읽는다. 호출자에게 줄 복사본은 GetCurrentStats에서 만든다.
func (c *Collector) getCachedStats() *SystemStats {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	if c.cached == nil {
		return nil
	}

	if time.Since(c.cachedAt) > c.cacheTTL {
		return nil
	}

	return c.cached
}

func cloneSystemStats(src *SystemStats) *SystemStats {
	if src == nil {
		return nil
	}

	cloned := *src

	cloned.ServiceGoroutines = append([]ServiceGoroutines(nil), src.ServiceGoroutines...)

	return &cloned
}

// fetchServiceGoroutines: 외부 서비스들의 goroutine 수를 병렬로 조회합니다.
func (c *Collector) fetchServiceGoroutines(ctx context.Context) []ServiceGoroutines {
	if len(c.endpoints) == 0 {
		return nil
	}

	results := make([]ServiceGoroutines, len(c.endpoints))

	var wg sync.WaitGroup

	for i, ep := range c.endpoints {
		if ep.URL == "" {
			results[i] = ServiceGoroutines{Name: ep.Name, Available: false}
			continue
		}

		wg.Go(func() {
			if err := panicguard.RunE(nil, panicscope.BackgroundTask, "system-stats-service-goroutines", func() error {
				goroutines, ok := c.fetchGoroutineCount(ctx, ep.URL)

				results[i] = ServiceGoroutines{
					Name:       ep.Name,
					Goroutines: goroutines,
					Available:  ok,
				}

				return nil
			}); err != nil {
				results[i] = ServiceGoroutines{Name: ep.Name, Available: false}
			}
		})
	}

	wg.Wait()

	return results
}

// healthResponse: /health 엔드포인트 응답 파싱용.
type healthResponse struct {
	Goroutines int `json:"goroutines"`
	Components map[string]struct {
		Detail map[string]any `json:"detail"`
	} `json:"components"`
}

// fetchGoroutineCount: 단일 서비스의 goroutine 수를 조회합니다.
func (c *Collector) fetchGoroutineCount(ctx context.Context, url string) (int, bool) {
	if c == nil {
		return 0, false
	}

	resp, ok := c.fetchHealthResponse(ctx, url)
	if !ok {
		return 0, false
	}

	hr, ok := decodeHealthResponse(resp)
	if !ok {
		return 0, false
	}

	return goroutineCountFromHealthResponse(hr)
}

func (c *Collector) fetchHealthResponse(ctx context.Context, url string) (*http.Response, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, false
	}

	client, ok := c.clientForURL(url)
	if !ok {
		return nil, false
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}

	if resp == nil {
		return nil, false
	}

	if resp.Body == nil {
		return nil, false
	}

	return resp, true
}

func decodeHealthResponse(resp *http.Response) (healthResponse, bool) {
	if err := httputil.CheckStatus(resp); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return healthResponse{}, false
		}

		return healthResponse{}, false
	}

	hr, err := httputil.DecodeJSON[healthResponse](resp)
	if err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return healthResponse{}, false
		}

		return healthResponse{}, false
	}

	if closeErr := resp.Body.Close(); closeErr != nil {
		return healthResponse{}, false
	}

	return hr, true
}

func (c *Collector) clientForURL(rawURL string) (*http.Client, bool) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "https://") {
		if c.h3ClientErr != nil || c.h3Client == nil {
			return nil, false
		}

		return c.h3Client, true
	}

	return c.httpClient, c.httpClient != nil
}

func goroutineCountFromHealthResponse(hr healthResponse) (int, bool) {
	if hr.Goroutines > 0 {
		return hr.Goroutines, true
	}

	app, ok := hr.Components["app"]
	if !ok {
		return 0, false
	}

	return goroutineCountFromDetail(app.Detail)
}

func goroutineCountFromDetail(detail map[string]any) (int, bool) {
	gr, ok := detail["goroutines"]
	if !ok {
		return 0, false
	}

	return goroutineCountValue(gr)
}

func goroutineCountValue(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}

	return 0, false
}
