package providers

import (
	"context"
	"testing"
	"time"
)

type noopPoller struct{}

func (noopPoller) Poll(context.Context, string) error { return nil }
func (noopPoller) Name() string                       { return "noop" }

func TestEstimatedRequestsPerMinute(t *testing.T) {
	t.Parallel()

	registrations := []ChannelPollerRegistration{
		NewChannelPollerRegistration(noopPoller{}, 0, 15*time.Minute),
		NewChannelPollerRegistration(noopPoller{}, 0, 30*time.Minute),
		NewChannelPollerRegistration(noopPoller{}, 0, 0),
	}

	got := estimatedRequestsPerMinute(registrations)
	want := (60.0 / (15 * time.Minute).Seconds()) + (60.0 / (30 * time.Minute).Seconds())
	if got != want {
		t.Fatalf("estimatedRequestsPerMinute() = %f, want %f", got, want)
	}
}
