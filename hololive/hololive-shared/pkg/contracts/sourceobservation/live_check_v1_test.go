package sourceobservation

import (
	"encoding/json/jsontext"
	"testing"
	"time"
)

const (
	testSelectedVideoID = "video-selected"
	testOtherChannelID  = "UC_OTHER"
)

var liveCheckScheduledFor = time.Date(2026, time.September, 26, 1, 0, 0, 0, time.UTC)

func TestChannelLiveCheckNegativeEvidenceRequiresConfirmedIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		payload      ChannelLiveCheckV1
		completeness Completeness
		negative     bool
	}{
		{"upcoming waiting room", channelLiveCheck(ChannelLiveCheckUpcomingVideo, testSelectedVideoID, true, ""), CompletenessComplete, true},
		{"channel page", channelLiveCheck(ChannelLiveCheckChannelPage, "", true, ""), CompletenessComplete, true},
		{"live video", channelLiveCheck(ChannelLiveCheckLiveVideo, testSelectedVideoID, true, ""), CompletenessComplete, false},
		{"upcoming without waiting state", channelLiveCheck(ChannelLiveCheckUnknown, testSelectedVideoID, true, LiveCheckReasonNotWaitingState), CompletenessUnknown, false},
		{"request failed", channelLiveCheck(ChannelLiveCheckUnknown, "", false, LiveCheckReasonRequestFailed), CompletenessUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prepared, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindChannelLiveCheck, tt.payload, tt.completeness))
			if err != nil {
				t.Fatalf("prepare channel live check: %v", err)
			}

			var decoded ChannelLiveCheckV1

			if err := decodeStrictJSON(prepared.Payload, &decoded); err != nil {
				t.Fatalf("decode prepared channel live check: %v", err)
			}

			if got := decoded.NegativeLiveEvidence(); got != tt.negative {
				t.Fatalf("NegativeLiveEvidence() = %t, want %t", got, tt.negative)
			}

			if decoded.UnknownReason != tt.payload.UnknownReason {
				t.Fatalf("unknown reason = %q, want %q", decoded.UnknownReason, tt.payload.UnknownReason)
			}
		})
	}
}

func TestChannelLiveCheckRejectsInconsistentOutcomes(t *testing.T) {
	t.Parallel()

	otherCoverage := channelLiveCheck(ChannelLiveCheckChannelPage, "", true, "")

	otherCoverage.Coverage.ChannelID = testOtherChannelID

	tests := []struct {
		name         string
		payload      ChannelLiveCheckV1
		completeness Completeness
	}{
		{"channel page without confirmed identity", channelLiveCheck(ChannelLiveCheckChannelPage, "", false, ""), CompletenessComplete},
		{"upcoming without selected video", channelLiveCheck(ChannelLiveCheckUpcomingVideo, "", true, ""), CompletenessComplete},
		{"channel page with selected video", channelLiveCheck(ChannelLiveCheckChannelPage, testSelectedVideoID, true, ""), CompletenessComplete},
		{"known outcome with unknown completeness", channelLiveCheck(ChannelLiveCheckChannelPage, "", true, ""), CompletenessUnknown},
		{"known outcome with unknown reason", channelLiveCheck(ChannelLiveCheckChannelPage, "", true, LiveCheckReasonNotWaitingState), CompletenessComplete},
		{"unknown without reason", channelLiveCheck(ChannelLiveCheckUnknown, "", false, ""), CompletenessUnknown},
		{"unknown with complete completeness", channelLiveCheck(ChannelLiveCheckUnknown, "", false, LiveCheckReasonStructureUnrecognized), CompletenessComplete},
		{"identity mismatch claiming identity", channelLiveCheck(ChannelLiveCheckUnknown, testSelectedVideoID, true, LiveCheckReasonIdentityMismatch), CompletenessUnknown},
		{"video-only availability reason", channelLiveCheck(ChannelLiveCheckUnknown, testSelectedVideoID, true, LiveCheckReasonAvailabilityUnclassified), CompletenessUnknown},
		{"unsupported outcome", channelLiveCheck("OFFLINE", "", true, ""), CompletenessComplete},
		{"coverage outside subject", otherCoverage, CompletenessComplete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindChannelLiveCheck, tt.payload, tt.completeness)); err == nil {
				t.Fatal("inconsistent channel live check was accepted")
			}
		})
	}
}

