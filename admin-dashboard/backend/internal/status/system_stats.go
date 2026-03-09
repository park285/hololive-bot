package status

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// SystemStats: 시스템 리소스 통계 (WebSocket 스트리밍용)
type SystemStats struct {
	CPUUsage          float64             `json:"cpuUsage"`          // CPU 사용률 (%)
	MemoryUsage       float64             `json:"memoryUsage"`       // 메모리 사용률 (%)
	MemoryTotal       uint64              `json:"memoryTotal"`       // 전체 메모리 (Bytes)
	MemoryUsed        uint64              `json:"memoryUsed"`        // 사용 중인 메모리 (Bytes)
	Goroutines        int                 `json:"goroutines"`        // 현재 프로세스 Go 루틴 개수
	TotalGoroutines   int                 `json:"totalGoroutines"`   // 전체 서비스 Go 루틴 합계
	ServiceGoroutines []ServiceGoroutines `json:"serviceGoroutines"` // 서비스별 Go 루틴 통계
}

// ServiceGoroutines: 개별 서비스의 goroutine 통계
type ServiceGoroutines struct {
	Name       string `json:"name"`
	Goroutines int    `json:"goroutines"`
	Available  bool   `json:"available"`
}

// GetSystemStats: 현재 시스템 리소스 상태를 반환합니다.
func (c *Collector) GetSystemStats(ctx context.Context) (*SystemStats, error) {
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

	adminGoroutines := runtime.NumGoroutine()

	// 서비스 상태 병렬 수집
	services := c.fetchAllServiceStatus(ctx)

	// ServiceGoroutines 변환 및 합계 계산
	serviceGoroutines := make([]ServiceGoroutines, 0, len(services)+1)

	// admin-dashboard 자체 추가
	serviceGoroutines = append(serviceGoroutines, ServiceGoroutines{
		Name:       "admin-dashboard",
		Goroutines: adminGoroutines,
		Available:  true,
	})

	totalGoroutines := adminGoroutines
	for _, svc := range services {
		serviceGoroutines = append(serviceGoroutines, ServiceGoroutines{
			Name:       svc.Name,
			Goroutines: svc.Goroutines,
			Available:  svc.Available,
		})
		if svc.Available {
			totalGoroutines += svc.Goroutines
		}
	}

	return &SystemStats{
		CPUUsage:          cpuUsage,
		MemoryUsage:       v.UsedPercent,
		MemoryTotal:       v.Total,
		MemoryUsed:        v.Used,
		Goroutines:        adminGoroutines,
		TotalGoroutines:   totalGoroutines,
		ServiceGoroutines: serviceGoroutines,
	}, nil
}

// StreamSystemStats: 2초마다 시스템 통계를 채널로 전송합니다.
func (c *Collector) StreamSystemStats(ctx context.Context, out chan<- *SystemStats) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// 최초 1회 즉시 전송
	if stats, err := c.GetSystemStats(ctx); err == nil {
		select {
		case out <- stats:
		case <-ctx.Done():
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := c.GetSystemStats(ctx)
			if err != nil {
				continue
			}
			select {
			case out <- stats:
			case <-ctx.Done():
				return
			}
		}
	}
}
