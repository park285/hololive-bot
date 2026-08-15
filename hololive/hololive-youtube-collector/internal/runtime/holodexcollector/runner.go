package holodexcollector

import (
	"context"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

type Fetcher interface {
	Fetch(ctx context.Context) ([]byte, error)
}

type Runner struct {
	client      Fetcher
	jobKind     string
	emissions   []contract.ObservationKind
	targetKinds []contract.ObservationKind
	rosterKinds []contract.ObservationKind
}

func NewLiveRunner(client Fetcher) *Runner {
	return &Runner{
		client:      client,
		jobKind:     "holodex_live",
		emissions:   []contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
		targetKinds: []contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
		rosterKinds: []contract.ObservationKind{contract.KindLiveSnapshot},
	}
}

func NewMetadataRunner(client Fetcher) *Runner {
	return &Runner{
		client:      client,
		jobKind:     "holodex_metadata",
		emissions:   []contract.ObservationKind{contract.KindChannelStats, contract.KindChannelPhoto},
		targetKinds: []contract.ObservationKind{contract.KindChannelStats, contract.KindChannelPhoto},
		rosterKinds: []contract.ObservationKind{contract.KindChannelStats, contract.KindChannelPhoto},
	}
}

func NewScheduleRunner(client Fetcher) *Runner {
	return &Runner{
		client:      client,
		jobKind:     "holodex_schedule",
		emissions:   []contract.ObservationKind{contract.KindSchedule},
		targetKinds: []contract.ObservationKind{contract.KindSchedule, contract.KindLiveSnapshot},
		rosterKinds: []contract.ObservationKind{contract.KindLiveSnapshot},
	}
}

func (r *Runner) Provider() contract.Provider { return contract.ProviderHolodex }
func (r *Runner) JobKind() string             { return r.jobKind }
func (r *Runner) Emissions() []contract.ObservationKind {
	return append([]contract.ObservationKind(nil), r.emissions...)
}
func (r *Runner) TargetKinds() []contract.ObservationKind {
	return append([]contract.ObservationKind(nil), r.targetKinds...)
}

func (r *Runner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "holodex client is not configured")
	}
	if err := collectutil.ValidateInput(input); err != nil {
		return collectutil.RunOutput{}, err
	}
	started := time.Now()
	body, err := r.client.Fetch(ctx)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	rows, err := parseLiveRows(body)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	envelopes, err := r.buildBatch(input, rows)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	return collectutil.Output(envelopes, started)
}

func (r *Runner) buildBatch(input *collectutil.RunInput, rows []parsedLive) ([]contract.Envelope, error) {
	allowed := requestedSet(r.requestedIDs(input))
	envelopes, err := r.channelEnvelopes(input, groupByRequestedChannel(rows, allowed))
	if err != nil {
		return nil, err
	}
	viewers, err := r.viewerEnvelopes(input, rows, allowed)
	if err != nil {
		return nil, err
	}
	schedule, err := r.scheduleEnvelope(input, rows, allowed)
	if err != nil {
		return nil, err
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
		sessions := byChannel[channelID]
		added, err := r.appendChannelKind(input, envelopes, contract.KindLiveSnapshot, channelID, livePayload(channelID, sessions), true)
		if err != nil {
			return nil, err
		}
		envelopes = added
		stats, ok := statsPayload(channelID, sessions)
		added, err = r.appendChannelKind(input, envelopes, contract.KindChannelStats, channelID, stats, ok)
		if err != nil {
			return nil, err
		}
		envelopes = added
		photo, ok := photoPayload(channelID, sessions)
		added, err = r.appendChannelKind(input, envelopes, contract.KindChannelPhoto, channelID, photo, ok)
		if err != nil {
			return nil, err
		}
		envelopes = added
	}
	return envelopes, nil
}

func (r *Runner) appendChannelKind(
	input *collectutil.RunInput,
	envelopes []contract.Envelope,
	kind contract.ObservationKind,
	channelID string,
	payload any,
	ok bool,
) ([]contract.Envelope, error) {
	if !r.emits(kind) || !ok || !subjectAllowed(input, kind, channelID) {
		return envelopes, nil
	}
	envelope, err := r.envelope(input, kind, channelID, payload)
	if err != nil {
		return nil, err
	}
	return append(envelopes, envelope), nil
}

func (r *Runner) viewerEnvelopes(
	input *collectutil.RunInput,
	rows []parsedLive,
	allowed map[string]struct{},
) ([]contract.Envelope, error) {
	if !r.emits(contract.KindViewerSample) {
		return nil, nil
	}
	windowStart := input.Lease.ScheduledFor.UTC()
	windowSeconds := collectutil.SampleWindowSeconds(input.Spec.PollInterval)
	envelopes := make([]contract.Envelope, 0)
	for i := range rows {
		row := &rows[i]
		if _, ok := allowed[row.channelID]; !ok || !subjectAllowed(input, contract.KindViewerSample, row.row.ID) {
			continue
		}
		payload, err := viewerPayload(row, windowStart, windowSeconds)
		if err != nil {
			return nil, err
		}
		envelope, err := r.envelope(input, contract.KindViewerSample, row.row.ID, payload)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (r *Runner) scheduleEnvelope(
	input *collectutil.RunInput,
	rows []parsedLive,
	allowed map[string]struct{},
) (*contract.Envelope, error) {
	if !r.emits(contract.KindSchedule) || !subjectAllowed(input, contract.KindSchedule, officialScheduleSubject) {
		return nil, nil
	}
	payload := schedulePayload(rows, allowed)
	if len(payload.Items) == 0 {
		return nil, nil
	}
	envelope, err := r.envelope(input, contract.KindSchedule, officialScheduleSubject, payload)
	if err != nil {
		return nil, err
	}
	return &envelope, nil
}

func (r *Runner) requestedIDs(input *collectutil.RunInput) []string {
	var ids []string
	for _, kind := range r.rosterKinds {
		ids = append(ids, input.EnabledSubjects[kind]...)
	}
	return collectutil.UniqueSorted(ids)
}

func (r *Runner) emits(kind contract.ObservationKind) bool {
	return slices.Contains(r.emissions, kind)
}

func (r *Runner) envelope(input *collectutil.RunInput, kind contract.ObservationKind, subject string, payload any) (contract.Envelope, error) {
	generation, err := collectutil.Generation(input, kind)
	if err != nil {
		return contract.Envelope{}, err
	}
	envelope, err := collectutil.Envelope(
		contract.ProviderHolodex,
		kind,
		subject,
		generation,
		&input.Lease,
		contract.CompletenessPartial,
		contract.ContinuityNotApplicable,
		payload,
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, err)
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
