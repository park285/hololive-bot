package youtubejscollector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

func TestCommunityRunnerPublishesExhaustedFixture(t *testing.T) {
	t.Parallel()
	var result youtubejs.CommunityResult
	loadJSON(t, "community.json", &result)
	runner := NewCommunityRunner(&communityFake{result: result}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput("UC_TEST", "community_collect", contract.KindCommunityPage))
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Observations) != 1 || output.Observations[0].Completeness != contract.CompletenessComplete {
		t.Fatalf("output = %#v", output.Observations)
	}
}

func TestCommunityRunnerSkipsMissingTab(t *testing.T) {
	t.Parallel()
	runner := NewCommunityRunner(&communityFake{result: youtubejs.CommunityResult{MissingTab: true}}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput("UC_NONE", "community_collect", contract.KindCommunityPage))
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Observations) != 0 {
		t.Fatalf("missing tab published %#v", output.Observations)
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
	first := mustCollectCommunity(t, result)
	second := mustCollectCommunity(t, reversed)
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
	output, err := runner.Collect(context.Background(), youtubeInput("UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList))
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 || len(output.Observations) != 2 {
		t.Fatalf("calls=%d observations=%d", fake.calls, len(output.Observations))
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
	output, err := runner.Collect(context.Background(), youtubeInput("UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList))
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Observations) != 1 || output.Observations[0].ObservationKind != contract.KindVideoList {
		t.Fatalf("observations = %#v", output.Observations)
	}
}

func TestContentRunnerFetchesAndEmitsOnlyEnabledKind(t *testing.T) {
	t.Parallel()
	var videos youtubejs.ContentResult
	loadJSON(t, "videos.json", &videos)
	fake := &contentFake{results: map[string]youtubejs.ContentResult{"videos": videos}}
	input := youtubeInput("UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList)
	input.EnabledSubjects = map[contract.ObservationKind][]string{
		contract.KindVideoList:  {"UC_TEST"},
		contract.KindShortsList: {},
	}
	output, err := NewContentRunner(fake, 10).Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || len(output.Observations) != 1 || output.Observations[0].ObservationKind != contract.KindVideoList {
		t.Fatalf("calls=%d observations=%#v", fake.calls, output.Observations)
	}
}

func TestChannelRunnersKeepLiveAndMetadataEmissionsSeparate(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	fake := &channelFake{result: result}
	live, err := NewChannelLiveRunner(fake).Collect(context.Background(), youtubeInput(
		"UC_TEST", "youtubejs_channel_live", contract.KindLiveSnapshot,
	))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := NewChannelMetadataRunner(fake).Collect(context.Background(), youtubeInput(
		"UC_TEST", "youtubejs_channel_metadata",
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Observations) != 1 || live.Observations[0].ObservationKind != contract.KindLiveSnapshot {
		t.Fatalf("live observations = %#v", live.Observations)
	}
	if len(metadata.Observations) != 3 {
		t.Fatalf("metadata observations = %#v", metadata.Observations)
	}
}

func TestChannelPhotoDoesNotFetchMediaOrSynthesizeFingerprint(t *testing.T) {
	t.Parallel()
	var result youtubejs.ChannelResult
	loadJSON(t, "channel.json", &result)
	fake := &channelFake{result: result}
	output, err := NewChannelMetadataRunner(fake).Collect(context.Background(), youtubeInput(
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
	for i := range output.Observations {
		if output.Observations[i].ObservationKind != contract.KindChannelPhoto {
			continue
		}
		if err := json.Unmarshal(output.Observations[i].Payload, &payload); err != nil {
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
	input := youtubeInput(
		"UC_TEST", "youtubejs_channel_metadata",
		contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto,
	)
	input.EnabledSubjects = map[contract.ObservationKind][]string{
		contract.KindChannelStats:   {"UC_TEST"},
		contract.KindChannelProfile: {},
		contract.KindChannelPhoto:   {},
	}
	output, err := NewChannelMetadataRunner(fake).Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || len(output.Observations) != 1 || output.Observations[0].ObservationKind != contract.KindChannelStats {
		t.Fatalf("calls=%d observations=%#v", fake.calls, output.Observations)
	}
}

func TestViewerRunnerRejectsChannelSubject(t *testing.T) {
	t.Parallel()
	runner := NewViewerRunner(&viewerFake{})
	_, err := runner.Collect(context.Background(), youtubeInput(
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
	output, err := runner.Collect(context.Background(), youtubeInput("vid-1", "youtubejs_viewer", contract.KindViewerSample))
	if err != nil {
		t.Fatal(err)
	}
	var payload contract.ViewerSampleV1
	if err := json.Unmarshal(output.Observations[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Availability != "HIDDEN" || payload.ViewerCount != nil {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestViewerRunnerSameSlotRetryKeepsSampleIdentity(t *testing.T) {
	t.Parallel()
	var result youtubejs.ViewerResult
	loadJSON(t, "viewer_hidden.json", &result)
	runner := NewViewerRunner(&viewerFake{result: result})
	input := youtubeInput("vid-1", "youtubejs_viewer", contract.KindViewerSample)
	first, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var payload contract.ViewerSampleV1
	if err := json.Unmarshal(first.Observations[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.SampleWindowStart.Equal(input.Lease.ScheduledFor) {
		t.Fatalf("sample window = %s, want lease %s", payload.SampleWindowStart, input.Lease.ScheduledFor)
	}
	if first.Observations[0].ObservationKey != second.Observations[0].ObservationKey {
		t.Fatalf("retry changed observation key %s vs %s", first.Observations[0].ObservationKey, second.Observations[0].ObservationKey)
	}
}

func TestContentRunnerDoesNotPublishOnParserDrift(t *testing.T) {
	t.Parallel()
	runner := NewContentRunner(&contentFake{err: collecterr.New(collecterr.ParserDrift, "content row is missing video id")}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput("UC_TEST", "youtubejs_content", contract.KindVideoList, contract.KindShortsList))
	if err == nil || collecterr.Code(err) != collecterr.ParserDrift || len(output.Observations) != 0 {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestCommunityRunnerDoesNotPublishOnFetchError(t *testing.T) {
	t.Parallel()
	runner := NewCommunityRunner(&communityFake{err: collecterr.New(collecterr.Timeout, "helper timeout")}, 10)
	output, err := runner.Collect(context.Background(), youtubeInput("UC_FAIL", "community_collect", contract.KindCommunityPage))
	if err == nil || collecterr.Code(err) != collecterr.Timeout || len(output.Observations) != 0 {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func mustCollectCommunity(t *testing.T, result youtubejs.CommunityResult) contract.Envelope {
	t.Helper()
	output, err := NewCommunityRunner(&communityFake{result: result}, 10).Collect(
		context.Background(), youtubeInput("UC_TEST", "community_collect", contract.KindCommunityPage),
	)
	if err != nil {
		t.Fatal(err)
	}
	return output.Observations[0]
}

func youtubeInput(subject, jobKind string, kinds ...contract.ObservationKind) collectutil.RunInput {
	generations := make(map[contract.ObservationKind]int64, len(kinds))
	for _, kind := range kinds {
		generations[kind] = 1
	}
	return collectutil.RunInput{
		Spec: joblease.JobSpec{
			JobKey: "collector:youtubejs:" + jobKind + ":" + subject, Provider: contract.ProviderYouTubeJS,
			Class: "SUBJECT", CollectionJobKind: jobKind, SubjectKey: subject, PollInterval: time.Minute,
		},
		Lease: contract.LeaseProof{
			JobKey: "collector:youtubejs:" + jobKind + ":" + subject, CollectionJobKind: jobKind,
			OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
			ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
		},
		ContractGenerations: generations,
		MaxPages:            1,
		MaxAggregateBytes:   1 << 20,
	}
}

func loadJSON(t *testing.T, name string, dest any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
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
	results map[string]youtubejs.ContentResult
	calls   int
	err     error
}

func (f *contentFake) FetchContent(_ context.Context, request youtubejs.ContentRequest) (youtubejs.ContentResult, error) {
	f.calls++
	if f.err != nil {
		return youtubejs.ContentResult{}, f.err
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
