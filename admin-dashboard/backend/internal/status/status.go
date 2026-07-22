package status

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type ServiceEndpoint struct {
	Name       string
	URL        string
	HealthPath string
}

type endpointClient struct {
	client *http.Client
	err    error
}

func (e endpointClient) resolve() (client *http.Client, errMsg string) {
	if e.err != nil {
		return nil, e.err.Error()
	}
	if e.client == nil {
		return nil, "status client missing"
	}
	return e.client, ""
}

// https endpoint는 H3-only 서비스이므로 URL scheme에 맞는 client를 endpoint별로 고정한다.
func endpointClients(endpoints []ServiceEndpoint, timeout time.Duration) map[string]endpointClient {
	clients := make(map[string]endpointClient, len(endpoints))
	for _, endpoint := range endpoints {
		client, err := internalhttp.NewClientForURLStrict(endpoint.URL, timeout, nil)
		clients[endpoint.Name] = endpointClient{client: client, err: err}
	}
	return clients
}

type ServiceStatus struct {
	Name           string  `json:"name"`
	Available      bool    `json:"available"`
	ResponseTimeMS *uint64 `json:"response_time_ms"`
	Error          *string `json:"error"`
}

type AggregatedStatus struct {
	Services []ServiceStatus `json:"services"`
	Uptime   string          `json:"uptime"`
	Version  string          `json:"version"`
}

type Collector struct {
	clients   map[string]endpointClient
	endpoints []ServiceEndpoint
	start     time.Time
	version   string
}

func NewCollector(endpoints []ServiceEndpoint, version string) *Collector {
	return &Collector{
		clients:   endpointClients(endpoints, 3*time.Second),
		endpoints: append([]ServiceEndpoint(nil), endpoints...),
		start:     time.Now(),
		version:   version,
	}
}

func (c *Collector) Collect(ctx context.Context) AggregatedStatus {
	services := make([]ServiceStatus, len(c.endpoints)+1)
	zero := uint64(0)
	services[0] = ServiceStatus{Name: "admin-dashboard", Available: true, ResponseTimeMS: &zero}
	var wg sync.WaitGroup
	for i := range c.endpoints {
		index := i + 1
		endpoint := c.endpoints[i]
		wg.Add(1)
		panicguard.Go(nil, "admin-dashboard-status-endpoint", func() {
			defer wg.Done()
			var status ServiceStatus
			if err := panicguard.RunE(nil, "admin-dashboard-status-endpoint", func() error {
				status = c.collectEndpoint(ctx, endpoint)
				return nil
			}); err != nil {
				errText := err.Error()
				status = ServiceStatus{Name: endpoint.Name, Available: false, Error: &errText}
			}
			services[index] = status
		})
	}
	wg.Wait()
	return AggregatedStatus{Services: services, Uptime: FormatDuration(time.Since(c.start)), Version: c.version}
}

func (c *Collector) collectEndpoint(ctx context.Context, endpoint ServiceEndpoint) ServiceStatus {
	result := doHealthGET(ctx, c.clients[endpoint.Name], endpoint)
	if result.errMsg != "" {
		status := ServiceStatus{Name: endpoint.Name, Available: false, Error: &result.errMsg}
		if result.measured {
			status.ResponseTimeMS = &result.latencyMS
		}
		return status
	}
	defer func() {
		if err := result.resp.Body.Close(); err != nil {
			return
		}
	}()
	return ServiceStatus{Name: endpoint.Name, Available: true, ResponseTimeMS: &result.latencyMS}
}

func FormatDuration(duration time.Duration) string {
	secs := int64(duration.Seconds())
	days := secs / 86400
	hours := (secs % 86400) / 3600
	minutes := (secs % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

type RuntimeMetricKind string

const RuntimeMetricGoroutine RuntimeMetricKind = "goroutine"

type ServiceRuntimeStats struct {
	Name       string            `json:"name"`
	Count      int               `json:"count"`
	MetricKind RuntimeMetricKind `json:"metricKind"`
	Available  bool              `json:"available"`
	Error      *string           `json:"error,omitempty"`
}

type SystemStats struct {
	CPUUsage          float64               `json:"cpuUsage"`
	MemoryTotal       uint64                `json:"memoryTotal"`
	MemoryUsed        uint64                `json:"memoryUsed"`
	MemoryUsage       float64               `json:"memoryUsage"`
	ThreadCount       int                   `json:"threadCount"`
	TotalGoGoroutines int                   `json:"totalGoGoroutines"`
	TotalRuntimeUnits int                   `json:"totalRuntimeUnits"`
	ServiceRuntime    []ServiceRuntimeStats `json:"serviceRuntime"`
	LoadAvg1          float64               `json:"loadAvg1"`
	LoadAvg5          float64               `json:"loadAvg5"`
	LoadAvg15         float64               `json:"loadAvg15"`
}

func memoryStats() (total, used uint64) {
	data, err := osReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	values := map[string]uint64{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	total = values["MemTotal"]
	available := values["MemAvailable"]
	if total > available {
		used = total - available
	}
	return total, used
}

func loadAverage() (one, five, fifteen float64) {
	data, err := osReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	one, err = strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0
	}
	five, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0
	}
	fifteen, err = strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0
	}
	return one, five, fifteen
}

func threadCount() int {
	data, err := osReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		value, ok := parseThreadLine(line)
		if ok {
			return value
		}
	}
	return 0
}

func parseThreadLine(line string) (int, bool) {
	if !strings.HasPrefix(line, "Threads:") {
		return 0, false
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return 0, true
	}
	value, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, true
	}
	return value, true
}

type procSampler struct {
	mu        sync.Mutex
	lastIdle  uint64
	lastTotal uint64
}

func (s *procSampler) cpuUsage() float64 {
	idle, total, ok := readCPUSample()
	if !ok {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastTotal == 0 {
		s.lastTotal = total
		s.lastIdle = idle
		return 0
	}
	totalDelta := total - s.lastTotal
	idleDelta := idle - s.lastIdle
	s.lastTotal = total
	s.lastIdle = idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100
}

func readCPUSample() (idle, total uint64, ok bool) {
	data, err := osReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) == 0 {
		return 0, 0, false
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	values, ok := parseCPUFields(fields[1:])
	if !ok {
		return 0, 0, false
	}
	return cpuTotals(values)
}

func parseCPUFields(fields []string) ([]uint64, bool) {
	values := make([]uint64, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

func cpuTotals(values []uint64) (idle, total uint64, ok bool) {
	if len(values) < 4 {
		return 0, 0, false
	}
	idle = values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	for _, value := range values {
		total += value
	}
	return idle, total, true
}
