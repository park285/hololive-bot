package polling

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/internal/admission"
)

type admissionTestPoller struct {
	name string
}

func (p admissionTestPoller) Poll(context.Context, string) error { return nil }
func (p admissionTestPoller) Name() string                       { return p.name }

type admissionDeferredClaimStub struct {
	deferCalled         bool
	deferTTL            time.Duration
	markCompletedCalled bool
	releaseCalled       bool
}

func (c *admissionDeferredClaimStub) Renew(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (c *admissionDeferredClaimStub) MarkCompleted(context.Context, time.Duration) (bool, error) {
	c.markCompletedCalled = true
	return true, nil
}

func (c *admissionDeferredClaimStub) Release(context.Context) (bool, error) {
	c.releaseCalled = true
	return true, nil
}

func (c *admissionDeferredClaimStub) Defer(_ context.Context, ttl time.Duration) (bool, error) {
	c.deferCalled = true
	c.deferTTL = ttl
	return true, nil
}

type admissionNonDeferrableClaimStub struct {
	releaseCalled bool
}

func (c *admissionNonDeferrableClaimStub) Renew(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (c *admissionNonDeferrableClaimStub) MarkCompleted(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (c *admissionNonDeferrableClaimStub) Release(context.Context) (bool, error) {
	c.releaseCalled = true
	return true, nil
}

func TestSchedulerRescheduleJobAfterPoll_AdmissionDeferredDoesNotIncrementFailures(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0, ErrorBackoffMin: time.Second})
	poller := admissionTestPoller{name: "community"}
	scheduler.Register("channel-1", poller, PriorityNormal, time.Minute)
	job := scheduler.jobMap["channel-1:community"]
	if job == nil {
		t.Fatalf("registered job not found")
	}
	job.consecutiveFailures = 3
	delay := 5 * time.Second
	pollErr := admission.NewDeferredError("test", "bucket", "local_interval", delay, nil)

	before := time.Now()
	scheduler.rescheduleJobAfterPoll(job, pollErr)
	after := time.Now()

	if job.consecutiveFailures != 0 {
		t.Fatalf("consecutiveFailures = %d, want 0", job.consecutiveFailures)
	}
	lower := before.Add(delay)
	upper := after.Add(delay).Add(20 * time.Millisecond)
	if job.NextRunAt.Before(lower) || job.NextRunAt.After(upper) {
		t.Fatalf("NextRunAt = %s, want between %s and %s", job.NextRunAt, lower, upper)
	}
}

func TestSchedulerLogPollResult_AdmissionDeferredStatus(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0})
	job := &Job{ChannelID: "channel-1", Poller: admissionTestPoller{name: "community"}}
	pollErr := admission.NewDeferredError("test", "bucket", "local_interval", time.Second, nil)

	status := scheduler.logPollResult(job, 1, context.Background(), time.Millisecond, pollErr)
	if status != "deferred" {
		t.Fatalf("status = %q, want deferred", status)
	}
}

func TestSchedulerFinishJobClaim_AdmissionDeferredUsesDeferrableClaim(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0, ErrorBackoffMin: time.Second})
	job := &Job{ChannelID: "channel-1", Poller: admissionTestPoller{name: "community"}, Interval: time.Minute}
	claim := &admissionDeferredClaimStub{}
	delay := 4 * time.Second
	pollErr := admission.NewDeferredError("test", "bucket", "local_interval", delay, nil)

	err := scheduler.finishJobClaim(context.Background(), job, claim, pollErr)
	if err != pollErr {
		t.Fatalf("finishJobClaim err = %v, want original deferred err", err)
	}
	if !claim.deferCalled {
		t.Fatalf("Defer was not called")
	}
	if claim.deferTTL != delay {
		t.Fatalf("Defer ttl = %s, want %s", claim.deferTTL, delay)
	}
	if claim.markCompletedCalled {
		t.Fatalf("MarkCompleted must not be called for admission deferred poll")
	}
	if claim.releaseCalled {
		t.Fatalf("Release must not be called when Defer succeeds")
	}
}

func TestSchedulerFinishJobClaim_AdmissionDeferredFallsBackToRelease(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0, ErrorBackoffMin: time.Second})
	job := &Job{ChannelID: "channel-1", Poller: admissionTestPoller{name: "community"}, Interval: time.Minute}
	claim := &admissionNonDeferrableClaimStub{}
	pollErr := admission.NewDeferredError("test", "bucket", "local_interval", time.Second, nil)

	err := scheduler.finishJobClaim(context.Background(), job, claim, pollErr)
	if err != pollErr {
		t.Fatalf("finishJobClaim err = %v, want original deferred err", err)
	}
	if !claim.releaseCalled {
		t.Fatalf("Release was not called for non-deferrable claim")
	}
}