func TestVideoLiveCheckPreservesAbsentAndFalseFacts(t *testing.T) {
	t.Parallel()

	absent := publicEndedVideoCheck()

	absent.IsLiveContent = nil

	explicitFalse := publicEndedVideoCheck()

	explicitFalse.IsLiveContent = new(false)

	preparedAbsent, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, absent, CompletenessPartial))
	if err != nil {
		t.Fatalf("prepare absent is_live_content: %v", err)
	}

	preparedFalse, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, explicitFalse, CompletenessPartial))
	if err != nil {
		t.Fatalf("prepare false is_live_content: %v", err)
	}

	if preparedAbsent.PayloadSHA256 == preparedFalse.PayloadSHA256 {
		t.Fatal("absent and false is_live_content share one payload identity")
	}

	decodedAbsent := decodeVideoLiveCheck(t, preparedAbsent.Payload)
	decodedFalse := decodeVideoLiveCheck(t, preparedFalse.Payload)

	if decodedAbsent.IsLiveContent != nil {
		t.Fatalf("absent is_live_content decoded as %v", *decodedAbsent.IsLiveContent)
	}

	if decodedFalse.IsLiveContent == nil || *decodedFalse.IsLiveContent {
		t.Fatalf("false is_live_content decoded as %v", decodedFalse.IsLiveContent)
	}
}

func TestVideoLiveCheckVerifiedEndRequiresTrustedIdentity(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	endedKST := liveCheckScheduledFor.Add(-10 * time.Minute).In(kst)
	zoned := publicEndedVideoCheck()

	zoned.EndedAt = &endedKST

	mismatch := withVideoUnknown(publicEndedVideoCheck(), LiveCheckReasonIdentityMismatch)

	mismatch.IdentityConfirmed = false
	mismatch.ChannelID = testOtherChannelID

	explicitNotLive := publicEndedVideoCheck()

	explicitNotLive.IsLive = new(false)

	availabilityOnly := withVideoUnknown(publicEndedVideoCheck(), LiveCheckReasonAvailabilityUnclassified)

	availabilityOnly.IsPrivate = nil

	tests := []struct {
		name     string
		payload  VideoLiveCheckV1
		verified bool
	}{
		{"public ended with offset timestamp", zoned, true},
		{"availability unclassified keeps valid end", availabilityOnly, true},
		{"identity mismatch", mismatch, false},
		{"login required", withVideoUnknown(publicEndedVideoCheck(), LiveCheckReasonLoginRequiredUnclassified), false},
		{"error renderer", withVideoUnknown(publicEndedVideoCheck(), LiveCheckReasonErrorUnclassified), false},
		{"structure unrecognized", withVideoUnknown(publicEndedVideoCheck(), LiveCheckReasonStructureUnrecognized), false},
		{"explicit non-live with verified end", explicitNotLive, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prepared, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, tt.payload, videoCompleteness(&tt.payload)))
			if err != nil {
				t.Fatalf("prepare video live check: %v", err)
			}

			decoded := decodeVideoLiveCheck(t, prepared.Payload)

			endedAt, ok := decoded.VerifiedEndedAt()
			if ok != tt.verified {
				t.Fatalf("VerifiedEndedAt() ok = %t, want %t", ok, tt.verified)
			}

			if !ok {
				return
			}

			want := liveCheckScheduledFor.Add(-10 * time.Minute)
			if !endedAt.Equal(want) || endedAt.Location() != time.UTC {
				t.Fatalf("VerifiedEndedAt() = %s, want %s in UTC", endedAt, want)
			}
		})
	}
}

