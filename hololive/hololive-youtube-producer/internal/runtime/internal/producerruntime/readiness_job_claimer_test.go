package producerruntime

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

type readinessClaimStub struct {
	status poller.JobClaimStatus
	claim  poller.JobClaim
	err    error
}

func (s readinessClaimStub) TryClaim(
	context.Context,
	string,
	string,
	time.Duration,
	time.Duration,
) (poller.JobClaimStatus, poller.JobClaim, error) {
	return s.status, s.claim, s.err
}

type readinessBudgetLimiterStub struct {
	reservation poller.BudgetReservation
	decision    poller.BudgetDecision
	err         error
	calls       int
}

func (s *readinessBudgetLimiterStub) TryReserve(
	context.Context,
	poller.BudgetJob,
	poller.BudgetProfile,
	time.Duration,
) (poller.BudgetReservation, poller.BudgetDecision, error) {
	s.calls++
	return s.reservation, s.decision, s.err
}

type readinessProbeClaimStub struct {
	released bool
}

func (s *readinessProbeClaimStub) Renew(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (s *readinessProbeClaimStub) MarkCompleted(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (s *readinessProbeClaimStub) Release(context.Context) (bool, error) {
	s.released = true
	return true, nil
}

func TestReadinessReportingJobClaimerMarksLeaseUnavailable(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		ActiveActiveEnabled: true,
	})
	state.MarkRunning()
	claimer := newReadinessReportingJobClaimer(readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
		err:    fmt.Errorf("valkey unavailable"),
	}, state)

	_, _, err := claimer.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)

	if err == nil {
		t.Fatal("TryClaim error = nil, want error")
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["valkey_available"] != false {
		t.Fatalf("valkey_available = %v, want false", payload["valkey_available"])
	}
}

func TestReadinessReportingJobClaimerMarksLeaseAvailable(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		ActiveActiveEnabled: true,
	})
	state.MarkRunning()
	state.MarkLeaseUnavailable("")
	claimer := newReadinessReportingJobClaimer(readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimPeerOwned},
	}, state)

	_, _, err := claimer.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)

	if err != nil {
		t.Fatalf("TryClaim error = %v, want nil", err)
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["valkey_available"] != true {
		t.Fatalf("valkey_available = %v, want true", payload["valkey_available"])
	}
}

func TestProbeReadinessJobClaimerMarksLeaseAvailableAndReleasesSyntheticClaim(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		ActiveActiveEnabled: true,
	})
	state.MarkRunning()
	claim := &readinessProbeClaimStub{}
	claimer := newReadinessReportingJobClaimer(readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimAcquired},
		claim:  claim,
	}, state)

	probeReadinessJobClaimer(context.Background(), claimer, nil)

	if !claim.released {
		t.Fatal("probe claim was not released")
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["valkey_available"] != true {
		t.Fatalf("valkey_available = %v, want true", payload["valkey_available"])
	}
}

func TestProbeReadinessJobClaimerKeepsLeaseUnavailableOnFailure(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		ActiveActiveEnabled: true,
	})
	state.MarkRunning()
	claimer := newReadinessReportingJobClaimer(readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
		err:    fmt.Errorf("valkey unavailable"),
	}, state)

	probeReadinessJobClaimer(context.Background(), claimer, nil)

	statusCode, payload := state.Response()
	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["scraping_paused"] != true {
		t.Fatalf("scraping_paused = %v, want true", payload["scraping_paused"])
	}
}

