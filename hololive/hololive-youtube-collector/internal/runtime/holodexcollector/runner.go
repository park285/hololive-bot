package holodexcollector

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

type Fetcher interface {
	Fetch(ctx context.Context) ([]byte, error)
}

type Runner struct {
	client  Fetcher
	jobKind string
}

func NewLiveRunner(client Fetcher) *Runner {
	return &Runner{
		client:  client,
		jobKind: "holodex_live",
	}
}

func NewMetadataRunner(client Fetcher) *Runner {
	return &Runner{
		client:  client,
		jobKind: "holodex_metadata",
	}
}

func NewScheduleRunner(client Fetcher) *Runner {
	return &Runner{
		client:  client,
		jobKind: "holodex_schedule",
	}
}

func (r *Runner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderHolodex, Kind: sourceobservation.JobKind(r.jobKind)}
}

func (r *Runner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "holodex client is not configured")
	}

	if input == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	body, err := r.client.Fetch(ctx)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("fetch: %w", err)
	}

	rows, err := parseLiveRows(body)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("parse live rows: %w", err)
	}

	envelopes, err := r.buildBatch(input, rows)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("build batch: %w", err)
	}

	out, err := collectutil.CompleteFromEnvelopes(envelopes, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func (r *Runner) buildBatch(input *collectutil.RunInput, rows []parsedLive) ([]contract.Envelope, error) {
	requested, err := r.requestedIDs(input)
	if err != nil {
		return nil, fmt.Errorf("requested IDs: %w", err)
	}

	allowed := requestedSet(requested)

	envelopes, err := r.channelEnvelopes(input, groupByRequestedChannel(rows, allowed))
	if err != nil {
		return nil, fmt.Errorf("channel envelopes: %w", err)
	}

	viewers, err := r.viewerEnvelopes(input, rows, allowed)
	if err != nil {
		return nil, fmt.Errorf("viewer envelopes: %w", err)
	}

	schedule, err := r.scheduleEnvelope(input, rows, allowed)
	if err != nil {
		return nil, fmt.Errorf("schedule envelope: %w", err)
	}

	envelopes = append(envelopes, viewers...)

	if schedule != nil {
		envelopes = append(envelopes, *schedule)
	}

	return envelopes, nil
}

func groupByRequestedChannel(rows []parsedLive, allowed map[string]struct{}) map[string][]parsedLive {
	byChannel := make(map[string][]parsedLive, len(allowed))

	for i := range rows {
		row := &rows[i]
		if _, ok := allowed[row.channelID]; !ok {
			continue
		}

		byChannel[row.channelID] = append(byChannel[row.channelID], *row)
	}

	return byChannel
}

func (r *Runner) channelEnvelopes(input *collectutil.RunInput, byChannel map[string][]parsedLive) ([]contract.Envelope, error) {
	envelopes := make([]contract.Envelope, 0)

	for _, channelID := range collectutil.UniqueSorted(keys(byChannel)) {
		added, err := r.channelEnvelopesFor(input, channelID, byChannel[channelID])
		if err != nil {
			return nil, fmt.Errorf("channel envelopes for: %w", err)
		}

		envelopes = append(envelopes, added...)
	}

	return envelopes, nil
}

func (r *Runner) channelEnvelopesFor(input *collectutil.RunInput, channelID string, sessions []parsedLive) ([]contract.Envelope, error) {
	envelopes, err := r.appendChannelKind(input, nil, contract.KindLiveSnapshot, channelID, livePayload(channelID, sessions), true)
	if err != nil {
		return nil, fmt.Errorf("append channel kind: %w", err)
	}

	stats, ok, err := statsPayload(channelID, sessions)
	if err != nil {
		return nil, fmt.Errorf("stats payload: %w", err)
	}

	envelopes, err = r.appendChannelKind(input, envelopes, contract.KindChannelStats, channelID, stats, ok)
	if err != nil {
		return nil, fmt.Errorf("append channel kind: %w", err)
	}

	photo, ok, err := photoPayload(channelID, sessions)
	if err != nil {
		return nil, fmt.Errorf("photo payload: %w", err)
	}

	out, err := r.appendChannelKind(input, envelopes, contract.KindChannelPhoto, channelID, photo, ok)
	if err != nil {
		return out, fmt.Errorf("append channel kind: %w", err)
	}

	return out, nil
}

func (r *Runner) appendChannelKind(
	input *collectutil.RunInput,
	envelopes []contract.Envelope,
	kind contract.ObservationKind,
	channelID string,
	payload any,
	ok bool,
) ([]contract.Envelope, error) {
	if !input.Job().Emits(kind) || !ok {
		return envelopes, nil
	}

	allowed, err := subjectAllowed(input, kind, channelID)
	if err != nil {
		return nil, fmt.Errorf("subject allowed: %w", err)
	}

	if !allowed {
		return envelopes, nil
	}

	envelope, err := r.envelope(input, kind, channelID, payload)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}

	return append(envelopes, envelope), nil
}