func TestVideoLiveCheckCurrentLiveOnlyFromTrustedFacts(t *testing.T) {
	t.Parallel()

	unplayableLive := withVideoUnknown(publicLiveVideoCheck(), LiveCheckReasonAvailabilityUnclassified)

	unplayableLive.IsPrivate = nil

	membersOnly := publicLiveVideoCheck()

	membersOnly.IsPrivate = nil
	membersOnly.Availability = VideoAvailabilityMembersOnly
	membersOnly.Method = VideoAvailabilityMethodPlayerMembersOnly

	tests := []struct {
		name    string
		payload VideoLiveCheckV1
		live    bool
	}{
		{"public live", publicLiveVideoCheck(), true},
		{"members only live", membersOnly, true},
		{"availability unclassified live", unplayableLive, true},
		{"login required", withVideoUnknown(publicLiveVideoCheck(), LiveCheckReasonLoginRequiredUnclassified), false},
		{"error renderer", withVideoUnknown(publicLiveVideoCheck(), LiveCheckReasonErrorUnclassified), false},
		{"structure unrecognized", withVideoUnknown(publicLiveVideoCheck(), LiveCheckReasonStructureUnrecognized), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prepared, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, tt.payload, videoCompleteness(&tt.payload)))
			if err != nil {
				t.Fatalf("prepare video live check: %v", err)
			}

			decoded := decodeVideoLiveCheck(t, prepared.Payload)
			if got := decoded.CurrentlyLive(); got != tt.live {
				t.Fatalf("CurrentlyLive() = %t, want %t", got, tt.live)
			}
		})
	}
}

func TestVideoLiveCheckContradictionsRequireContradictoryReason(t *testing.T) {
	t.Parallel()

	for _, tt := range contradictoryVideoFacts() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, tt.payload, CompletenessPartial)); err == nil {
				t.Fatal("contradictory facts were accepted as a known result")
			}

			availabilityOnly := withVideoUnknown(tt.payload, LiveCheckReasonAvailabilityUnclassified)
			if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, availabilityOnly, CompletenessUnknown)); err == nil {
				t.Fatal("contradictory facts were accepted as trusted lifecycle facts")
			}

			reported := withVideoUnknown(tt.payload, LiveCheckReasonContradictoryFields)

			prepared, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, reported, CompletenessUnknown))
			if err != nil {
				t.Fatalf("contradictory_fields observation rejected: %v", err)
			}

			decoded := decodeVideoLiveCheck(t, prepared.Payload)
			if _, ok := decoded.VerifiedEndedAt(); ok || decoded.CurrentlyLive() {
				t.Fatal("contradictory_fields observation exposed lifecycle facts")
			}
		})
	}
}

