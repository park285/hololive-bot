package collectorruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/park285/shared-go/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type supersededLease struct {
	reason      joblease.ReleaseReason
	releaseCall int
	contextErr  error
}

func (l *supersededLease) Proof() contract.LeaseProof            { return contract.LeaseProof{} }
func (l *supersededLease) Renew(context.Context) error           { return nil }
func (l *supersededLease) CompleteCurrent(context.Context) error { return nil }
func (l *supersededLease) Defer(context.Context, time.Time, string, string, string) error {
	return nil
}
func (l *supersededLease) Release(ctx context.Context, reason joblease.ReleaseReason) error {
	l.releaseCall++
	l.reason = reason
	l.contextErr = ctx.Err()
	return nil
}

func TestSupersededReleaseUsesDetachedCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease := &supersededLease{}
	collector := settings.DefaultYouTubeCollectorConfig()
	collector.CleanupTimeout = time.Second
	scheduler := &leaseScheduler{collector: collector}
	if err := scheduler.releaseSuperseded(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if lease.releaseCall != 1 || lease.reason != joblease.ReleaseSuperseded || lease.contextErr != nil {
		t.Fatalf("release = calls:%d reason:%q context:%v", lease.releaseCall, lease.reason, lease.contextErr)
	}
}

func TestExpectedProjectionChurnUsesSupersededAttemptAndPublishMetrics(t *testing.T) {
	t.Parallel()
	for name, sourceErr := range map[string]error{
		"projection stale": sourceobservation.ErrProjectionStale,
		"target disabled":  sourceobservation.ErrTargetDisabled,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := collecterr.Wrap(collecterr.PublishRejected, collecterr.ClassTransient, fmt.Errorf("publish fence: %w", sourceErr))
			if got := attemptResult(err); got != resultSuperseded {
				t.Fatalf("attemptResult() = %q, want %q", got, resultSuperseded)
			}
			if !ignoreRunError(err) {
				t.Fatal("expected projection churn to remain ignored by run completion")
			}

			registerer := prometheus.NewPedanticRegistry()
			metrics := NewMetrics(registerer)
			metrics.ObserveAttempt(contract.ProviderYouTubeJS, "community_collect", attemptResult(err), time.Second)
			scheduler := &leaseScheduler{metrics: metrics}
			scheduler.observePublishError(
				&joblease.JobSpec{Provider: contract.ProviderYouTubeJS, CollectionJobKind: "community_collect"},
				testRunOutput(t, []contract.Envelope{{
					Provider:        contract.ProviderYouTubeJS,
					ObservationKind: contract.KindCommunityPage,
				}}),
				err,
			)

			if got := metricValue(t, registerer, "youtube_collection_attempts_total", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": "community_collect", "result": resultSuperseded,
			}); got != 1 {
				t.Fatalf("superseded attempt count = %v, want 1", got)
			}
			if got := histogramCount(t, registerer, "youtube_collection_duration_seconds", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": "community_collect",
			}); got != 1 {
				t.Fatalf("attempt duration count = %d, want 1", got)
			}
			if got := metricValue(t, registerer, "youtube_observation_publish_total", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": string(contract.KindCommunityPage), "outcome": outcomeSuperseded,
			}); got != 1 {
				t.Fatalf("superseded publish count = %v, want 1", got)
			}
			if got := metricValue(t, registerer, "youtube_observation_publish_total", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": string(contract.KindCommunityPage), "outcome": outcomeRejected,
			}); got != 0 {
				t.Fatalf("rejected publish count = %v, want 0", got)
			}
		})
	}
}