func (r *Runner) viewerEnvelopes(
	input *collectutil.RunInput,
	rows []parsedLive,
	allowed map[string]struct{},
) ([]contract.Envelope, error) {
	if !input.Job().Emits(contract.KindViewerSample) {
		return nil, nil
	}

	lease := input.Lease()
	windowStart := lease.ScheduledFor.UTC()
	windowSeconds := collectutil.SampleWindowSeconds(input.Spec().PollInterval)
	envelopes := make([]contract.Envelope, 0)

	for i := range rows {
		row := &rows[i]
		if _, ok := allowed[row.channelID]; !ok {
			continue
		}

		isAllowed, err := subjectAllowed(input, contract.KindViewerSample, row.row.ID)
		if err != nil {
			return nil, fmt.Errorf("subject allowed: %w", err)
		}

		if !isAllowed {
			continue
		}

		payload, err := viewerPayload(row, windowStart, windowSeconds)
		if err != nil {
			return nil, fmt.Errorf("viewer payload: %w", err)
		}

		envelope, err := r.envelope(input, contract.KindViewerSample, row.row.ID, payload)
		if err != nil {
			return nil, fmt.Errorf("envelope: %w", err)
		}

		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

//nolint:nilnil // 방출 대상이 아니면 봉투 없이 건너뛴다는 뜻이라 오류가 아니다.
func (r *Runner) scheduleEnvelope(
	input *collectutil.RunInput,
	rows []parsedLive,
	allowed map[string]struct{},
) (*contract.Envelope, error) {
	if !input.Job().Emits(contract.KindSchedule) {
		return nil, nil
	}

	allowedSubject, err := subjectAllowed(input, contract.KindSchedule, officialScheduleSubject)
	if err != nil {
		return nil, fmt.Errorf("subject allowed: %w", err)
	}

	if !allowedSubject {
		return nil, nil
	}

	payload := schedulePayload(rows, allowed)
	if len(payload.Items) == 0 {
		return nil, nil
	}

	envelope, err := r.envelope(input, contract.KindSchedule, officialScheduleSubject, payload)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}

	return &envelope, nil
}

func (r *Runner) requestedIDs(input *collectutil.RunInput) ([]string, error) {
	var ids []string

	for _, kind := range input.Job().RosterKinds() {
		subjects, err := input.Roster(kind)
		if err != nil {
			return nil, fmt.Errorf("roster: %w", err)
		}

		ids = append(ids, subjects...)
	}

	return collectutil.UniqueSorted(ids), nil
}

func (r *Runner) envelope(input *collectutil.RunInput, kind contract.ObservationKind, subject string, payload any) (contract.Envelope, error) {
	generation, err := input.Generation(kind)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("generation: %w", err)
	}

	lease := input.Lease()

	envelope, err := collectutil.Envelope(
		contract.ProviderHolodex,
		kind,
		subject,
		generation,
		&lease,
		contract.CompletenessPartial,
		contract.ContinuityNotApplicable,
		payload,
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}

	return envelope, nil
}

func keys(values map[string][]parsedLive) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}

	return result
}