func TestVideoLiveCheckAvailabilityRequiresRawPrivateFlag(t *testing.T) {
	t.Parallel()

	publicWithoutFlag := publicEndedVideoCheck()

	publicWithoutFlag.IsPrivate = nil

	publicPrivate := publicEndedVideoCheck()

	publicPrivate.IsPrivate = new(true)

	unavailableNotPrivate := withAvailability(publicEndedVideoCheck(), VideoAvailabilityPublicUnavailable, VideoAvailabilityMethodPlayerPrivate)
	unavailableWithoutFlag := withAvailability(publicEndedVideoCheck(), VideoAvailabilityPublicUnavailable, VideoAvailabilityMethodPlayerPrivate)

	unavailableWithoutFlag.IsPrivate = nil

	publicUnknownMethod := withAvailability(publicEndedVideoCheck(), VideoAvailabilityPublic, VideoAvailabilityMethodUnknown)
	unknownPublicMethod := withVideoUnknown(publicEndedVideoCheck(), LiveCheckReasonAvailabilityUnclassified)

	unknownPublicMethod.Method = VideoAvailabilityMethodPlayerPublic

	for name, payload := range map[string]VideoLiveCheckV1{
		"public without raw is_private":              publicWithoutFlag,
		"public with is_private true":                publicPrivate,
		"public unavailable with is_private false":   unavailableNotPrivate,
		"public unavailable without raw is_private":  unavailableWithoutFlag,
		"public with unknown method":                 publicUnknownMethod,
		"unknown availability with player method":    unknownPublicMethod,
		"unsupported availability":                   withAvailability(publicEndedVideoCheck(), "DELETED", VideoAvailabilityMethodUnknown),
		"known availability with unknown completion": publicEndedVideoCheck(),
	} {
		completeness := CompletenessPartial

		if name == "known availability with unknown completion" || payload.Availability == VideoAvailabilityUnknown {
			completeness = CompletenessUnknown
		}

		if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, payload, completeness)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	private := withAvailability(publicEndedVideoCheck(), VideoAvailabilityPublicUnavailable, VideoAvailabilityMethodPlayerPrivate)

	private.IsPrivate = new(true)
	private.EndedAt = nil

	prepared, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, private, CompletenessPartial))
	if err != nil {
		t.Fatalf("raw is_private=true public unavailable rejected: %v", err)
	}

	decoded := decodeVideoLiveCheck(t, prepared.Payload)
	if _, ok := decoded.VerifiedEndedAt(); ok || decoded.CurrentlyLive() {
		t.Fatal("public unavailable without end time exposed lifecycle facts")
	}
}

func TestVideoLiveCheckUnknownShapes(t *testing.T) {
	t.Parallel()

	requestFailed := VideoLiveCheckV1{
		VideoID: testVideoID, Availability: VideoAvailabilityUnknown, Method: VideoAvailabilityMethodUnknown,
		UnknownReason: LiveCheckReasonRequestFailed, Coverage: VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}
	identityMissing := requestFailed

	identityMissing.UnknownReason = LiveCheckReasonIdentityMissing

	for name, payload := range map[string]VideoLiveCheckV1{
		"request failed without response":  requestFailed,
		"identity missing without channel": identityMissing,
	} {
		if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, payload, CompletenessUnknown)); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}

	requestFailedWithChannel := requestFailed

	requestFailedWithChannel.ChannelID = testChannelID

	requestFailedWithFact := requestFailed

	requestFailedWithFact.IsLiveNow = new(false)

	identityMissingConfirmed := identityMissing

	identityMissingConfirmed.IdentityConfirmed = true
	identityMissingConfirmed.ChannelID = testChannelID

	loginWithoutIdentity := requestFailed

	loginWithoutIdentity.UnknownReason = LiveCheckReasonLoginRequiredUnclassified

	channelOnlyReason := withVideoUnknown(publicLiveVideoCheck(), LiveCheckReasonNotWaitingState)
	confirmedWithoutChannel := publicEndedVideoCheck()

	confirmedWithoutChannel.ChannelID = ""

	missingReason := withVideoUnknown(publicLiveVideoCheck(), "")
	subjectMismatch := publicEndedVideoCheck()

	subjectMismatch.Coverage.VideoID = testSelectedVideoID

	for name, payload := range map[string]VideoLiveCheckV1{
		"request failed with channel":        requestFailedWithChannel,
		"request failed with response fact":  requestFailedWithFact,
		"identity missing claiming identity": identityMissingConfirmed,
		"login required before identity":     loginWithoutIdentity,
		"channel-only reason":                channelOnlyReason,
		"confirmed identity without channel": confirmedWithoutChannel,
		"unknown without reason":             missingReason,
		"coverage outside subject":           subjectMismatch,
	} {
		if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, payload, videoCompleteness(&payload))); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestVideoLiveCheckRejectsNonStrictRawFacts(t *testing.T) {
	t.Parallel()

	const (
		prefix = `{"video_id":"video-1","channel_id":"UC_TEST","identity_confirmed":true,"is_private":false,`
		suffix = `"availability":"PUBLIC","method":"player_public","coverage":{"video_id":"video-1"}}`
	)

	base := newLiveCheckEnvelope(t, KindVideoLiveCheck, publicEndedVideoCheck(), CompletenessPartial)

	base.Payload = jsontext.Value(prefix + `"is_live_now":false,"ended_at":"2026-09-26T00:50:00Z",` + suffix)
	if _, err := PrepareEnvelope(base); err != nil {
		t.Fatalf("strict RFC3339 end rejected: %v", err)
	}

	for name, raw := range map[string]string{
		"non RFC3339 end":      prefix + `"is_live_now":false,"ended_at":"2026-09-26 00:50:00",` + suffix,
		"zero end":             prefix + `"is_live_now":false,"ended_at":"0001-01-01T00:00:00Z",` + suffix,
		"string boolean":       prefix + `"is_live_now":"false","ended_at":"2026-09-26T00:50:00Z",` + suffix,
		"unknown status field": prefix + `"playability_status":"UNPLAYABLE",` + suffix,
	} {
		envelope := base

		envelope.Payload = jsontext.Value(raw)

		if _, err := PrepareEnvelope(envelope); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestLiveCheckEnvelopeContract(t *testing.T) {
	t.Parallel()

	payload := channelLiveCheck(ChannelLiveCheckChannelPage, "", true, "")
	if _, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindChannelLiveCheck, payload, CompletenessComplete)); err != nil {
		t.Fatalf("valid channel live check rejected: %v", err)
	}

	sourceEventAt := liveCheckScheduledFor

	for name, mutate := range map[string]func(*Envelope){
		"non youtubejs provider": func(e *Envelope) { e.Provider = ProviderHolodex },
		"contiguous continuity":  func(e *Envelope) { e.Continuity = ContinuityContiguous },
		"future generation":      func(e *Envelope) { e.ContractGeneration++ },
		"source event time":      func(e *Envelope) { e.SourceEventAt = &sourceEventAt },
	} {
		for _, kind := range []ObservationKind{KindChannelLiveCheck, KindVideoLiveCheck} {
			var envelope Envelope

			if kind == KindChannelLiveCheck {
				envelope = newLiveCheckEnvelope(t, kind, payload, CompletenessComplete)
			} else {
				envelope = newLiveCheckEnvelope(t, kind, publicEndedVideoCheck(), CompletenessPartial)
			}

			mutate(&envelope)

			if _, err := PrepareEnvelope(envelope); err == nil {
				t.Fatalf("%s %s was accepted", kind, name)
			}
		}
	}
}

