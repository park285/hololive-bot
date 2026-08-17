package youtubejscollector

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
	"github.com/kapu/hololive-youtube-collector/internal/testutil"
)

func TestCommunityRunnerPublishesExhaustedFixture(t *testing.T) {
	t.Parallel()
	var result youtubejs.CommunityResult
	loadJSON(t, "community.json", &result)
	runner := NewCommunityRunner(&communityFake{result: result}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput(t, "UC_TEST", "community_collect", contract.KindCommunityPage))
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if len(observations) != 1 || observations[0].Completeness != contract.CompletenessComplete {
		t.Fatalf("output = %#v", observations)
	}
}

func TestCommunityRunnerSkipsMissingTab(t *testing.T) {
	t.Parallel()
	runner := NewCommunityRunner(&communityFake{result: youtubejs.CommunityResult{MissingTab: true}}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput(t, "UC_NONE", "community_collect", contract.KindCommunityPage))
	if err != nil {
		t.Fatal(err)
	}
	if !output.Output().Empty() {
		t.Fatalf("missing tab published %#v", output.Output().Observations())
	}
}

func TestCommunityRunnerPreservesInputOrderHash(t *testing.T) {
	t.Parallel()
	var result youtubejs.CommunityResult
	loadJSON(t, "community.json", &result)
	reversed := result
	reversed.Posts = append([]*parser.CommunityPost(nil), result.Posts...)
	for i, j := 0, len(reversed.Posts)-1; i < j; i, j = i+1, j-1 {
		reversed.Posts[i], reversed.Posts[j] = reversed.Posts[j], reversed.Posts[i]
	}
	first := mustCollectCommunity(t, &result)
	second := mustCollectCommunity(t, &reversed)
	if first.PayloadSHA256 != second.PayloadSHA256 || first.ScopeSHA256 != second.ScopeSHA256 {
		t.Fatalf("ordering changed hashes %s/%s vs %s/%s", first.PayloadSHA256, first.ScopeSHA256, second.PayloadSHA256, second.ScopeSHA256)
	}
}

func TestContentRunnerEmitsVideosAndShortsFromOneJob(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	var shorts youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	loadJSON(t, "shorts.json", &shorts)
	fake := &contentFake{results: map[string]youtubejs.ContentResult{"videos": videos, "shorts": shorts}}
	runner := NewContentRunner(fake, 10)
	output, err := runner.Collect(context.Background(), youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList))
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if fake.calls != 2 || len(observations) != 2 {
		t.Fatalf("calls=%d observations=%d", fake.calls, len(observations))
	}
}

func TestContentRunnerOmitsMissingShortsTab(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	fake := &contentFake{results: map[string]youtubejs.ContentResult{
		"videos": videos,
		"shorts": {MissingTab: true},
	}}
	runner := NewContentRunner(fake, 10)
	output, err := runner.Collect(context.Background(), youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList))
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if len(observations) != 1 || observations[0].ObservationKind != contract.KindVideoList {
		t.Fatalf("observations = %#v", observations)
	}
}

func TestContentRunnerReturnsExplicitPartialAfterShortsTimeout(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	fake := &contentFake{
		results: map[string]youtubejs.ContentResult{"videos": videos},
		errByKind: map[string]error{
			"shorts": collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "shorts timeout"),
		},
	}
	result, err := NewContentRunner(fake, 10).Collect(
		context.Background(),
		youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList),
	)
	if err != nil {
		t.Fatal(err)
	}
	partial, ok := result.PartialFailure()
	if result.Kind() != collectutil.CollectPartial || !ok ||
		len(result.Output().Observations()) != 1 ||
		len(partial.FailedKinds()) != 1 || partial.FailedKinds()[0] != contract.KindShortsList {
		t.Fatalf("partial result = %#v failed=%#v", result, partial)
	}
}