func TestUnknownPublishErrorRemainsFailedAndRejected(t *testing.T) {
	t.Parallel()
	err := collecterr.Wrap(collecterr.PublishRejected, collecterr.ClassTransient, errors.New("database unavailable"))
	if got := attemptResult(err); got != resultFailed {
		t.Fatalf("attemptResult() = %q, want %q", got, resultFailed)
	}
	if ignoreRunError(err) {
		t.Fatal("unexpected publish error was ignored")
	}

	registerer := prometheus.NewPedanticRegistry()
	metrics := NewMetrics(registerer)
	metrics.ObserveAttempt(contract.ProviderYouTubeJS, "community_collect", attemptResult(err), time.Second)
	scheduler := &leaseScheduler{metrics: metrics}
	scheduler.observePublishError(
		&joblease.JobSpec{Provider: contract.ProviderYouTubeJS, CollectionJobKind: "community_collect"},
		testRunOutput(t, []contract.Envelope{{
			Provider:        contract.ProviderYouTubeJS,
			ObservationKind: contract.KindCommunityPage,
		}}),
		err,
	)

	if got := metricValue(t, registerer, "youtube_collection_attempts_total", map[string]string{
		"provider": string(contract.ProviderYouTubeJS), "kind": "community_collect", "result": resultFailed,
	}); got != 1 {
		t.Fatalf("failed attempt count = %v, want 1", got)
	}
	if got := metricValue(t, registerer, "youtube_observation_publish_total", map[string]string{
		"provider": string(contract.ProviderYouTubeJS), "kind": string(contract.KindCommunityPage), "outcome": outcomeRejected,
	}); got != 1 {
		t.Fatalf("rejected publish count = %v, want 1", got)
	}
}

func testRunOutput(t *testing.T, envelopes []contract.Envelope) collectutil.RunOutput {
	t.Helper()
	output, err := collectutil.OutputFromEnvelopes(envelopes, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestPublishedOutcomeMapsExactOrdinals(t *testing.T) {
	t.Parallel()
	result := sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
		sourceobservation.NewPublishedObservation(11, sourceobservation.PublishInserted, 0),
		sourceobservation.NewPublishedObservation(12, sourceobservation.PublishDuplicate, 1),
		sourceobservation.NewPublishedObservation(13, sourceobservation.PublishCollision, 2),
	}}
	if got, ok := publishedOutcome(result, 0); !ok || got != outcomeInserted {
		t.Fatalf("inserted = %q ok=%v", got, ok)
	}
	if got, ok := publishedOutcome(result, 1); !ok || got != outcomeDuplicate {
		t.Fatalf("duplicate = %q ok=%v", got, ok)
	}
	if got, ok := publishedOutcome(result, 2); !ok || got != outcomeCollision {
		t.Fatalf("collision = %q ok=%v", got, ok)
	}
}

func TestPublishedOutcomeRejectsMissingRowAndUnknownOutcome(t *testing.T) {
	t.Parallel()
	result := sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
		sourceobservation.NewPublishedObservation(11, sourceobservation.PublishInserted, 0),
	}}
	if got, ok := publishedOutcome(result, 1); ok || got != "" {
		t.Fatalf("missing row = %q ok=%v", got, ok)
	}
	result.Results[0].Outcome = "WEIRD"
	if got, ok := publishedOutcome(result, 0); ok || got != "" {
		t.Fatalf("unknown outcome = %q ok=%v", got, ok)
	}
}

func TestAttemptFailureResultMapsCanonicalCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "timeout", err: collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "timeout"), want: resultTimeout},
		{name: "canceled", err: collecterr.New(collecterr.Canceled, collecterr.ClassCanceled, "canceled"), want: resultCanceled},
		{name: "parser drift", err: collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "drift"), want: resultParserDrift},
		{name: "failed", err: collecterr.New(collecterr.Failed, collecterr.ClassTransient, "failed"), want: resultFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := attemptFailureResult(test.err); got != test.want {
				t.Fatalf("attemptFailureResult() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLogFailureUsesStructuredSecretSafeDiagnostics(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	scheduler := &leaseScheduler{logger: slog.New(slog.NewJSONHandler(&output, nil))}
	spec := &joblease.JobSpec{JobKey: "job", Provider: contract.ProviderYouTubeJS, CollectionJobKind: "community_collect", SubjectKey: "UC_TEST"}
	scheduler.logFailure("collect", string(collecterr.Failed), "HelperError", "token=secret-value", spec, &contract.LeaseProof{})
	if strings.Contains(output.String(), "secret-value") {
		t.Fatalf("structured log leaked diagnostic credential: %s", output.String())
	}
	if !strings.Contains(output.String(), `"error_class":"HelperError"`) || !strings.Contains(output.String(), `"error_detail"`) {
		t.Fatalf("structured log omitted diagnostics: %s", output.String())
	}
}

func metricValue(t *testing.T, registerer prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := registerer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			if !metricLabelsMatch(metric.GetLabel(), labels) {
				continue
			}
			return metric.GetCounter().GetValue()
		}
	}
	return 0
}