type videoLiveCheckCase struct {
	name    string
	payload VideoLiveCheckV1
}

func contradictoryVideoFacts() []videoLiveCheckCase {
	endedAfterObserved := publicEndedVideoCheck()
	future := liveCheckScheduledFor.Add(2 * time.Second)

	endedAfterObserved.EndedAt = &future

	endedBeforeStarted := publicEndedVideoCheck()
	early := endedBeforeStarted.StartedAt.Add(-time.Minute)

	endedBeforeStarted.EndedAt = &early

	liveDisagrees := publicLiveVideoCheck()

	liveDisagrees.IsLiveNow = new(false)

	liveWithEnd := publicLiveVideoCheck()
	ended := liveCheckScheduledFor.Add(-time.Minute)

	liveWithEnd.EndedAt = &ended

	liveWithUpcoming := publicLiveVideoCheck()

	liveWithUpcoming.IsUpcoming = new(true)

	upcomingWithEnd := publicEndedVideoCheck()

	upcomingWithEnd.IsUpcoming = new(true)

	return []videoLiveCheckCase{
		{"end after observed", endedAfterObserved},
		{"end before start", endedBeforeStarted},
		{"is_live and is_live_now disagree", liveDisagrees},
		{"live with end", liveWithEnd},
		{"live with upcoming", liveWithUpcoming},
		{"upcoming with end", upcomingWithEnd},
	}
}

