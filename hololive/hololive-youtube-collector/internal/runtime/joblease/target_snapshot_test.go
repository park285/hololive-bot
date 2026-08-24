package joblease

import (
	"errors"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestExactTargetSnapshotPreservesExplicitDisabledAndDefensiveCopies(t *testing.T) {
	requested := []contract.ObservationKind{contract.KindShortsList, contract.KindVideoList}

	snapshot, err := newExactTargetSnapshot(7, subjectUCA, requested, map[contract.ObservationKind]bool{
		contract.KindVideoList: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	requested[0] = contract.KindSchedule

	if snapshot.Generation() != 7 || snapshot.Membership() != sourceobservation.JobMembershipExactSubject {
		t.Fatalf("snapshot identity = %d/%s", snapshot.Generation(), snapshot.Membership())
	}

	kinds := snapshot.RequestedKinds()
	if len(kinds) == 0 {
		t.Fatal("requested kinds are empty")
	}

	kinds[0] = contract.KindSchedule

	if allowed, allowErr := snapshot.Allows(contract.KindVideoList, subjectUCA); allowErr != nil || !allowed {
		t.Fatalf("enabled exact target = %t, %v", allowed, allowErr)
	}

	if allowed, denyErr := snapshot.Allows(contract.KindShortsList, subjectUCA); denyErr != nil || allowed {
		t.Fatalf("disabled exact target = %t, %v", allowed, denyErr)
	}

	roster, err := snapshot.Roster(contract.KindVideoList)
	if err != nil {
		t.Fatal(err)
	}

	if len(roster) == 0 {
		t.Fatal("enabled exact roster is empty")
	}

	roster[0] = "mutated"

	roster, err = snapshot.Roster(contract.KindVideoList)
	if err != nil {
		t.Fatal(err)
	}

	if len(roster) != 1 || roster[0] != subjectUCA {
		t.Fatalf("defensive roster = %#v", roster)
	}

	if _, err := snapshot.Allows(contract.KindSchedule, subjectUCA); err == nil {
		t.Fatal("missing kind was allowed")
	}
}

func TestProjectionTargetSnapshotPreservesEmptyRosterAndCap(t *testing.T) {
	requested := []contract.ObservationKind{contract.KindLiveSnapshot, contract.KindSchedule}

	snapshot, err := newProjectionTargetSnapshot(9, requested, map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot: {subjectUCB, subjectUCA},
		contract.KindSchedule:     {},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	roster, err := snapshot.Roster(contract.KindLiveSnapshot)
	if err != nil {
		t.Fatal(err)
	}

	if len(roster) != 2 || roster[0] != subjectUCA || roster[1] != subjectUCB {
		t.Fatalf("sorted roster = %#v", roster)
	}

	empty, err := snapshot.Roster(contract.KindSchedule)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("explicit empty roster = %#v, %v", empty, err)
	}

	_, err = newProjectionTargetSnapshot(9, requested, map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot: {subjectUCA, subjectUCB},
		contract.KindSchedule:     {"schedule"},
	}, 2)
	if collecterr.CodeOf(err) != collecterr.TargetRosterTooLarge || collecterr.ClassOf(err) != collecterr.ClassResourceLimit {
		t.Fatalf("cap error = %v", err)
	}
}

func TestTargetSnapshotRejectsDuplicatesMissingKindsAndInvalidGeneration(t *testing.T) {
	_, err := newProjectionTargetSnapshot(1, []contract.ObservationKind{contract.KindLiveSnapshot}, map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot: {subjectUCA, subjectUCA},
	}, 2)
	if collecterr.ClassOf(err) != collecterr.ClassInternal {
		t.Fatalf("duplicate error = %v", err)
	}

	_, err = newProjectionTargetSnapshot(1, []contract.ObservationKind{contract.KindLiveSnapshot}, map[contract.ObservationKind][]string{}, 2)
	if collecterr.ClassOf(err) != collecterr.ClassInternal {
		t.Fatalf("missing kind error = %v", err)
	}

	_, err = newExactTargetSnapshot(0, subjectUCA, []contract.ObservationKind{contract.KindLiveSnapshot}, nil)
	if collecterr.ClassOf(err) != collecterr.ClassInternal {
		t.Fatalf("generation error = %v", err)
	}

	_, err = newExactTargetSnapshot(1, subjectUCA, []contract.ObservationKind{
		contract.KindLiveSnapshot, contract.KindLiveSnapshot,
	}, nil)
	if collecterr.ClassOf(err) != collecterr.ClassInternal {
		t.Fatalf("requested duplicate error = %v", err)
	}
}

func TestTargetSnapshotValidateRequestedAndClone(t *testing.T) {
	snapshot, err := newProjectionTargetSnapshot(2, []contract.ObservationKind{contract.KindLiveSnapshot}, map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot: {subjectUCA},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	if matchErr := snapshot.ValidateRequested([]contract.ObservationKind{contract.KindLiveSnapshot}); matchErr != nil {
		t.Fatal(matchErr)
	}

	if mismatchErr := snapshot.ValidateRequested([]contract.ObservationKind{contract.KindSchedule}); mismatchErr == nil {
		t.Fatal("mismatched requested kinds passed validation")
	}

	clone := snapshot.Clone()
	cloneSubjects, ok := clone.subjects[contract.KindLiveSnapshot]

	if !ok || len(cloneSubjects) == 0 {
		t.Fatal("cloned live roster is empty")
	}

	cloneSubjects[0] = "mutated"

	allowed, err := snapshot.Allows(contract.KindLiveSnapshot, subjectUCA)

	if err != nil || !allowed {
		t.Fatalf("clone mutated source = %t, %v", allowed, err)
	}
}

func TestBuildJobKeyAndExpectedLeaseSubjectPreservePersistedGlobalIdentity(t *testing.T) {
	jobs := sourceobservation.InitialJobContracts()

	for _, test := range []struct {
		id      sourceobservation.JobID
		subject string
		key     string
	}{
		{sourceobservation.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_live"}, "global:holodex_live", "collector:holodex:holodex_live:global"},
		{sourceobservation.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_metadata"}, "global:holodex_metadata", "collector:holodex:holodex_metadata:global"},
		{sourceobservation.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_schedule"}, "global:holodex_schedule", "collector:holodex:holodex_schedule:global"},
		{sourceobservation.JobID{Provider: contract.ProviderHololiveOfficial, Kind: "official_schedule"}, subjectGlobalSchedule, "collector:hololive_official:official_schedule:global"},
	} {
		job, ok := jobs.Definition(test.id)
		if !ok {
			t.Fatalf("%s contract missing", test.id)
		}

		subject, err := ExpectedLeaseSubject(job, "ignored")
		if err != nil || subject != test.subject {
			t.Fatalf("%s global subject = %q, %v", test.id, subject, err)
		}

		key, err := BuildJobKey(job.ID(), subject)
		if err != nil || key != test.key {
			t.Fatalf("%s global key = %q, %v", test.id, key, err)
		}
	}

	subjectJob, _ := jobs.Definition(sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"})
	subject, err := ExpectedLeaseSubject(subjectJob, subjectUCA)

	if err != nil || subject != subjectUCA {
		t.Fatalf("subject identity = %q, %v", subject, err)
	}

	key, err := BuildJobKey(subjectJob.ID(), subject)
	if err != nil || key != "collector:youtubejs:community_collect:UC_A" {
		t.Fatalf("subject key = %q, %v", key, err)
	}

	if _, err := BuildJobKey(subjectJob.ID(), ""); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("empty subject error = %v", err)
	}
}
