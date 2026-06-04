package status

import (
	"context"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/pkg/json"
)

const historyCap = 30

type Hub struct {
	endpoints []ServiceEndpoint
	http      *http.Client
	sampler   *procSampler
	mu        sync.Mutex
	nextID    int64
	subs      map[int64]chan SystemStats
	history   []SystemStats
	stop      chan struct{}
	done      chan struct{}
}

func NewHub(endpoints []ServiceEndpoint) *Hub {
	return &Hub{
		endpoints: append([]ServiceEndpoint(nil), endpoints...),
		http:      &http.Client{Timeout: 2 * time.Second},
		sampler:   &procSampler{},
		subs:      make(map[int64]chan SystemStats),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

func (h *Hub) Start() {
	go h.run()
}

func (h *Hub) run() {
	defer close(h.done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for h.tick(ticker.C) {
	}
}

func (h *Hub) tick(tick <-chan time.Time) bool {
	select {
	case <-h.stop:
		return false
	case <-tick:
		h.broadcastSample()
		return true
	}
}

func (h *Hub) broadcastSample() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	stats := h.collect(ctx)
	cancel()
	h.Publish(stats)
}

func (h *Hub) Stop() {
	select {
	case <-h.done:
		return
	default:
	}
	close(h.stop)
	<-h.done
}

func (h *Hub) Subscribe() ([]SystemStats, chan SystemStats, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	ch := make(chan SystemStats, 4)
	h.subs[id] = ch
	return slices.Clone(h.history), ch, func() {
		h.mu.Lock()
		delete(h.subs, id)
		close(ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(stats SystemStats) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, stats)
	if len(h.history) > historyCap {
		h.history = h.history[len(h.history)-historyCap:]
	}
	for _, ch := range h.subs {
		sendDropOldest(ch, stats)
	}
}

func sendDropOldest(ch chan SystemStats, stats SystemStats) {
	if trySend(ch, stats) {
		return
	}
	select {
	case <-ch:
	default:
	}
	trySend(ch, stats)
}

func trySend(ch chan SystemStats, stats SystemStats) bool {
	select {
	case ch <- stats:
		return true
	default:
		return false
	}
}

func (h *Hub) collect(ctx context.Context) SystemStats {
	memTotal, memUsed := memoryStats()
	load1, load5, load15 := loadAverage()
	threadCount := threadCount()
	adminGoroutines := runtime.NumGoroutine()
	serviceRuntime := []ServiceRuntimeStats{{Name: "admin-dashboard", Count: adminGoroutines, MetricKind: RuntimeMetricGoroutine, Available: true}}
	serviceRuntime = append(serviceRuntime, h.externalRuntimeStats(ctx)...)
	totalGo := 0
	for _, service := range serviceRuntime {
		if service.Available && service.MetricKind == RuntimeMetricGoroutine {
			totalGo += service.Count
		}
	}
	memoryUsage := 0.0
	if memTotal > 0 {
		memoryUsage = float64(memUsed) / float64(memTotal) * 100
	}
	return SystemStats{
		CPUUsage:          h.sampler.cpuUsage(),
		MemoryTotal:       memTotal,
		MemoryUsed:        memUsed,
		MemoryUsage:       memoryUsage,
		ThreadCount:       threadCount,
		TotalGoGoroutines: totalGo,
		TotalRuntimeUnits: totalGo,
		ServiceRuntime:    serviceRuntime,
		LoadAvg1:          load1,
		LoadAvg5:          load5,
		LoadAvg15:         load15,
	}
}

func (h *Hub) externalRuntimeStats(ctx context.Context) []ServiceRuntimeStats {
	results := make([]ServiceRuntimeStats, 0, len(h.endpoints))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, endpoint := range h.endpoints {
		wg.Go(func() {
			stat := h.fetchRuntime(ctx, endpoint)
			mu.Lock()
			results = append(results, stat)
			mu.Unlock()
		})
	}
	wg.Wait()
	return results
}

func (h *Hub) fetchRuntime(ctx context.Context, endpoint ServiceEndpoint) ServiceRuntimeStats {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint.URL, "/")+endpoint.HealthPath, nil)
	resp, err := h.http.Do(req)
	if err != nil {
		msg := err.Error()
		return ServiceRuntimeStats{Name: endpoint.Name, MetricKind: RuntimeMetricGoroutine, Available: false, Error: &msg}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := "status: " + resp.Status
		return ServiceRuntimeStats{Name: endpoint.Name, MetricKind: RuntimeMetricGoroutine, Available: false, Error: &msg}
	}
	var payload healthPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		msg := "invalid health payload: " + err.Error()
		return ServiceRuntimeStats{Name: endpoint.Name, MetricKind: RuntimeMetricGoroutine, Available: false, Error: &msg}
	}
	count := payload.Goroutines
	if count == 0 {
		count = componentGoroutines(payload.Components)
	}
	return ServiceRuntimeStats{Name: endpoint.Name, Count: count, MetricKind: RuntimeMetricGoroutine, Available: true}
}

type healthPayload struct {
	Goroutines int                        `json:"goroutines"`
	Components map[string]healthComponent `json:"components"`
}

type healthComponent struct {
	Detail map[string]any `json:"detail"`
}

func componentGoroutines(components map[string]healthComponent) int {
	app, ok := components["app"]
	if !ok {
		return 0
	}
	value, ok := app.Detail["goroutines"].(float64)
	if !ok {
		return 0
	}
	return int(value)
}
