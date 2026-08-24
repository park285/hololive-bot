package sourceobservation

import (
	"reflect"
	"slices"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestAPI002JobContractGettersAreDefensiveCopies(t *testing.T) {
	t.Parallel()

	job := mustLookupJob(t, contract.ProviderYouTubeJS, "youtubejs_content")
	emissions := job.Emissions()
	cadence := job.CadenceKinds()
	roster := job.RosterKinds()
	requested := job.RequestedKinds()

	if len(emissions) == 0 || len(cadence) == 0 || len(requested) == 0 {
		t.Fatal("youtubejs_content contract returned empty required kinds")
	}

	emissions[0] = testMutatedValue
	cadence[0] = testMutatedValue

	if len(roster) > 0 {
		roster[0] = testMutatedValue
	}

	requested[0] = testMutatedValue

	freshEmissions := job.Emissions()
	freshCadence := job.CadenceKinds()
	freshRequested := job.RequestedKinds()

	if len(freshEmissions) == 0 || len(freshCadence) == 0 || len(freshRequested) == 0 {
		t.Fatal("youtubejs_content contract returned empty defensive copies")
	}

	if freshEmissions[0] == testMutatedValue || freshCadence[0] == testMutatedValue || freshRequested[0] == testMutatedValue {
		t.Fatal("JobContract getter mutation leaked into contract")
	}

	clone := job.Clone()
	if !reflect.DeepEqual(clone.Emissions(), job.Emissions()) {
		t.Fatal("Clone emissions mismatch")
	}
}

func TestAPI007JobContractSetDefinitionUsesJobID(t *testing.T) {
	t.Parallel()

	var set JobContractSet = InitialJobContracts()

	id := JobID{Provider: contract.ProviderHolodex, Kind: "holodex_live"}
	job, ok := set.Definition(id)

	if !ok || job.ID() != id {
		t.Fatalf("Definition(%s) = %#v ok=%t", id, job, ok)
	}

	if !set.Allows(id, contract.KindLiveSnapshot) || set.Allows(id, contract.KindCommunityPage) {
		t.Fatal("Allows mismatch")
	}

	if got := set.IDs(); len(got) != 9 {
		t.Fatalf("IDs() = %d", len(got))
	}
}

func TestAPI008DeferCollectionInputHasNoPublicMutableSurface(t *testing.T) {
	t.Parallel()

	diagnostic, err := contract.NewFailureDiagnostic(contract.ErrorCooldown, contract.ClassCooldown, "retry later")
	if err != nil {
		t.Fatal(err)
	}

	schedule, err := NewRetryDelaySchedule(1500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	input, err := NewDeferCollectionInput(diagnostic, RetryBounds{Minimum: time.Second, Maximum: time.Minute}, schedule)
	if err != nil {
		t.Fatal(err)
	}

	bounds := input.Bounds()

	bounds.Minimum = time.Hour

	got := input.Schedule()

	if bounds.Minimum != time.Hour || input.Bounds().Minimum != time.Second || got.Delay() != 1500*time.Millisecond {
		t.Fatal("DeferCollectionInput getter mutation leaked")
	}

	if input.Diagnostic().Detail() != "retry later" {
		t.Fatal("diagnostic getter changed")
	}
}

type jobContractFixture struct {
	id           JobID
	class        JobClass
	membership   JobMembership
	leaseSubject string
	emissions    []contract.ObservationKind
	cadence      []contract.ObservationKind
	roster       []contract.ObservationKind
}

func subjectJobContractFixtures() []jobContractFixture {
	return []jobContractFixture{
		{
			mustJobID(contract.ProviderYouTubeJS, "community_collect"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindCommunityPage},
			[]contract.ObservationKind{contract.KindCommunityPage},
			nil,
		},
		{
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_content"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindShortsList, contract.KindVideoList},
			[]contract.ObservationKind{contract.KindShortsList, contract.KindVideoList},
			nil,
		},
		{
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_live"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindLiveSnapshot},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
			nil,
		},
		{
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_metadata"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindChannelPhoto, contract.KindChannelProfile, contract.KindChannelStats},
			[]contract.ObservationKind{contract.KindChannelPhoto, contract.KindChannelProfile, contract.KindChannelStats},
			nil,
		},
		{
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_viewer"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindViewerSample},
			[]contract.ObservationKind{contract.KindViewerSample},
			nil,
		},
	}
}

func globalJobContractFixtures() []jobContractFixture {
	return []jobContractFixture{
		{
			mustJobID(contract.ProviderHolodex, "holodex_live"),
			JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_live",
			[]contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
			[]contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
		},
		{
			mustJobID(contract.ProviderHolodex, "holodex_metadata"),
			JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_metadata",
			[]contract.ObservationKind{contract.KindChannelPhoto, contract.KindChannelStats},
			[]contract.ObservationKind{contract.KindChannelPhoto, contract.KindChannelStats},
			[]contract.ObservationKind{contract.KindChannelPhoto, contract.KindChannelStats},
		},
		{
			mustJobID(contract.ProviderHolodex, "holodex_schedule"),
			JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_schedule",
			[]contract.ObservationKind{contract.KindSchedule},
			[]contract.ObservationKind{contract.KindSchedule},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
		},
		{
			mustJobID(contract.ProviderHololiveOfficial, "official_schedule"),
			JobClassGlobal, JobMembershipExactSubject, "global:hololive-schedule",
			[]contract.ObservationKind{contract.KindSchedule},
			[]contract.ObservationKind{contract.KindSchedule},
			nil,
		},
	}
}

func requireJobContract(t *testing.T, got StaticJobContracts, fixture jobContractFixture) {
	t.Helper()

	job, ok := got.Definition(fixture.id)
	if !ok {
		t.Fatalf("missing %s", fixture.id)
	}

	if job.Class() != fixture.class || job.Membership() != fixture.membership || job.LeaseSubject() != fixture.leaseSubject {
		t.Fatalf("%s class/membership/subject = %s/%s/%q", fixture.id, job.Class(), job.Membership(), job.LeaseSubject())
	}

	if !slices.Equal(job.Emissions(), fixture.emissions) {
		t.Fatalf("%s emissions = %#v, want %#v", fixture.id, job.Emissions(), fixture.emissions)
	}

	if !slices.Equal(job.CadenceKinds(), fixture.cadence) {
		t.Fatalf("%s cadence = %#v, want %#v", fixture.id, job.CadenceKinds(), fixture.cadence)
	}

	if !slices.Equal(emptyNil(job.RosterKinds()), emptyNil(fixture.roster)) {
		t.Fatalf("%s roster = %#v, want %#v", fixture.id, job.RosterKinds(), fixture.roster)
	}
}

func TestInitialJobContractsExactTable(t *testing.T) {
	t.Parallel()

	got := InitialJobContracts()
	want := slices.Concat(subjectJobContractFixtures(), globalJobContractFixtures())

	if len(got) != len(want) {
		t.Fatalf("InitialJobContracts size = %d, want %d", len(got), len(want))
	}

	for _, fixture := range want {
		requireJobContract(t, got, fixture)
	}
}

func TestLookupByJobKindOneReleaseAdapter(t *testing.T) {
	t.Parallel()

	job, ok := InitialJobContracts().LookupByJobKind("official_schedule")
	if !ok || job.ID().Provider != contract.ProviderHololiveOfficial {
		t.Fatalf("LookupByJobKind = %#v ok=%t", job, ok)
	}

	if _, ok := InitialJobContracts().LookupByJobKind("missing"); ok {
		t.Fatal("missing kind must fail closed")
	}
}

func (s StaticJobContracts) LookupByJobKind(jobKind string) (JobContract, bool) {
	var found JobContract

	matches := 0

	for id, job := range s {
		if string(id.Kind) != jobKind {
			continue
		}

		found = job.Clone()
		matches++
	}

	if matches != 1 {
		return JobContract{}, false
	}

	return found, true
}

func TestNewJobContractRejectsDuplicatesAndEmissionOutsideCadence(t *testing.T) {
	t.Parallel()

	id := mustJobID(contract.ProviderYouTubeJS, "community_collect")
	if _, err := NewJobContract(
		id, JobClassSubject, JobMembershipExactSubject, "",
		[]contract.ObservationKind{contract.KindCommunityPage, contract.KindCommunityPage},
		[]contract.ObservationKind{contract.KindCommunityPage},
		nil,
	); err == nil {
		t.Fatal("duplicate emission must fail")
	}

	if _, err := NewJobContract(
		id, JobClassSubject, JobMembershipExactSubject, "",
		[]contract.ObservationKind{contract.KindCommunityPage},
		[]contract.ObservationKind{contract.KindLiveSnapshot},
		nil,
	); err == nil {
		t.Fatal("emission outside cadence must fail")
	}
}

func TestPublishedObservationOrdinalDefaultsAndConstructor(t *testing.T) {
	t.Parallel()

	var zero PublishedObservation

	if zero.Ordinal != 0 {
		t.Fatalf("zero ordinal = %d", zero.Ordinal)
	}

	got := NewPublishedObservation(9, PublishInserted, 3)
	if got.ObservationID != 9 || got.Outcome != PublishInserted || got.Ordinal != 3 {
		t.Fatalf("NewPublishedObservation = %#v", got)
	}
}

func mustLookupJob(t *testing.T, provider contract.Provider, kind string) JobContract {
	t.Helper()

	job, ok := InitialJobContracts().Definition(JobID{Provider: provider, Kind: JobKind(kind)})
	if !ok {
		t.Fatalf("missing job %s/%s", provider, kind)
	}

	return job
}

func emptyNil(kinds []contract.ObservationKind) []contract.ObservationKind {
	if len(kinds) == 0 {
		return nil
	}

	return kinds
}
