package status

import (
	"context"
	"sync"
	"time"

	jsonv2 "encoding/json/v2"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

const defaultEndpointSampleTTL = 2 * time.Second

type endpointSample struct {
	status  ServiceStatus
	runtime ServiceRuntimeStats
}

type endpointSnapshot struct {
	sampledAt time.Time
	endpoints []endpointSample
}

type Sampler struct {
	endpoints []ServiceEndpoint
	clients   map[string]endpointClient
	ttl       time.Duration
	now       func() time.Time

	mu     sync.Mutex
	cached endpointSnapshot

	closeOnce sync.Once
	closeErr  error
}

func NewSampler(endpoints []ServiceEndpoint) *Sampler {
	return &Sampler{
		endpoints: append([]ServiceEndpoint(nil), endpoints...),
		clients:   endpointClients(endpoints, 3*time.Second),
		ttl:       defaultEndpointSampleTTL,
		now:       time.Now,
	}
}

func (s *Sampler) Close() error {
	if s == nil {
		return nil
	}

	s.closeOnce.Do(func() {
		s.closeErr = closeEndpointClients(s.clients)
	})

	return s.closeErr
}

func (s *Sampler) sample(ctx context.Context) endpointSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if !s.cached.sampledAt.IsZero() && now.Sub(s.cached.sampledAt) < s.ttl {
		return cloneEndpointSnapshot(s.cached)
	}

	snapshot := endpointSnapshot{
		sampledAt: now,
		endpoints: make([]endpointSample, len(s.endpoints)),
	}
	var wg sync.WaitGroup
	for i := range s.endpoints {
		index := i
		endpoint := s.endpoints[i]
		wg.Add(1)
		panicguard.Go(nil, "admin-dashboard-endpoint-sample", func() {
			defer wg.Done()
			var sample endpointSample
			if err := panicguard.RunE(nil, "admin-dashboard-endpoint-sample", func() error {
				sample = s.sampleEndpoint(ctx, endpoint)
				return nil
			}); err != nil {
				errText := err.Error()
				sample = failedEndpointSample(endpoint.Name, errText, nil)
			}
			snapshot.endpoints[index] = sample
		})
	}
	wg.Wait()
	if ctx.Err() == nil {
		s.cached = cloneEndpointSnapshot(snapshot)
	}
	return snapshot
}

func (s *Sampler) sampleEndpoint(ctx context.Context, endpoint ServiceEndpoint) endpointSample {
	result := doHealthGET(ctx, s.clients[endpoint.Name], endpoint)
	if result.errMsg != "" {
		var latency *uint64
		if result.measured {
			value := result.latencyMS
			latency = &value
		}
		return failedEndpointSample(endpoint.Name, result.errMsg, latency)
	}
	latency := result.latencyMS
	sample := endpointSample{
		status: ServiceStatus{
			Name:           endpoint.Name,
			Available:      true,
			ResponseTimeMS: &latency,
		},
		runtime: ServiceRuntimeStats{
			Name:       endpoint.Name,
			MetricKind: RuntimeMetricGoroutine,
		},
	}
	var payload healthPayload
	decodeErr := jsonv2.UnmarshalRead(result.resp.Body, &payload)
	closeErr := result.resp.Body.Close()
	if decodeErr != nil {
		errText := "invalid health payload: " + decodeErr.Error()
		sample.runtime.Error = &errText
		return sample
	}
	if closeErr != nil {
		errText := "close health payload: " + closeErr.Error()
		sample.runtime.Error = &errText
		return sample
	}
	count := payload.Goroutines
	if count == 0 {
		count = componentGoroutines(payload.Components)
	}
	sample.runtime.Count = count
	sample.runtime.Available = true
	return sample
}

func failedEndpointSample(name, errText string, latency *uint64) endpointSample {
	statusError := errText
	runtimeError := errText
	return endpointSample{
		status: ServiceStatus{
			Name:           name,
			Available:      false,
			ResponseTimeMS: latency,
			Error:          &statusError,
		},
		runtime: ServiceRuntimeStats{
			Name:       name,
			MetricKind: RuntimeMetricGoroutine,
			Available:  false,
			Error:      &runtimeError,
		},
	}
}

func cloneEndpointSnapshot(snapshot endpointSnapshot) endpointSnapshot {
	cloned := endpointSnapshot{
		sampledAt: snapshot.sampledAt,
		endpoints: make([]endpointSample, len(snapshot.endpoints)),
	}
	for i := range snapshot.endpoints {
		cloned.endpoints[i] = endpointSample{
			status:  cloneServiceStatus(snapshot.endpoints[i].status),
			runtime: cloneServiceRuntimeStat(snapshot.endpoints[i].runtime),
		}
	}
	return cloned
}

func cloneServiceStatus(status ServiceStatus) ServiceStatus {
	if status.ResponseTimeMS != nil {
		value := *status.ResponseTimeMS
		status.ResponseTimeMS = &value
	}
	if status.Error != nil {
		value := *status.Error
		status.Error = &value
	}
	return status
}

func cloneServiceRuntimeStat(stats ServiceRuntimeStats) ServiceRuntimeStats {
	if stats.Error != nil {
		value := *stats.Error
		stats.Error = &value
	}
	return stats
}