func TestReadinessReportingBudgetLimiterMarksBackendUnavailableOnError(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()
	limiter := newReadinessReportingBudgetLimiter(&readinessBudgetLimiterStub{
		err: fmt.Errorf("valkey unavailable"),
	}, state)

	_, _, err := limiter.TryReserve(context.Background(), poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)

	if err == nil {
		t.Fatal("TryReserve error = nil, want error")
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if payload["budget_backend_available"] != false {
		t.Fatalf("budget_backend_available = %v, want false", payload["budget_backend_available"])
	}
	if payload["scraping_paused"] != true {
		t.Fatalf("scraping_paused = %v, want true", payload["scraping_paused"])
	}
}

func TestReadinessReportingBudgetLimiterMarksBudgetExhausted(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()
	limiter := newReadinessReportingBudgetLimiter(&readinessBudgetLimiterStub{
		decision: poller.BudgetDecision{Allowed: false, Reason: "budget_exhausted"},
	}, state)

	_, decision, err := limiter.TryReserve(context.Background(), poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper, poller.BudgetSourceHolodexLive), time.Minute)

	if err != nil {
		t.Fatalf("TryReserve error = %v, want nil", err)
	}
	if decision.Allowed {
		t.Fatal("decision.Allowed = true, want false")
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["budget_backend_available"] != true {
		t.Fatalf("budget_backend_available = %v, want true", payload["budget_backend_available"])
	}
	if payload["budget_exhausted"] != true {
		t.Fatalf("budget_exhausted = %v, want true", payload["budget_exhausted"])
	}
	if payload["source_cooldown"] != false {
		t.Fatalf("source_cooldown = %v, want false", payload["source_cooldown"])
	}
	wantSources := []string{"holodex_live", "youtube_scraper"}
	if !reflect.DeepEqual(payload["affected_sources"], wantSources) {
		t.Fatalf("affected_sources = %v, want %v", payload["affected_sources"], wantSources)
	}
}

func TestReadinessReportingBudgetLimiterMarksSourceCooldown(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()
	limiter := newReadinessReportingBudgetLimiter(&readinessBudgetLimiterStub{
		decision: poller.BudgetDecision{Allowed: false, Reason: "source_cooldown"},
	}, state)

	_, _, err := limiter.TryReserve(context.Background(), poller.BudgetJob{PollerName: "live"}, readinessBudgetProfile(poller.BudgetSourceHolodexLive), time.Minute)

	if err != nil {
		t.Fatalf("TryReserve error = %v, want nil", err)
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["budget_exhausted"] != false {
		t.Fatalf("budget_exhausted = %v, want false", payload["budget_exhausted"])
	}
	if payload["source_cooldown"] != true {
		t.Fatalf("source_cooldown = %v, want true", payload["source_cooldown"])
	}
	wantSources := []string{"holodex_live"}
	if !reflect.DeepEqual(payload["affected_sources"], wantSources) {
		t.Fatalf("affected_sources = %v, want %v", payload["affected_sources"], wantSources)
	}
}

func TestReadinessReportingBudgetLimiterAllowedClearsAdmissionAndBackend(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{
		YouTubeEnabled:      true,
		GlobalBudgetEnabled: true,
	})
	state.MarkRunning()
	state.MarkBudgetBackendUnavailable("valkey_unavailable_global_budget_fail_closed")
	state.MarkBudgetAdmissionDenied("budget_exhausted", []string{"youtube_scraper"})
	limiter := newReadinessReportingBudgetLimiter(&readinessBudgetLimiterStub{
		decision: poller.BudgetDecision{Allowed: true},
	}, state)

	_, decision, err := limiter.TryReserve(context.Background(), poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)

	if err != nil {
		t.Fatalf("TryReserve error = %v, want nil", err)
	}
	if !decision.Allowed {
		t.Fatal("decision.Allowed = false, want true")
	}
	statusCode, payload := state.Response()
	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusCode, http.StatusOK)
	}
	if payload["budget_backend_available"] != true {
		t.Fatalf("budget_backend_available = %v, want true", payload["budget_backend_available"])
	}
	if payload["budget_exhausted"] != false {
		t.Fatalf("budget_exhausted = %v, want false", payload["budget_exhausted"])
	}
	if payload["source_cooldown"] != false {
		t.Fatalf("source_cooldown = %v, want false", payload["source_cooldown"])
	}
	wantSources := []string{}
	if !reflect.DeepEqual(payload["affected_sources"], wantSources) {
		t.Fatalf("affected_sources = %v, want empty slice", payload["affected_sources"])
	}
}

func readinessBudgetProfile(sources ...poller.BudgetSource) poller.BudgetProfile {
	sourceUnits := make(map[poller.BudgetSource]float64, len(sources))
	for _, source := range sources {
		sourceUnits[source] = 1
	}
	return poller.BudgetProfile{
		SourceUnits: sourceUnits,
		BurstClass:  poller.BudgetBurstPrimary,
		Priority:    poller.BudgetPriorityNormal,
	}
}