func channelLiveCheck(outcome ChannelLiveCheckOutcome, selected string, confirmed bool, reason LiveCheckUnknownReason) ChannelLiveCheckV1 {
	return ChannelLiveCheckV1{
		ChannelID: testChannelID, Outcome: outcome, SelectedVideoID: selected,
		ChannelIdentityConfirmed: confirmed, UnknownReason: reason,
		Coverage: ChannelLiveCheckCoverageV1{ChannelID: testChannelID},
	}
}

func publicEndedVideoCheck() VideoLiveCheckV1 {
	started := liveCheckScheduledFor.Add(-2 * time.Hour)
	ended := liveCheckScheduledFor.Add(-10 * time.Minute)

	return VideoLiveCheckV1{
		VideoID: testVideoID, ChannelID: testChannelID, IdentityConfirmed: true,
		IsLiveNow: new(false), IsLiveContent: new(true), IsPrivate: new(false), HasLiveBroadcastDetails: new(true),
		StartedAt: &started, EndedAt: &ended,
		Availability: VideoAvailabilityPublic, Method: VideoAvailabilityMethodPlayerPublic,
		Coverage: VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}
}

func publicLiveVideoCheck() VideoLiveCheckV1 {
	started := liveCheckScheduledFor.Add(-time.Hour)

	return VideoLiveCheckV1{
		VideoID: testVideoID, ChannelID: testChannelID, IdentityConfirmed: true,
		IsLive: new(true), IsLiveNow: new(true), IsLiveContent: new(true), IsPrivate: new(false),
		HasLiveBroadcastDetails: new(true), StartedAt: &started,
		Availability: VideoAvailabilityPublic, Method: VideoAvailabilityMethodPlayerPublic,
		Coverage: VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}
}

func withVideoUnknown(payload VideoLiveCheckV1, reason LiveCheckUnknownReason) VideoLiveCheckV1 {
	payload.Availability = VideoAvailabilityUnknown
	payload.Method = VideoAvailabilityMethodUnknown
	payload.UnknownReason = reason

	return payload
}

func withAvailability(payload VideoLiveCheckV1, availability VideoAvailability, method VideoAvailabilityMethod) VideoLiveCheckV1 {
	payload.Availability = availability
	payload.Method = method

	return payload
}

func videoCompleteness(payload *VideoLiveCheckV1) Completeness {
	if payload.Availability == VideoAvailabilityUnknown {
		return CompletenessUnknown
	}

	return CompletenessPartial
}

func decodeVideoLiveCheck(t *testing.T, payload jsontext.Value) VideoLiveCheckV1 {
	t.Helper()

	var decoded VideoLiveCheckV1

	if err := decodeStrictJSON(payload, &decoded); err != nil {
		t.Fatalf("decode prepared video live check: %v", err)
	}

	return decoded
}

func newLiveCheckEnvelope(t *testing.T, kind ObservationKind, payload any, completeness Completeness) Envelope {
	t.Helper()

	subject, jobKind := testChannelID, "youtubejs_channel_live_check"
	schemaVersion, generation := SchemaVersionV1, LiveCheckContractGeneration

	if kind == KindVideoLiveCheck {
		subject, jobKind = testVideoID, "youtubejs_video_live"
		schemaVersion, generation = VideoLifecycleSchemaVersion, VideoLifecycleContractGeneration
	}

	return Envelope{
		Provider: ProviderYouTubeJS, ObservationKind: kind, SubjectKey: subject,
		SchemaVersion: schemaVersion, ContractGeneration: generation,
		ScheduledFor: liveCheckScheduledFor, ObservedAt: liveCheckScheduledFor.Add(time.Second),
		Completeness: completeness, Continuity: ContinuityNotApplicable,
		Payload: mustMarshalPayload(t, payload), CollectorInstance: testCollectorInstance,
		Lease: LeaseProof{
			JobKey: "collector:youtubejs:" + jobKind + ":" + subject, CollectionJobKind: jobKind,
			OwnerInstance: testCollectorInstance, FenceEpoch: 1, ProjectionGeneration: 1, ScheduledFor: liveCheckScheduledFor,
		},
	}
}
