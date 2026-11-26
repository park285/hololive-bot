package sourceobservation

import (
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	unchangedEvidenceSHA256 = "577ba8713d4dda952fc67ac71ea24ee879e4f00d6e1f3910e923522c45136683"
	unchangedPayloadSHA256  = "fcd6503b5e1eb59285c6f7c1b4a250c84f29103eb0b8bdb0634544e7f39d3386"
)

func relocationEnvelope(t *testing.T, session LiveSessionV1) Envelope {
	t.Helper()

	return relocationEnvelopeAt(t, session, LiveSnapshotQueryContractGeneration)
}

func relocationEnvelopeAt(t *testing.T, session LiveSessionV1, generation int64) Envelope {
	t.Helper()

	statuses := slices.Clone(liveSnapshotQueryStatuses)
	payload := LiveSnapshotV1{
		Sessions: []LiveSessionV1{session},
		Coverage: GlobalChannelCoverageV1{RequestedChannelIDs: []string{testChannelID}, Filters: LiveFiltersV1{Statuses: statuses}},
		Query:    &LiveSnapshotQueryV1{ChannelID: testChannelID, Source: "streams", Statuses: statuses, Exhausted: true, PageCount: 1},
	}

	if generation == LiveSnapshotMetadataContractGeneration {
		payload.Query = nil
	}

	envelope := newPaginatedEnvelope(t, KindLiveSnapshot, mustMarshalPayload(t, payload), CompletenessComplete)

	envelope.ContractGeneration = generation

	return envelope
}

func relocationUpcoming() LiveSessionV1 {
	return LiveSessionV1{
		VideoID: testVideoID, ChannelID: testChannelID, Status: queryUpcomingStatus,
		ScheduledAt: new(time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)),
	}
}

func TestEvidenceDigestWithoutScheduleObservationsIsUnchanged(t *testing.T) {
	prepared, err := PrepareEnvelope(relocationEnvelope(t, relocationUpcoming()))
	if err != nil {
		t.Fatal(err)
	}

	if prepared.EvidenceSHA256 != unchangedEvidenceSHA256 || prepared.PayloadSHA256 != unchangedPayloadSHA256 {
		t.Fatalf("evidence=%s payload=%s", prepared.EvidenceSHA256, prepared.PayloadSHA256)
	}
}

func TestRelocatedScheduleObservationKeepsPayloadAndChangesEvidence(t *testing.T) {
	envelope := relocationEnvelope(t, relocationUpcoming())
	observed := envelope.ScheduledFor.Add(-2 * time.Minute).In(time.FixedZone("KST", 9*60*60))

	envelope.ScheduleObservedAt = map[string]time.Time{testVideoID: observed}

	prepared, err := PrepareEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}

	if prepared.PayloadSHA256 != unchangedPayloadSHA256 {
		t.Fatalf("payload sha256 = %s, want the payload without schedule observations", prepared.PayloadSHA256)
	}

	if prepared.EvidenceSHA256 == unchangedEvidenceSHA256 {
		t.Fatal("evidence sha256 ignores the relocated schedule observation")
	}

	stored, err := jsonv2.Marshal(prepared.ScheduleObservedAt)
	if err != nil {
		t.Fatal(err)
	}

	var loaded map[string]time.Time

	if err := jsonv2.Unmarshal(stored, &loaded); err != nil {
		t.Fatal(err)
	}

	prepared.ScheduleObservedAt = loaded
	if _, err := prepared.ValidateAndCanonicalPayload(); err != nil {
		t.Fatalf("stored schedule observations fail validation: %v", err)
	}

	prepared.ScheduleObservedAt = map[string]time.Time{testVideoID: observed.Add(-time.Second)}
	if _, err := prepared.ValidateAndCanonicalPayload(); err == nil {
		t.Fatal("changed schedule observation passed evidence validation")
	}
}

func TestRelocatedScheduleObservationRejectsInvalidShapes(t *testing.T) {
	base := relocationEnvelope(t, relocationUpcoming())
	slot := base.ScheduledFor
	valid := slot.Add(-2 * time.Minute)

	both := relocationUpcoming()

	both.ScheduleObservedAt = new(valid)

	live := relocationUpcoming()

	live.Status = testStatusLive
	live.ScheduledAt = nil

	cases := map[string]struct {
		session  LiveSessionV1
		observed map[string]time.Time
	}{
		"both forms":          {session: both, observed: map[string]time.Time{testVideoID: valid}},
		"unknown video":       {session: relocationUpcoming(), observed: map[string]time.Time{"vid-unknown": valid}},
		"live row":            {session: live, observed: map[string]time.Time{testVideoID: valid}},
		"same slot":           {session: relocationUpcoming(), observed: map[string]time.Time{testVideoID: slot}},
		"beyond reuse window": {session: relocationUpcoming(), observed: map[string]time.Time{testVideoID: slot.Add(-LiveScheduleObservationMaxAge - time.Second)}},
	}

	for name, tc := range cases {
		envelope := relocationEnvelope(t, tc.session)

		envelope.ScheduleObservedAt = tc.observed

		if _, err := PrepareEnvelope(envelope); err == nil || !strings.Contains(err.Error(), "schedule observation") {
			t.Fatalf("%s: error = %v, want schedule observation rejection", name, err)
		}
	}

	metadata := relocationEnvelopeAt(t, relocationUpcoming(), LiveSnapshotMetadataContractGeneration)

	metadata.ScheduleObservedAt = map[string]time.Time{testVideoID: valid}

	if _, err := PrepareEnvelope(metadata); err == nil || !strings.Contains(err.Error(), "schedule observation") {
		t.Fatalf("metadata generation: error = %v, want schedule observation rejection", err)
	}

	videoList := newPaginatedEnvelope(t, KindVideoList, mustMarshalPayload(t, VideoListV1{ChannelID: testChannelID}), CompletenessComplete)

	videoList.ScheduleObservedAt = map[string]time.Time{testVideoID: valid}

	if _, err := PrepareEnvelope(videoList); err == nil || !strings.Contains(err.Error(), "schedule observation") {
		t.Fatalf("video list: error = %v, want schedule observation rejection", err)
	}
}
