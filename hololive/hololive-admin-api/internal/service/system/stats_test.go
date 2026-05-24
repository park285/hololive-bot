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
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/park285/shared-go/pkg/json"
)

func TestCloneSystemStats(t *testing.T) {
	t.Parallel()

	t.Run("nil 입력은 nil을 반환", func(t *testing.T) {
		t.Parallel()

		got := cloneSystemStats(nil)
		if got != nil {
			t.Errorf("cloneSystemStats(nil) = %v, want nil", got)
		}
	})

	t.Run("원본 수정이 클론에 영향을 주지 않음 (deep copy)", func(t *testing.T) {
		t.Parallel()

		original := &SystemStats{
			CPUUsage:    25.0,
			MemoryUsage: 50.0,
			MemoryTotal: 8 * 1024 * 1024 * 1024,
			MemoryUsed:  4 * 1024 * 1024 * 1024,
			Goroutines:  100,
			ServiceGoroutines: []ServiceGoroutines{
				{Name: "svc-a", Goroutines: 10, Available: true},
				{Name: "svc-b", Goroutines: 20, Available: true},
			},
		}

		cloned := cloneSystemStats(original)

		// 원본 슬라이스 요소 수정
		original.ServiceGoroutines[0].Goroutines = 999
		original.CPUUsage = 99.9

		if cloned.CPUUsage != 25.0 {
			t.Errorf("cloned.CPUUsage = %v, want 25.0 (원본 수정에 영향받으면 안 됨)", cloned.CPUUsage)
		}

		if cloned.ServiceGoroutines[0].Goroutines != 10 {
			t.Errorf("cloned.ServiceGoroutines[0].Goroutines = %v, want 10 (슬라이스 deep copy 실패)", cloned.ServiceGoroutines[0].Goroutines)
		}
	})

	t.Run("ServiceGoroutines가 빈 슬라이스이면 정상 동작", func(t *testing.T) {
		t.Parallel()

		original := &SystemStats{
			CPUUsage:          10.0,
			ServiceGoroutines: []ServiceGoroutines{},
		}

		cloned := cloneSystemStats(original)
		if cloned == nil {
			t.Fatal("cloneSystemStats with empty ServiceGoroutines returned nil")
		}

		if cloned.CPUUsage != 10.0 {
			t.Errorf("cloned.CPUUsage = %v, want 10.0", cloned.CPUUsage)
		}
	})
}

func TestCollector_FetchGoroutineCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantOK    bool
	}{
		{
			name: "goroutines 직접 필드 형식 파싱 성공",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				_ = json.NewEncoder(w).Encode(map[string]any{"goroutines": 42})
			},
			wantCount: 42,
			wantOK:    true,
		},
		{
			name: "components.app.detail.goroutines 형식 파싱 성공",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				body := map[string]any{
					"components": map[string]any{
						"app": map[string]any{
							"detail": map[string]any{
								"goroutines": 30,
							},
						},
					},
				}

				_ = json.NewEncoder(w).Encode(body)
			},
			wantCount: 30,
			wantOK:    true,
		},
		{
			name: "비-200 응답은 (0, false) 반환",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			wantCount: 0,
			wantOK:    false,
		},
		{
			name: "잘못된 JSON은 (0, false) 반환",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				_, _ = w.Write([]byte("{invalid json"))
			},
			wantCount: 0,
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			// 동일 패키지이므로 unexported 필드 직접 초기화 가능
			c := &Collector{
				httpClient: srv.Client(),
				endpoints:  nil,
			}

			gotCount, gotOK := c.fetchGoroutineCount(t.Context(), srv.URL)

			if gotOK != tc.wantOK {
				t.Errorf("fetchGoroutineCount() ok = %v, want %v", gotOK, tc.wantOK)
			}

			if gotCount != tc.wantCount {
				t.Errorf("fetchGoroutineCount() count = %v, want %v", gotCount, tc.wantCount)
			}
		})
	}
}
