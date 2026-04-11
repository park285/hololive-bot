package providers

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
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

func TestProvideScraperScheduler_UsesExplicitChannelIDs(t *testing.T) {
	t.Parallel()

	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(noopPoller{}, 0, 15*time.Minute),
		}),
		WithSchedulerChannelIDs([]string{"UC_A", "UC_B"}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	got := providerJobKeys(t, scheduler)
	want := []string{"UC_A:noop", "UC_B:noop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providerJobKeys() = %v, want %v", got, want)
	}
}

func providerJobKeys(t *testing.T, scheduler *poller.Scheduler) []string {
	t.Helper()

	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	if !field.IsValid() {
		t.Fatal("jobMap field must exist")
	}

	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	keys := make([]string, 0, field.Len())
	iterator := field.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}
	slices.Sort(keys)
	return keys
}
