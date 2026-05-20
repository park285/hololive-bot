package producerruntime

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

type readinessClaimStub struct {
	status poller.JobClaimStatus
	err    error
}

func (s readinessClaimStub) TryClaim(
	context.Context,
	string,
	string,
	time.Duration,
	time.Duration,
) (poller.JobClaimStatus, poller.JobClaim, error) {
	return s.status, nil, s.err
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
