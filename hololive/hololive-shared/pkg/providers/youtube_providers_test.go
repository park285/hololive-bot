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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type noopPoller struct{}

func (noopPoller) Poll(context.Context, string) error { return nil }
func (noopPoller) Name() string                       { return "noop" }

type namedNoopPoller struct {
	name string
}

func (p namedNoopPoller) Poll(context.Context, string) error { return nil }
func (p namedNoopPoller) Name() string                       { return p.name }

type testMemberDataProvider struct {
	members []*domain.Member
}

func (p testMemberDataProvider) GetAllMembers() []*domain.Member {
	return p.members
}

func (p testMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member { return nil }
func (p testMemberDataProvider) FindMemberByName(name string) *domain.Member           { return nil }
func (p testMemberDataProvider) FindMemberByAlias(alias string) *domain.Member         { return nil }
func (p testMemberDataProvider) GetChannelIDs() []string                               { return nil }
func (p testMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider {
	return p
}
func (p testMemberDataProvider) FindMembersByName(name string) []*domain.Member   { return nil }
func (p testMemberDataProvider) FindMembersByAlias(alias string) []*domain.Member { return nil }

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

func TestProvideScraperScheduler_UsesPerRegistrationChannelIDs(t *testing.T) {
	t.Parallel()

	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(namedNoopPoller{name: "videos"}, 0, 15*time.Minute).WithChannelIDs([]string{"UC_A"}),
			NewChannelPollerRegistration(namedNoopPoller{name: "stats"}, 0, time.Hour).WithChannelIDs([]string{"UC_A", "UC_B"}),
		}),
		WithSchedulerChannelIDs([]string{"UC_X", "UC_Y"}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	got := providerJobKeys(t, scheduler)
	want := []string{"UC_A:stats", "UC_A:videos", "UC_B:stats"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providerJobKeys() = %v, want %v", got, want)
	}
}

func TestProvideScraperScheduler_ExplicitRegistrationsWorkWithoutDefaultChannelIDs(t *testing.T) {
	t.Parallel()

	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(namedNoopPoller{name: "videos"}, 0, 15*time.Minute).WithChannelIDs([]string{"UC_A"}),
			NewChannelPollerRegistration(namedNoopPoller{name: "stats"}, 0, time.Hour).WithChannelIDs([]string{"UC_B"}),
		}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	got := providerJobKeys(t, scheduler)
	want := []string{"UC_A:videos", "UC_B:stats"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providerJobKeys() = %v, want %v", got, want)
	}
}

func TestProvideScraperScheduler_NonExplicitRegistrationsRequireDefaultsOrMembers(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registrations := []ChannelPollerRegistration{
		NewChannelPollerRegistration(namedNoopPoller{name: "videos"}, 0, 15*time.Minute),
	}

	t.Run("without defaults or members no jobs are created", func(t *testing.T) {
		t.Parallel()

		scheduler := ProvideScraperScheduler(
			nil,
			logger,
			WithChannelPollerRegistrations(registrations),
		)
		if scheduler == nil {
			t.Fatal("scheduler is nil")
		}

		got := providerJobKeys(t, scheduler)
		if len(got) != 0 {
			t.Fatalf("providerJobKeys() = %v, want empty", got)
		}
	})

	t.Run("defaults still backfill non-explicit registrations", func(t *testing.T) {
		t.Parallel()

		scheduler := ProvideScraperScheduler(
			nil,
			logger,
			WithChannelPollerRegistrations(registrations),
			WithSchedulerChannelIDs([]string{"UC_DEFAULT"}),
		)
		if scheduler == nil {
			t.Fatal("scheduler is nil")
		}

		got := providerJobKeys(t, scheduler)
		want := []string{"UC_DEFAULT:videos"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("providerJobKeys() = %v, want %v", got, want)
		}
	})

	t.Run("members still backfill non-explicit registrations", func(t *testing.T) {
		t.Parallel()

		scheduler := ProvideScraperScheduler(
			testMemberDataProvider{
				members: []*domain.Member{
					{ChannelID: "UC_MEMBER"},
					{ChannelID: "UC_GRADUATED", IsGraduated: true},
				},
			},
			logger,
			WithChannelPollerRegistrations(registrations),
		)
		if scheduler == nil {
			t.Fatal("scheduler is nil")
		}

		got := providerJobKeys(t, scheduler)
		want := []string{"UC_MEMBER:videos"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("providerJobKeys() = %v, want %v", got, want)
		}
	})
}

func TestProvideScraperScheduler_MixedRegistrationsKeepExplicitJobsWithoutDefaultsOrMembers(t *testing.T) {
	t.Parallel()

	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(namedNoopPoller{name: "videos"}, 0, 15*time.Minute).WithChannelIDs([]string{"UC_EXPLICIT"}),
			NewChannelPollerRegistration(namedNoopPoller{name: "community"}, 0, 10*time.Minute),
		}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	got := providerJobKeys(t, scheduler)
	want := []string{"UC_EXPLICIT:videos"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providerJobKeys() = %v, want %v", got, want)
	}
}

func TestProvideScraperScheduler_RespectsExplicitEmptyChannelIDs(t *testing.T) {
	t.Parallel()

	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(namedNoopPoller{name: "videos"}, 0, 15*time.Minute).WithChannelIDs(nil),
			NewChannelPollerRegistration(namedNoopPoller{name: "stats"}, 0, time.Hour).WithChannelIDs([]string{"UC_A"}),
		}),
		WithSchedulerChannelIDs([]string{"UC_DEFAULT"}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	got := providerJobKeys(t, scheduler)
	want := []string{"UC_A:stats"}
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