func histogramCount(t *testing.T, registerer prometheus.Gatherer, name string, labels map[string]string) uint64 {
	t.Helper()
	families, err := registerer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			if !metricLabelsMatch(metric.GetLabel(), labels) {
				continue
			}
			return metric.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func metricLabelsMatch(labels []*dto.LabelPair, want map[string]string) bool {
	if len(labels) != len(want) {
		return false
	}
	for _, label := range labels {
		if want[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}

func TestNewCollectionExecutorMapsEverySchedulerField(t *testing.T) {
	t.Parallel()
	scheduler := newLifecycleScheduler(t)
	scheduler.publisher = NewPublisher(nil)
	scheduler.owner = "owner-test"
	scheduler.gates = newProviderGates(&scheduler.collector)
	scheduler.workerTracker = workercontract.NewExecutorTracker()
	scheduler.workerTotals = &workercontract.Counters{}

	executor := newCollectionExecutor(scheduler)

	executorValue := reflect.ValueOf(executor).Elem()
	schedulerValue := reflect.ValueOf(scheduler).Elem()
	for i := range executorValue.NumField() {
		field := executorValue.Type().Field(i)
		got := executorValue.Field(i)
		if got.IsZero() {
			t.Fatalf("executor.%s = zero, want mapped from scheduler", field.Name)
		}
		if field.Type.Kind() == reflect.Func {
			continue
		}
		assertExecutorFieldMirrorsScheduler(t, &field, got, schedulerValue.FieldByName(field.Name))
	}

	boom := errors.New("boom")
	executor.reportFatal(boom)
	select {
	case err := <-scheduler.Fatal():
		if !errors.Is(err, boom) {
			t.Fatalf("fatal = %v, want wrapped %v", err, boom)
		}
	default:
		t.Fatal("executor.reportFatal is not bound to the scheduler fatal channel")
	}
}

func assertExecutorFieldMirrorsScheduler(t *testing.T, field *reflect.StructField, got, want reflect.Value) {
	t.Helper()
	if !want.IsValid() {
		t.Fatalf("executor.%s has no scheduler field of the same name", field.Name)
	}
	if want.IsZero() {
		t.Fatalf("scheduler.%s = zero, test fixture must populate every mapped field", field.Name)
	}
	switch {
	case field.Type.Kind() == reflect.Pointer || field.Type.Kind() == reflect.Map:
		if got.Pointer() != want.Pointer() {
			t.Fatalf("executor.%s points to a different instance than scheduler.%s", field.Name, field.Name)
		}
	case field.Type.Comparable():
		if !got.Equal(want) {
			t.Fatalf("executor.%s = %v, want scheduler value %v", field.Name, got, want)
		}
	default:
		t.Fatalf("executor.%s has unsupported kind %s, extend the mapping assertion", field.Name, field.Type.Kind())
	}
}

type recordingLease struct {
	deferCalls int
	deferCode  string
	deferClass string
}

func (l *recordingLease) Proof() contract.LeaseProof                            { return contract.LeaseProof{} }
func (l *recordingLease) Renew(context.Context) error                           { return nil }
func (l *recordingLease) CompleteCurrent(context.Context) error                 { return nil }
func (l *recordingLease) Release(context.Context, joblease.ReleaseReason) error { return nil }

func (l *recordingLease) Defer(_ context.Context, _ time.Time, code, class, _ string) error {
	l.deferCalls++
	l.deferCode = code
	l.deferClass = class
	return nil
}

func newRunErrorExecutor(fatal *[]error) *collectionExecutor {
	collector := settings.DefaultYouTubeCollectorConfig()
	collector.CleanupTimeout = time.Second
	return &collectionExecutor{
		logger:    slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		metrics:   NewMetrics(prometheus.NewPedanticRegistry()),
		collector: collector,
		config: joblease.Config{
			MinRetryDelay: time.Minute,
			MaxRetryDelay: 10 * time.Minute,
		},
		reportFatal: func(err error) { *fatal = append(*fatal, err) },
	}
}

type handleRunErrorCase struct {
	name      string
	err       error
	wantFatal bool
	wantCode  collecterr.ErrorCode
	wantClass collecterr.FailureClass
}

func assertHandleRunErrorCase(t *testing.T, test handleRunErrorCase) {
	t.Helper()

	var fatal []error

	executor := newRunErrorExecutor(&fatal)
	lease := &recordingLease{}
	spec := &joblease.JobSpec{
		JobKey:            "job",
		Provider:          contract.ProviderYouTubeJS,
		CollectionJobKind: "community_collect",
		SubjectKey:        "UC_TEST",
	}

	executor.handleRunError(context.Background(), lease, spec, &contract.LeaseProof{}, test.err)

	if lease.deferCalls != 1 {
		t.Fatalf("lease.Defer calls = %d, want 1", lease.deferCalls)
	}

	if lease.deferCode != string(test.wantCode) || lease.deferClass != string(test.wantClass) {
		t.Fatalf("deferred diagnostic = %q/%q, want %q/%q",
			lease.deferCode, lease.deferClass, test.wantCode, test.wantClass)
	}

	if !test.wantFatal {
		if len(fatal) != 0 {
			t.Fatalf("retryable error was promoted to fatal: %v", fatal)
		}

		return
	}

	if len(fatal) != 1 {
		t.Fatalf("fatal reports = %d, want 1", len(fatal))
	}

	var runtimeErr *FatalRuntimeError
	if !errors.As(fatal[0], &runtimeErr) || runtimeErr.Phase != "collection" {
		t.Fatalf("fatal report = %#v, want collection phase FatalRuntimeError", fatal[0])
	}

	if !errors.Is(fatal[0], test.err) {
		t.Fatalf("fatal report does not wrap the original error: %v", fatal[0])
	}
}

func TestHandleRunErrorPromotesOnlyClassifiedFatalErrors(t *testing.T) {
	t.Parallel()

	tests := []handleRunErrorCase{
		{
			name:      "unclassified database failure",
			err:       errors.New("acquire connection: connection reset by peer"),
			wantFatal: false,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "unclassified deadlock",
			err:       fmt.Errorf("load target snapshot: %w", errors.New("SQLSTATE 40P01 deadlock detected")),
			wantFatal: false,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "unclassified maintenance page body",
			err:       fmt.Errorf("decode response: %w", errors.New("invalid character '<' looking for beginning of value")),
			wantFatal: false,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "helper read deadline through FromContext",
			err:       collecterr.FromContext(fmt.Errorf("read youtube.js helper: %w", os.ErrDeadlineExceeded)),
			wantFatal: false,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "provider body read failure through FromContext",
			err:       collecterr.FromContext(fmt.Errorf("read holodex: %w", syscall.ECONNREFUSED)),
			wantFatal: false,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "classified transient failure",
			err:       collecterr.Wrap(collecterr.Failed, collecterr.ClassTransient, errors.New("upstream unavailable")),
			wantFatal: false,
			wantCode:  collecterr.Failed,
			wantClass: collecterr.ClassTransient,
		},
		{
			name:      "classified internal invariant",
			err:       collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failure is missing"),
			wantFatal: true,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "wrapped classified internal invariant",
			err:       fmt.Errorf("collect and publish: %w", collecterr.New(collecterr.Internal, collecterr.ClassInternal, "invariant violated")),
			wantFatal: true,
			wantCode:  collecterr.Internal,
			wantClass: collecterr.ClassInternal,
		},
		{
			name:      "classified helper protocol mismatch",
			err:       collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "helper protocol mismatch"),
			wantFatal: true,
			wantCode:  collecterr.HelperProtocolMismatch,
			wantClass: collecterr.ClassProtocol,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertHandleRunErrorCase(t, test)
		})
	}
}
