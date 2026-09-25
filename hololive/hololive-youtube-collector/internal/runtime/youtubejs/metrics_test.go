package youtubejs

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type metricsTransport func(*http.Request) (*http.Response, error)

func (f metricsTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRPCMetricsSeparateLimiterWaitFromHelper(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := ratelimiter.New(2 * time.Second)
		if err := limiter.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}

		client := NewRPC(&http.Client{Transport: metricsTransport(func(*http.Request) (*http.Response, error) {
			time.Sleep(50 * time.Millisecond)

			return jsonResponse(http.StatusOK, `{"protocol_version":1,"items":[],"page_count":1,"exhausted":true,"continuity":"NOT_APPLICABLE","termination_reason":"exhausted"}`), nil
		})}, "http://helper", limiter)
		reg := prometheus.NewPedanticRegistry()
		client.EnableMetrics(reg, 2*time.Second)

		if _, err := client.FetchContent(t.Context(), ContentRequest{ChannelID: "private-channel", Kind: "videos"}); err != nil {
			t.Fatal(err)
		}

		assertRPCPhase(t, reg, "rate_limit", "success", 2)
		assertRPCPhase(t, reg, "helper", "success", 0.05)

		for _, phase := range []string{"rate_limit", "helper"} {
			if got := testutil.ToFloat64(client.metrics.inFlight.WithLabelValues("content", phase)); got != 0 {
				t.Fatalf("%s in-flight = %v", phase, got)
			}
		}
	})
}

func TestRPCMetricsCanceledAdmissionDoesNotCallHelper(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := ratelimiter.New(2 * time.Second)
		if err := limiter.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}

		client := NewRPC(&http.Client{Transport: metricsTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("helper called after admission timeout")

			return nil, context.DeadlineExceeded
		})}, "http://helper", limiter)
		reg := prometheus.NewPedanticRegistry()
		client.EnableMetrics(reg, 2*time.Second)

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		if _, err := client.FetchContent(ctx, ContentRequest{ChannelID: "private-channel", Kind: "videos"}); err == nil {
			t.Fatal("admission timeout was lost")
		}

		assertRPCPhase(t, reg, "rate_limit", "timeout", 1)

		if got := testutil.ToFloat64(client.metrics.inFlight.WithLabelValues("content", "rate_limit")); got != 0 {
			t.Fatalf("canceled wait left in-flight = %v", got)
		}
	})
}

func assertRPCPhase(t *testing.T, reg *prometheus.Registry, phase, outcome string, seconds float64) {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}

	for _, family := range families {
		if family.GetName() != "youtubejs_rpc_phase_duration_seconds" {
			continue
		}

		for _, metric := range family.Metric {
			labels := make(map[string]string)

			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}

			if len(labels) != 3 || labels["operation"] != "content" {
				t.Fatalf("unexpected metric labels: %v", labels)
			}

			if labels["phase"] == phase && labels["outcome"] == outcome {
				if metric.GetHistogram().GetSampleCount() != 1 || metric.GetHistogram().GetSampleSum() != seconds {
					t.Fatalf("%s duration = %v", phase, metric.GetHistogram())
				}

				return
			}
		}
	}

	t.Fatalf("missing phase=%s outcome=%s", phase, outcome)
}
