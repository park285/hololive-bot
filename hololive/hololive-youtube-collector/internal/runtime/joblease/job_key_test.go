package joblease

import (
	"errors"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestBuildJobKeyAndExpectedLeaseSubjectPreservePersistedGlobalIdentity(t *testing.T) {
	jobs := collection.InitialJobContracts()

	for _, test := range []struct {
		id      collection.JobID
		subject string
		key     string
	}{
		{collection.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_live"}, "global:holodex_live", "collector:holodex:holodex_live:global"},
		{collection.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_metadata"}, "global:holodex_metadata", "collector:holodex:holodex_metadata:global"},
		{collection.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_schedule"}, "global:holodex_schedule", "collector:holodex:holodex_schedule:global"},
		{collection.JobID{Provider: contract.ProviderHololiveOfficial, Kind: "official_schedule"}, subjectGlobalSchedule, "collector:hololive_official:official_schedule:global"},
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

	subjectJob, _ := jobs.Definition(collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"})
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
