package status

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
)

const historyCap = 30

type Hub struct {
	endpointSampler *Sampler
	processSampler  *procSampler
	mu              sync.Mutex
	nextID          int64
	subs            map[int64]chan SystemStats
	history         []SystemStats

	lifecycleMu sync.Mutex
	started     bool
	stopOnce    sync.Once
	stop        chan struct{}
	done        chan struct{}
}

func NewHub(endpoints []ServiceEndpoint) *Hub {
	return NewHubWithSampler(NewSampler(endpoints))
}

func NewHubWithSampler(sampler *Sampler) *Hub {
	return &Hub{
		endpointSampler: sampler,
		processSampler:  &procSampler{},
		subs:            make(map[int64]chan SystemStats),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
}

func (h *Hub) Start() {
	h.StartContext(context.Background())
}

func (h *Hub) StartContext(ctx context.Context) {
	if h == nil {
		return
	}
	if ctx == nil {
		return
	}

	h.lifecycleMu.Lock()
	if h.started {
		h.lifecycleMu.Unlock()
		return
	}
	h.started = true
	h.lifecycleMu.Unlock()

	panicguard.Go(nil, "admin-dashboard-status-hub", func() {
		h.run(ctx)
	})
}

func (h *Hub) run(ctx context.Context) {
	defer close(h.done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for h.tick(ctx, ticker.C) {
	}
}

func (h *Hub) tick(ctx context.Context, tick <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-h.stop:
		return false
	case <-tick:
		h.broadcastIfSubscribed(ctx)
		return true
	}
}

func (h *Hub) broadcastIfSubscribed(ctx context.Context) {
	if h.hasSubscribers() {
		h.broadcastSample(ctx)
	}
}

func (h *Hub) hasSubscribers() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs) > 0
}

func (h *Hub) broadcastSample(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	stats := h.collect(ctx)
	h.Publish(&stats)
}

func (h *Hub) Stop() {
	if h == nil {
		return
	}

	h.lifecycleMu.Lock()
	if !h.started {
		h.lifecycleMu.Unlock()
		return
	}
	h.stopOnce.Do(func() {
		close(h.stop)
	})
	done := h.done
	h.lifecycleMu.Unlock()

	<-done
}

func (h *Hub) Subscribe() (history []SystemStats, updates chan SystemStats, unsubscribe func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	ch := make(chan SystemStats, 4)
	h.subs[id] = ch

	var unsubscribeOnce sync.Once
	return cloneSystemStatsHistory(h.history), ch, func() {
		unsubscribeOnce.Do(func() {
			h.mu.Lock()
			if _, ok := h.subs[id]; ok {
				delete(h.subs, id)
				close(ch)
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) Publish(stats *SystemStats) {
	snapshot := cloneSystemStats(stats)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, snapshot)
	if len(h.history) > historyCap {
		h.history = h.history[len(h.history)-historyCap:]
	}
	for _, ch := range h.subs {
		subscriberSnapshot := cloneSystemStats(&snapshot)
		sendDropOldest(ch, &subscriberSnapshot)
	}
}

func sendDropOldest(ch chan SystemStats, stats *SystemStats) {
	if trySend(ch, stats) {
		return
	}
	select {
	case <-ch:
	default:
	}
	trySend(ch, stats)
}

func trySend(ch chan SystemStats, stats *SystemStats) bool {
	select {
	case ch <- *stats:
		return true
	default:
		return false
	}
}

func (h *Hub) collect(ctx context.Context) SystemStats {
	endpointSnapshot := h.endpointSampler.sample(ctx)
	memTotal, memUsed := memoryStats()
	load1, load5, load15 := loadAverage()
	threadCount := threadCount()
	adminGoroutines := runtime.NumGoroutine()
	serviceRuntime := make([]ServiceRuntimeStats, 0, len(endpointSnapshot.endpoints)+1)
	serviceRuntime = append(serviceRuntime, ServiceRuntimeStats{Name: "admin-dashboard", Count: adminGoroutines, MetricKind: RuntimeMetricGoroutine, Available: true})
	for i := range endpointSnapshot.endpoints {
		serviceRuntime = append(serviceRuntime, cloneServiceRuntimeStat(endpointSnapshot.endpoints[i].runtime))
	}
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
		SampledAt:         endpointSnapshot.sampledAt.UnixMilli(),
		CPUUsage:          h.processSampler.cpuUsage(),
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
	snapshot := h.endpointSampler.sample(ctx)
	results := make([]ServiceRuntimeStats, len(snapshot.endpoints))
	for i := range snapshot.endpoints {
		results[i] = cloneServiceRuntimeStat(snapshot.endpoints[i].runtime)
	}
	return results
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