func TestContentRunnerDoesNotPublishPartialForNonDegradableFailures(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	tests := []struct {
		name string
		err  error
	}{
		{name: "parser drift", err: collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "shorts parser drift")},
		{name: "configuration", err: collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "helper configuration")},
		{name: "internal", err: collecterr.New(collecterr.Internal, collecterr.ClassInternal, "helper invariant")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &contentFake{
				results:   map[string]youtubejs.ContentResult{"videos": videos},
				errByKind: map[string]error{"shorts": tt.err},
			}
			result, err := NewContentRunner(fake, 10).Collect(
				context.Background(),
				youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList),
			)
			if err == nil || !result.IsZero() || collecterr.ClassOf(err) != collecterr.ClassOf(tt.err) {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestContentRunnerFetchesAndEmitsOnlyEnabledKind(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	fake := &contentFake{results: map[string]youtubejs.ContentResult{"videos": videos}}
	input := youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList)
	input = withEnabled(t, input, map[contract.ObservationKind][]string{
		contract.KindVideoList:  {"UC_TEST"},
		contract.KindShortsList: {},
	})
	output, err := NewContentRunner(fake, 10).Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if fake.calls != 1 || len(observations) != 1 || observations[0].ObservationKind != contract.KindVideoList {
		t.Fatalf("calls=%d observations=%#v", fake.calls, observations)
	}
}

func TestChannelRunnersKeepLiveAndMetadataEmissionsSeparate(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	fake := &channelFake{result: result}
	live, err := NewChannelLiveRunner(fake).Collect(context.Background(), youtubeInput(t,
		"UC_TEST", "youtubejs_channel_live", contract.KindLiveSnapshot,
	))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := NewChannelMetadataRunner(fake).Collect(context.Background(), youtubeInput(t,
		"UC_TEST", "youtubejs_channel_metadata",
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	))
	if err != nil {
		t.Fatal(err)
	}
	liveObservations := live.Output().Observations()
	metadataObservations := metadata.Output().Observations()
	if len(liveObservations) != 1 || liveObservations[0].ObservationKind != contract.KindLiveSnapshot {
		t.Fatalf("live observations = %#v", liveObservations)
	}
	if len(metadataObservations) != 3 {
		t.Fatalf("metadata observations = %#v", metadataObservations)
	}
}

func TestChannelRunnersSkipMissingLiveTabButKeepMetadata(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	result.MissingTab = true
	fake := &channelFake{result: result}
	live, err := NewChannelLiveRunner(fake).Collect(context.Background(), youtubeInput(t,
		"UC_TEST", "youtubejs_channel_live", contract.KindLiveSnapshot,
	))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := NewChannelMetadataRunner(fake).Collect(context.Background(), youtubeInput(t,
		"UC_TEST", "youtubejs_channel_metadata",
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	))
	if err != nil {
		t.Fatal(err)
	}
	if !live.Output().Empty() {
		t.Fatalf("missing live tab published %#v", live.Output().Observations())
	}
	if len(metadata.Output().Observations()) != 3 {
		t.Fatalf("metadata observations = %#v", metadata.Output().Observations())
	}
}

func TestChannelPhotoDoesNotFetchMediaOrSynthesizeFingerprint(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	fake := &channelFake{result: result}
	output, err := NewChannelMetadataRunner(fake).Collect(context.Background(), youtubeInput(t,
		"UC_TEST", "youtubejs_channel_metadata",
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	))
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("channel fetch calls = %d, want 1", fake.calls)
	}
	var payload contract.ChannelPhotoV1
	found := false
	observations := output.Output().Observations()
	for i := range observations {
		if observations[i].ObservationKind != contract.KindChannelPhoto {
			continue
		}
		if err := json.Unmarshal(observations[i].Payload, &payload); err != nil {
			t.Fatal(err)
		}
		found = true
	}
	if !found {
		t.Fatal("missing channel_photo observation")
	}
	if len(payload.Variants) == 0 {
		t.Fatal("expected photo variants")
	}
	for _, variant := range payload.Variants {
		if variant.StableMediaID != "" || variant.ContentFingerprint != "" {
			t.Fatalf("collector synthesized photo identity: %#v", variant)
		}
	}
}

func TestChannelRunnerEmitsOnlyEnabledKinds(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	fake := &channelFake{result: result}
	input := youtubeInput(t,
		"UC_TEST", "youtubejs_channel_metadata",
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	)
	input = withEnabled(t, input, map[contract.ObservationKind][]string{
		contract.KindChannelStats:   {"UC_TEST"},
		contract.KindChannelProfile: {},
		contract.KindChannelPhoto:   {},
	})
	output, err := NewChannelMetadataRunner(fake).Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if fake.calls != 1 || len(observations) != 1 || observations[0].ObservationKind != contract.KindChannelStats {
		t.Fatalf("calls=%d observations=%#v", fake.calls, observations)
	}
}

