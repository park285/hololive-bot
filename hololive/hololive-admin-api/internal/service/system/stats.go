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

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
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

const defaultLocalServiceName = "hololive-admin-api"

type Collector struct {
	httpClient  *http.Client
	endpoints   []ServiceEndpoint
	cacheTTL    time.Duration
	serviceName string
	cacheMu     sync.RWMutex
	refreshMu   sync.Mutex
	cachedAt    time.Time
	cached      *SystemStats
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

func NewCollector(endpoints []ServiceEndpoint, opts ...CollectorOption) *Collector {
	collector := &Collector{
		httpClient:  httputil.NewInternalServiceClient(2 * time.Second),
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

func (c *Collector) GetCurrentStats(ctx context.Context) (*SystemStats, error) {
	if stats := c.getCachedStats(); stats != nil {
		return stats, nil
	}

	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	if stats := c.getCachedStats(); stats != nil {
		return stats, nil
	}

	stats, err := c.collectCurrentStats(ctx)
	if err != nil {
		return nil, err
	}

	c.cacheMu.Lock()
	c.cached = cloneSystemStats(stats)
	c.cachedAt = time.Now()
	c.cacheMu.Unlock()

	return stats, nil
}

func (c *Collector) collectCurrentStats(ctx context.Context) (*SystemStats, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}

	// CPU 사용률 (즉시 반환)
	cpus, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get cpu stats: %w", err)
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

	for _, svc := range serviceStats {
		if svc.Available {
			totalGoroutines += svc.Goroutines
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

func (c *Collector) getCachedStats() *SystemStats {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	if c.cached == nil {
		return nil
	}

	if time.Since(c.cachedAt) > c.cacheTTL {
		return nil
	}

	return cloneSystemStats(c.cached)
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

		wg.Add(1)

		go func(idx int, endpoint ServiceEndpoint) {
			defer wg.Done()

			goroutines, ok := c.fetchGoroutineCount(ctx, endpoint.URL)

			results[idx] = ServiceGoroutines{
				Name:       endpoint.Name,
				Goroutines: goroutines,
				Available:  ok,
			}
		}(i, ep)
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
	if c == nil || c.httpClient == nil {
		return 0, false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	if err := httputil.CheckStatus(resp); err != nil {
		return 0, false
	}

	var hr healthResponse
	if err := httputil.DecodeJSON(resp, &hr); err != nil {
		return 0, false
	}

	// 직접 goroutines 필드가 있는 경우 (game-bot 형식)
	if hr.Goroutines > 0 {
		return hr.Goroutines, true
	}

	// components.app.detail.goroutines 형식 (mcp-llm-server 형식)
	if app, ok := hr.Components["app"]; ok {
		if gr, ok := app.Detail["goroutines"]; ok {
			switch v := gr.(type) {
			case float64:
				return int(v), true
			case int:
				return v, true
			}
		}
	}

	return 0, false
}
