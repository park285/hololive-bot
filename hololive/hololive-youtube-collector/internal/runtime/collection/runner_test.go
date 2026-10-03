package collection

import (
	"context"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestOutputAcceptsHololiveSizedBatch(t *testing.T) {
	t.Parallel()

	const hololiveSized = 90*4 + 1

	if hololiveSized > MaxPublishBatchSize {
		t.Fatalf("publish limit %d cannot hold a Hololive-sized Holodex batch %d", MaxPublishBatchSize, hololiveSized)
	}

	envelopes := make([]contract.Envelope, hololiveSized)

	output, err := OutputFromEnvelopes(envelopes, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if got := len(output.Observations()); got != hololiveSized || output.ObservationCount() != hololiveSized {
		t.Fatalf("observations = %d/%d", got, output.ObservationCount())
	}
}

func TestOutputRejectsBatchAbovePublishLimit(t *testing.T) {
	t.Parallel()

	envelopes := make([]contract.Envelope, MaxPublishBatchSize+1)
	if _, err := OutputFromEnvelopes(envelopes, time.Now()); err == nil {
		t.Fatal("expected publish limit rejection")
	}
}

func TestRunOutputDefensivelyClonesPayload(t *testing.T) {
	t.Parallel()

	envelopes := []contract.Envelope{{Payload: []byte(`{"value":1}`)}}

	output, err := NewRunOutput(envelopes, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	envelopes[0].Payload[0] = 'x'

	gotEnvelopes := output.Observations()

	gotEnvelopes[0].Payload[0] = 'y'

	if string(output.Observations()[0].Payload) != `{"value":1}` {
		t.Fatal("RunOutput mutable bytes escaped")
	}
}

func TestPartialResultRejectsFatalOnlyClasses(t *testing.T) {
	t.Parallel()

	output, err := NewRunOutput([]contract.Envelope{{ObservationKind: contract.KindVideoList}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	for _, cause := range []error{
		context.Canceled,
		collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "protocol"),
		collecterr.New(collecterr.Internal, collecterr.ClassInternal, "internal"),
	} {
		if result, resultErr := NewPartialResult(output, cause, contract.KindShortsList); resultErr == nil || !result.IsZero() {
			t.Fatalf("fatal-only cause accepted as PARTIAL: %v", cause)
		}
	}
}

type runInputCase struct {
	name     string
	job      JobContract
	subject  string
	lease    contract.LeaseProof
	targets  TargetSnapshot
	maxPages int
	accepted bool
}

func TestNewRunInputBindsJobSubjectLeaseAndTargets(t *testing.T) {
	t.Parallel()

	for _, test := range runInputCases(t) {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			snapshot, err := NewContractSnapshot(test.job.Emissions(), contractGenerations(test.job.Emissions()))
			if err != nil {
				t.Fatal(err)
			}

			input, err := NewRunInput(test.job, test.subject, &test.lease, snapshot, test.targets, test.maxPages, 1)

			if test.accepted {
				if err != nil || input.Subject() != test.subject || input.Job().ID() != test.job.ID() {
					t.Fatalf("valid input = %q/%s, %v", input.Subject(), input.Job().ID(), err)
				}

				return
			}

			if err == nil || collecterr.CodeOf(err) != collecterr.Internal || collecterr.ClassOf(err) != collecterr.ClassInternal {
				t.Fatalf("mismatched input error = %v", err)
			}

			if input.Subject() != "" || input.Job().ID() != (JobID{}) {
				t.Fatal("rejected input returned a populated value")
			}
		})
	}
}

func runInputCases(t *testing.T) []runInputCase {
	t.Helper()

	content := mustLookupJob(t, contract.ProviderYouTubeJS, "youtubejs_content")
	official := mustLookupJob(t, contract.ProviderHololiveOfficial, "official_schedule")
	holodex := mustLookupJob(t, contract.ProviderHolodex, "holodex_live")
	contentLease := contract.LeaseProof{CollectionJobKind: "youtubejs_content", ProjectionGeneration: 3}
	officialLease := contract.LeaseProof{CollectionJobKind: "official_schedule", ProjectionGeneration: 3}
	holodexLease := contract.LeaseProof{CollectionJobKind: "holodex_live", ProjectionGeneration: 3}
	contentTargets := mustExactTargets(t, 3, subjectUCA, content)
	officialTargets := mustExactTargets(t, 3, official.LeaseSubject(), official)
	holodexTargets := mustProjectionTargets(t, 3, holodex)

	return []runInputCase{
		{"exact subject", content, subjectUCA, contentLease, contentTargets, 1, true},
		{"global exact subject", official, official.LeaseSubject(), officialLease, officialTargets, 1, true},
		{"global projection subject", holodex, holodex.LeaseSubject(), holodexLease, holodexTargets, 1, true},
		{"exact subject mismatch", content, subjectUCB, contentLease, contentTargets, 1, false},
		{"global exact subject mismatch", official, subjectUCA, officialLease, officialTargets, 1, false},
		{"global projection subject mismatch", holodex, subjectUCA, holodexLease, holodexTargets, 1, false},
		{"lease kind mismatch", content, subjectUCA, officialLease, contentTargets, 1, false},
		{"lease generation mismatch", content, subjectUCA, contract.LeaseProof{CollectionJobKind: "youtubejs_content", ProjectionGeneration: 4}, contentTargets, 1, false},
		{"membership mismatch", holodex, holodex.LeaseSubject(), holodexLease, mustExactTargets(t, 3, holodex.LeaseSubject(), holodex), 1, false},
		{"page limit", content, subjectUCA, contentLease, contentTargets, 0, false},
	}
}

func mustExactTargets(t *testing.T, generation int64, subject string, job JobContract) TargetSnapshot {
	t.Helper()

	targets, err := NewExactTargetSnapshot(generation, subject, job.RequestedKinds(), nil)
	if err != nil {
		t.Fatal(err)
	}

	return targets
}

func mustProjectionTargets(t *testing.T, generation int64, job JobContract) TargetSnapshot {
	t.Helper()

	rows := make(map[contract.ObservationKind][]string)

	for _, kind := range job.RequestedKinds() {
		rows[kind] = []string{subjectUCA}
	}

	targets, err := NewProjectionTargetSnapshot(generation, job.RequestedKinds(), rows, 100)
	if err != nil {
		t.Fatal(err)
	}

	return targets
}

func contractGenerations(kinds []contract.ObservationKind) map[contract.ObservationKind]int64 {
	generations := make(map[contract.ObservationKind]int64, len(kinds))
	for _, kind := range kinds {
		generations[kind] = 1
	}

	return generations
}
