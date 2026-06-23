package providers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/livestatus"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/stretchr/testify/assert"
)

type noopPoller struct{}

func (noopPoller) Poll(context.Context, string) error { return nil }
func (noopPoller) Name() string                       { return "noop" }

type namedNoopPoller struct {
	name string
}

func (p namedNoopPoller) Poll(context.Context, string) error { return nil }
func (p namedNoopPoller) Name() string                       { return p.name }

type providerBudgetLimiterStub struct {
	observations chan providerBudgetObservation
}

func (s providerBudgetLimiterStub) TryReserve(
	ctx context.Context,
	job *poller.BudgetJob,
	profile poller.BudgetProfile,
	ttl time.Duration,
) (result0 poller.BudgetReservation, result1 poller.BudgetDecision, err error) {
	_ = ttl
	if s.observations != nil {
		observation := providerBudgetObservation{profile: profile}
		if job != nil {
			observation.job = *job
		}
		if deadline, ok := ctx.Deadline(); ok {
			observation.deadline = time.Until(deadline)
		}
		select {
		case s.observations <- observation:
		default:
		}
	}
	return nil, poller.BudgetDecision{Allowed: true}, nil
}

type providerBudgetObservation struct {
	job      poller.BudgetJob
	profile  poller.BudgetProfile
	deadline time.Duration
}

type providerRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f providerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestProvideHolodexServiceWithConfigPassesLiveStatusFallbackConfig(t *testing.T) {
	t.Parallel()

	holodexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/live" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(holodexServer.Close)

	var youtubeRequests atomic.Int32
	youtubeClient := scraper.NewClient(
		scraper.WithRateLimiter(scraper.NewRateLimiter(0)),
		scraper.WithHTTPClient(&http.Client{
			Transport: providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
				youtubeRequests.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<html></html>")),
				}, nil
			}),
		}),
	)
	scraperService := ProvideScraperServiceWithYouTubeProducer(cachemocks.NewLenientClient(), nil, youtubeClient, slog.New(slog.NewTextHandler(io.Discard, nil)))
	holodexCfg := config.DefaultHolodexOperationalConfig()
	holodexCfg.BaseURL = holodexServer.URL
	holodexCfg.APIKey = "test-key"
	holodexCfg.DistributedRateLimit.Enabled = false
	holodexCfg.LiveStatusFallback = config.HolodexLiveStatusFallbackConfig{
		MaxPerCycle:     1,
		WallClockBudget: time.Second,
		DeadlineMargin:  0,
	}

	service, err := ProvideHolodexServiceWithConfig(&holodexCfg, cachemocks.NewLenientClient(), scraperService, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("ProvideHolodexServiceWithConfig() error = %v", err)
	}

	_, failed, err := service.GetChannelsLiveStatusWithFailures(context.Background(), []string{"c1", "c2"})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatusWithFailures() error = %v, want nil", err)
	}
	if got := youtubeRequests.Load(); got != 1 {
		t.Fatalf("youtube requests = %d, want 1 from configured MaxPerCycle", got)
	}
	if got := livestatus.ReasonOf(failed["c2"]); got != livestatus.DeferredReasonPerCycleCap {
		t.Fatalf("failed[c2] reason = %q, want %q", got, livestatus.DeferredReasonPerCycleCap)
	}
}

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

func TestNewGlobalPollerRegistration_UsesSyntheticGlobalTarget(t *testing.T) {
	t.Parallel()

	registration := NewGlobalPollerRegistration(namedNoopPoller{name: "resolver"}, poller.PriorityLow, 15*time.Second)

	assert.Equal(t, poller.PriorityLow, registration.Priority)
	assert.Equal(t, 15*time.Second, registration.Interval)
	assert.Equal(t, ChannelTargetGroupGlobal, registration.TargetGroup)
	assert.Equal(t, []string{SyntheticGlobalPollerChannelID}, registration.ChannelIDs)
	assert.True(t, registration.HasExplicitChannelIDs)
}

func TestProvideScraperScheduler_RegistersSyntheticGlobalPollerWithoutDefaults(t *testing.T) {
	t.Parallel()

	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewGlobalPollerRegistration(namedNoopPoller{name: "resolver"}, poller.PriorityLow, 15*time.Second),
		}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	got := providerJobKeys(t, scheduler)
	want := []string{SyntheticGlobalPollerChannelID + ":resolver"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providerJobKeys() = %v, want %v", got, want)
	}
}

