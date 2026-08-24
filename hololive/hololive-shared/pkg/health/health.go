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

package health

import (
	"maps"
	"runtime"
	"sync"
	"time"
)

var (
	startTime  time.Time
	version    = "dev"
	initOnce   sync.Once
	components = make(map[string]ComponentStatus)
	componentM sync.RWMutex
)

func Init(v string) {
	initOnce.Do(func() {
		startTime = time.Now()

		if v != "" {
			version = v
		}
	})
}

type Response struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	Uptime     string                     `json:"uptime"`
	Goroutines int                        `json:"goroutines"`
	Components map[string]ComponentStatus `json:"components,omitempty"`
}

type ComponentStatus struct {
	Ready    bool `json:"ready"`
	Degraded bool `json:"degraded"`
}

func Get() Response {
	return Response{
		Status:     "ok",
		Version:    version,
		Uptime:     formatDuration(time.Since(startTime)),
		Goroutines: runtime.NumGoroutine(),
		Components: componentSnapshot(),
	}
}

func GetReadiness() (Response, bool) {
	response := Get()
	ready := true

	for _, component := range response.Components {
		if !component.Ready {
			ready = false
			break
		}
	}

	if !ready {
		response.Status = "not_ready"
	}

	return response, ready
}

func SetComponent(name string, status ComponentStatus) {
	componentM.Lock()

	components[name] = status
	componentM.Unlock()
}

func RemoveComponent(name string) {
	componentM.Lock()
	delete(components, name)
	componentM.Unlock()
}

func componentSnapshot() map[string]ComponentStatus {
	componentM.RLock()
	defer componentM.RUnlock()

	if len(components) == 0 {
		return nil
	}

	snapshot := make(map[string]ComponentStatus, len(components))
	maps.Copy(snapshot, components)

	return snapshot
}

func GetVersion() string {
	return version
}

func GetUptime() string {
	return formatDuration(time.Since(startTime))
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
