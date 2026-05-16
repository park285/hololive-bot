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

package httpserver_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"

	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// 간단한 핸들러
func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}

func TestH2CProtocolDetection(t *testing.T) {
	// H2C 서버 생성
	h2cHandler := sharedserver.WrapH2C(healthHandler())
	ts := httptest.NewUnstartedServer(h2cHandler)
	ts.Start()
	defer ts.Close()

	// H2C 클라이언트 (HTTP/2 without TLS)
	h2cTransport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	h2cClient := &http.Client{Transport: h2cTransport}

	resp, err := h2cClient.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("H2C 요청 실패: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("응답 바디 닫기 실패: %v", closeErr)
		}
	})

	// HTTP/2 프로토콜 확인
	if resp.ProtoMajor != 2 {
		t.Errorf("예상: HTTP/2, 실제: HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)
	} else {
		t.Logf("✅ H2C 프로토콜 확인: HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)
	}

	body, _ := io.ReadAll(resp.Body)
	t.Logf("응답: %s", string(body))
}

func TestHTTP1Fallback(t *testing.T) {
	h2cHandler := sharedserver.WrapH2C(healthHandler())
	ts := httptest.NewUnstartedServer(h2cHandler)
	ts.Start()
	defer ts.Close()

	// HTTP/1.1 클라이언트 (ForceAttemptHTTP2 = false)
	h1Transport := &http.Transport{
		ForceAttemptHTTP2: false,
	}
	h1Client := &http.Client{Transport: h1Transport}

	resp, err := h1Client.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("HTTP/1.1 요청 실패: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("응답 바디 닫기 실패: %v", closeErr)
		}
	})

	// HTTP/1.1 fallback 확인
	if resp.ProtoMajor != 1 {
		t.Errorf("예상: HTTP/1.1, 실제: HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)
	} else {
		t.Logf("✅ HTTP/1.1 Fallback 확인: HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)
	}
}

func BenchmarkHTTP1(b *testing.B) {
	handler := healthHandler()
	ts := httptest.NewUnstartedServer(handler)
	ts.Start()
	defer ts.Close()

	transport := &http.Transport{
		ForceAttemptHTTP2: false,
		MaxIdleConns:      100,
		MaxConnsPerHost:   100,
	}
	client := &http.Client{Transport: transport}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(ts.URL + "/health")
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.Fatal(closeErr)
		}
	}
}

func BenchmarkH2C(b *testing.B) {
	h2cHandler := sharedserver.WrapH2C(healthHandler())
	ts := httptest.NewUnstartedServer(h2cHandler)
	ts.Start()
	defer ts.Close()

	h2cTransport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: h2cTransport}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(ts.URL + "/health")
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func BenchmarkHTTP1Concurrent(b *testing.B) {
	handler := healthHandler()
	ts := httptest.NewUnstartedServer(handler)
	ts.Start()
	defer ts.Close()

	transport := &http.Transport{
		ForceAttemptHTTP2: false,
		MaxIdleConns:      100,
		MaxConnsPerHost:   100,
	}
	client := &http.Client{Transport: transport}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(ts.URL + "/health")
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}

func BenchmarkH2CConcurrent(b *testing.B) {
	h2cHandler := sharedserver.WrapH2C(healthHandler())
	ts := httptest.NewUnstartedServer(h2cHandler)
	ts.Start()
	defer ts.Close()

	h2cTransport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: h2cTransport}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(ts.URL + "/health")
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}

func TestLatencyComparison(t *testing.T) {
	// HTTP/1.1 서버
	h1Handler := healthHandler()
	h1Server := httptest.NewUnstartedServer(h1Handler)
	h1Server.Start()
	defer h1Server.Close()

	// H2C 서버
	h2cHandler := sharedserver.WrapH2C(healthHandler())
	h2cServer := httptest.NewUnstartedServer(h2cHandler)
	h2cServer.Start()
	defer h2cServer.Close()

	// HTTP/1.1 클라이언트
	h1Transport := &http.Transport{
		ForceAttemptHTTP2: false,
		MaxIdleConns:      100,
		MaxConnsPerHost:   100,
	}
	h1Client := &http.Client{Transport: h1Transport}

	// H2C 클라이언트
	h2cTransport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	h2cClient := &http.Client{Transport: h2cTransport}

	const requests = 100
	const concurrency = 10

	// HTTP/1.1 레이턴시 측정
	h1Latencies := measureLatency(t, h1Client, h1Server.URL+"/health", requests, concurrency)

	// H2C 레이턴시 측정
	h2cLatencies := measureLatency(t, h2cClient, h2cServer.URL+"/health", requests, concurrency)

	t.Logf("\n=== 레이턴시 비교 (요청 %d개, 동시성 %d) ===", requests, concurrency)
	t.Logf("HTTP/1.1: 평균 %.3fms, 최소 %.3fms, 최대 %.3fms",
		avg(h1Latencies), min(h1Latencies), max(h1Latencies))
	t.Logf("H2C:      평균 %.3fms, 최소 %.3fms, 최대 %.3fms",
		avg(h2cLatencies), min(h2cLatencies), max(h2cLatencies))

	improvement := (avg(h1Latencies) - avg(h2cLatencies)) / avg(h1Latencies) * 100
	t.Logf("H2C 개선율: %.1f%%", improvement)
}

func measureLatency(t *testing.T, client *http.Client, url string, requests, concurrency int) []float64 {
	var (
		mu        sync.Mutex
		latencies []float64
		wg        sync.WaitGroup
	)

	sem := make(chan struct{}, concurrency)

	for range requests {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			start := time.Now()
			resp, err := client.Get(url)
			if err != nil {
				t.Logf("요청 실패: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()

			latency := float64(time.Since(start).Microseconds()) / 1000.0
			mu.Lock()
			latencies = append(latencies, latency)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return latencies
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func min(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func max(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func TestMultiplexingBenefit(t *testing.T) {
	h2cHandler := sharedserver.WrapH2C(healthHandler())
	ts := httptest.NewUnstartedServer(h2cHandler)
	ts.Start()
	defer ts.Close()

	h2cTransport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: h2cTransport}

	// 동시에 50개의 요청을 단일 연결로 처리
	const numRequests = 50
	var wg sync.WaitGroup
	start := time.Now()

	for i := range numRequests {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := client.Get(fmt.Sprintf("%s/health?req=%d", ts.URL, idx))
			if err != nil {
				t.Logf("요청 %d 실패: %v", idx, err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("✅ H2C 멀티플렉싱: %d개 동시 요청 완료 in %v", numRequests, elapsed)
	t.Logf("   평균 요청 시간: %.3fms", float64(elapsed.Microseconds())/float64(numRequests)/1000.0)
}