func TestNewScraperSchedulerMapsBudgetOptions(t *testing.T) {
	t.Parallel()

	observations := make(chan providerBudgetObservation, 1)
	limiter := providerBudgetLimiterStub{observations: observations}
	budgetContext := poller.BudgetContext{Namespace: "production", InstanceID: "ap-a", Enabled: true}
	resolvedOptions := resolveScraperSchedulerOptions(
		WithSchedulerBudgetLimiter(limiter),
		WithSchedulerBudgetContext(budgetContext),
		WithSchedulerBudgetAcquireTimeout(250*time.Millisecond),
	)
	scheduler := newScraperScheduler(
		&resolvedOptions,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	requireBudgetProfile := poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{poller.BudgetSourceYouTubeScraper: 1},
		BurstClass:  poller.BudgetBurstPrimary,
		Priority:    poller.BudgetPriorityNormal,
	}
	if err := scheduler.RegisterCheckedWithBudgetProfile("UC_A", namedNoopPoller{name: "videos"}, poller.PriorityNormal, time.Nanosecond, requireBudgetProfile); err != nil {
		t.Fatalf("register budget profile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	scheduler.Start(ctx)
	defer scheduler.Stop()

	select {
	case got := <-observations:
		assert.Equal(t, budgetContext.Namespace, got.job.Namespace)
		assert.Equal(t, budgetContext.InstanceID, got.job.InstanceID)
		assert.Equal(t, "UC_A:videos", got.job.JobKey)
		if got.deadline <= 0 || got.deadline > 250*time.Millisecond {
			t.Fatalf("budget acquire deadline = %s, want within 250ms", got.deadline)
		}
	case <-ctx.Done():
		t.Fatal("budget limiter was not called")
	}
}

func TestProvideScraperSchedulerRegistersBudgetProfiles(t *testing.T) {
	t.Parallel()

	observations := make(chan providerBudgetObservation, 1)
	profile := poller.BudgetProfile{
		SourceUnits: map[poller.BudgetSource]float64{
			poller.BudgetSourceYouTubeScraper: 2,
			poller.BudgetSourcePostgresWrite:  1,
		},
		BurstClass: poller.BudgetBurstPrimary,
		Priority:   poller.BudgetPriorityNormal,
	}
	scheduler := ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(namedNoopPoller{name: "videos"}, poller.PriorityNormal, time.Nanosecond).
				WithChannelIDs([]string{"UC_A"}).
				WithBudgetProfile(profile),
		}),
		WithSchedulerBudgetLimiter(providerBudgetLimiterStub{observations: observations}),
		WithSchedulerBudgetContext(poller.BudgetContext{Namespace: "test", InstanceID: "unit", Enabled: true}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	scheduler.Start(ctx)
	defer scheduler.Stop()

	select {
	case got := <-observations:
		assert.Equal(t, profile, got.profile)
	case <-ctx.Done():
		t.Fatal("budget profile was not passed to limiter")
	}
}

func TestProvideScraperScheduler_SeparatesExpectedRPMFromFaultEnvelope(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	scheduler := ProvideScraperScheduler(
		nil,
		logger,
		WithChannelPollerRegistrations([]ChannelPollerRegistration{
			NewChannelPollerRegistration(namedNoopPoller{name: "shorts"}, poller.PriorityLow, 2*time.Minute).
				WithChannelIDs(repeatProviderChannelIDs("UC_NOTIFY_", 12)).
				WithWorstCaseAttempts(1).
				WithWorstCaseRequestUnitsPerRun(4),
		}),
	)
	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"msg":"Scraper scheduler initialized"`)
	assert.Contains(t, logOutput, `"expected_total_rpm":6`)
	assert.Contains(t, logOutput, `"expected_total_retry_amplified_rpm_max":24`)
	assert.NotContains(t, logOutput, `"msg":"scraper_poll_budget_exceeds_rate_limit"`)
	assert.NotContains(t, logOutput, `"msg":"scraper_poll_fault_envelope_exceeds_rate_limit"`)
}

func TestEstimatedRegistrationRPMUsesRequestsPerRunNotWorstCaseUnits(t *testing.T) {
	t.Parallel()

	registration := NewChannelPollerRegistration(namedNoopPoller{name: "shorts"}, poller.PriorityLow, 2*time.Minute).
		WithChannelIDs([]string{"UC_A", "UC_B"}).
		WithRequestsPerRun(1).
		WithWorstCaseRequestUnitsPerRun(4)

	assert.Equal(t, 1.0, estimatedRegistrationRPM(&registration, 2))
	assert.Equal(t, 4.0, estimatedRegistrationWorstCaseRPM(&registration, 2))
}

func providerJobKeys(t *testing.T, scheduler *poller.Scheduler) []string {
	t.Helper()

	field := providerSchedulerField(t, scheduler, "jobMap")
	keys := make([]string, 0, field.Len())
	iterator := field.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}
	slices.Sort(keys)
	return keys
}

func providerSchedulerField(t *testing.T, scheduler *poller.Scheduler, name string) reflect.Value {
	t.Helper()

	field := reflect.ValueOf(scheduler).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s field must exist", name)
	}

	return field
}

func repeatProviderChannelIDs(prefix string, count int) []string {
	out := make([]string, 0, count)
	for i := range count {
		out = append(out, prefix+strings.Repeat("A", i+1))
	}
	return out
}