func TestViewerRunnerRejectsChannelSubject(t *testing.T) {
	t.Parallel()
	runner := NewViewerRunner(&viewerFake{})
	_, err := runner.Collect(context.Background(), youtubeInput(t,
		"UCoperationalchannel0001", "youtubejs_viewer", contract.KindViewerSample,
	))
	if err == nil || !strings.Contains(err.Error(), "video id") {
		t.Fatalf("error = %v, want video id rejection", err)
	}
}

func TestViewerRunnerKeepsHiddenCountTyped(t *testing.T) {
	t.Parallel()
	var result youtubejs.ViewerResult
	loadJSON(t, "viewer_hidden.json", &result)
	runner := NewViewerRunner(&viewerFake{result: result})
	output, err := runner.Collect(context.Background(), youtubeInput(t, "vid-1", "youtubejs_viewer", contract.KindViewerSample))
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want exactly one", observations)
		return
	}
	var payload contract.ViewerSampleV1
	if err := json.Unmarshal(observations[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Availability != "HIDDEN" || payload.ViewerCount != nil {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestViewerRunnerRejectsMismatchedResponseIdentity(t *testing.T) {
	t.Parallel()
	result := youtubejs.ViewerResult{VideoID: "different-video"}
	output, err := NewViewerRunner(&viewerFake{result: result}).Collect(
		context.Background(), youtubeInput(t, "requested-video", "youtubejs_viewer", contract.KindViewerSample),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestContentRunnerRejectsMismatchedResponseIdentity(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	videos.Items[0].ChannelID = "UC_OTHER"
	fake := &contentFake{results: map[string]youtubejs.ContentResult{"videos": videos}}
	output, err := NewContentRunner(fake, 10).Collect(
		context.Background(), youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestChannelRunnerRejectsMismatchedLiveIdentity(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	result.LiveSessions[0].ChannelID = "UC_OTHER"
	output, err := NewChannelLiveRunner(&channelFake{result: result}).Collect(
		context.Background(), youtubeInput(t, "UC_TEST", "youtubejs_channel_live", contract.KindLiveSnapshot),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestCommunityRunnerRejectsNullRows(t *testing.T) {
	t.Parallel()
	result := youtubejs.CommunityResult{
		Posts: []*parser.CommunityPost{nil},
		Pagination: youtubejs.Pagination{
			PageCount: 1, Exhausted: true, Continuity: string(contract.ContinuityContiguous),
			TerminationReason: youtubejs.TerminationExhausted,
		},
	}
	output, err := NewCommunityRunner(&communityFake{result: result}, 10).Collect(
		context.Background(), youtubeInput(t, "UC_TEST", "community_collect", contract.KindCommunityPage),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestViewerRunnerSameSlotRetryKeepsSampleIdentity(t *testing.T) {
	t.Parallel()
	var result youtubejs.ViewerResult
	loadJSON(t, "viewer_hidden.json", &result)
	runner := NewViewerRunner(&viewerFake{result: result})
	input := youtubeInput(t, "vid-1", "youtubejs_viewer", contract.KindViewerSample)
	first, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var payload contract.ViewerSampleV1
	firstObservations := first.Output().Observations()
	secondObservations := second.Output().Observations()
	if len(firstObservations) != 1 || len(secondObservations) != 1 {
		t.Fatalf("first=%#v second=%#v, want one observation each", firstObservations, secondObservations)
		return
	}
	if err := json.Unmarshal(firstObservations[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.SampleWindowStart.Equal(input.Lease().ScheduledFor) {
		t.Fatalf("sample window = %s, want lease %s", payload.SampleWindowStart, input.Lease().ScheduledFor)
	}
	if firstObservations[0].ObservationKey != secondObservations[0].ObservationKey {
		t.Fatalf("retry changed observation key %s vs %s", firstObservations[0].ObservationKey, secondObservations[0].ObservationKey)
	}
}

func TestContentRunnerDoesNotPublishOnParserDrift(t *testing.T) {
	t.Parallel()
	runner := NewContentRunner(&contentFake{err: collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "content row is missing video id")}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput(t, "UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList))
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestCommunityRunnerDoesNotPublishOnFetchError(t *testing.T) {
	t.Parallel()
	runner := NewCommunityRunner(&communityFake{err: collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "helper timeout")}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput(t, "UC_FAIL", "community_collect", contract.KindCommunityPage))
	if err == nil || collecterr.CodeOf(err) != collecterr.Timeout || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func mustCollectCommunity(t *testing.T, result *youtubejs.CommunityResult) contract.Envelope {
	t.Helper()
	output, err := NewCommunityRunner(&communityFake{result: *result}, 10).Collect(
		context.Background(), youtubeInput(t, "UC_TEST", "community_collect", contract.KindCommunityPage),
	)
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want exactly one", observations)
		return contract.Envelope{}
	}
	return observations[0]
}

func youtubeInput(t testing.TB, subject, jobKind string, kinds ...contract.ObservationKind) *collectutil.RunInput {
	t.Helper()
	generations := make(map[contract.ObservationKind]int64, len(kinds))
	for _, kind := range kinds {
		generations[kind] = 1
	}
	spec := joblease.JobSpec{
		JobKey: "collector:youtubejs:" + jobKind + ":" + subject, Provider: contract.ProviderYouTubeJS,
		Class: "SUBJECT", CollectionJobKind: jobKind, SubjectKey: subject, PollInterval: time.Minute,
	}
	lease := contract.LeaseProof{
		JobKey: "collector:youtubejs:" + jobKind + ":" + subject, CollectionJobKind: jobKind,
		OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
		ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: sourceobservation.JobKind(jobKind)})
	snapshot, err := collectutil.NewContractSnapshot(kinds, generations)
	if err != nil {
		t.Fatal(err)
	}
	enabled := make(map[contract.ObservationKind][]string, len(job.RequestedKinds()))
	for _, kind := range job.RequestedKinds() {
		enabled[kind] = []string{subject}
	}
	targets := testutil.TargetSnapshot(t, &spec, job, enabled)
	lease.ProjectionGeneration = targets.Generation()
	input, err := collectutil.NewRunInput(&spec, &lease, snapshot, targets, 1, 1<<20, job)
	if err != nil {
		t.Fatal(err)
	}
	return &input
}

func withEnabled(t testing.TB, input *collectutil.RunInput, enabled map[contract.ObservationKind][]string) *collectutil.RunInput {
	t.Helper()
	job := input.Job()
	generations := make(map[contract.ObservationKind]int64, len(job.Emissions()))
	for _, kind := range job.Emissions() {
		generation, err := input.Generation(kind)
		if err != nil {
			t.Fatal(err)
		}
		generations[kind] = generation
	}
	snapshot, err := collectutil.NewContractSnapshot(job.Emissions(), generations)
	if err != nil {
		t.Fatal(err)
	}
	inputSpec := input.Spec()
	targets := testutil.TargetSnapshot(t, &inputSpec, job, enabled)
	lease := input.Lease()
	lease.ProjectionGeneration = targets.Generation()
	result, err := collectutil.NewRunInput(
		&inputSpec, &lease, snapshot, targets,
		input.MaxPages(), input.MaxSuccessResponseBytes(), job,
	)
	if err != nil {
		t.Fatal(err)
	}
	return &result
}

func loadJSON(t *testing.T, name string, dest any) {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS("testdata"), name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatal(err)
	}
}

type communityFake struct {
	result youtubejs.CommunityResult
	err    error
}

func (f *communityFake) FetchCommunity(context.Context, youtubejs.CommunityRequest) (youtubejs.CommunityResult, error) {
	return f.result, f.err
}

type contentFake struct {
	results   map[string]youtubejs.ContentResult
	errByKind map[string]error
	calls     int
	err       error
}

func (f *contentFake) FetchContent(_ context.Context, request youtubejs.ContentRequest) (youtubejs.ContentResult, error) {
	f.calls++
	if f.err != nil {
		return youtubejs.ContentResult{}, f.err
	}
	if err := f.errByKind[request.Kind]; err != nil {
		return youtubejs.ContentResult{}, err
	}
	return f.results[request.Kind], nil
}

type channelFake struct {
	result youtubejs.ChannelResult
	calls  int
}

func (f *channelFake) FetchChannel(context.Context, youtubejs.ChannelRequest) (youtubejs.ChannelResult, error) {
	f.calls++
	return f.result, nil
}

type viewerFake struct {
	result youtubejs.ViewerResult
}

func (f *viewerFake) FetchViewer(context.Context, youtubejs.ViewerRequest) (youtubejs.ViewerResult, error) {
	return f.result, nil
}
