package officialcollector

import (
	"context"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

type Fetcher interface {
	Fetch(ctx context.Context) ([]byte, error)
}

type Runner struct {
	client Fetcher
}

func NewRunner(client Fetcher) *Runner {
	return &Runner{client: client}
}

func (r *Runner) Provider() contract.Provider { return contract.ProviderHololiveOfficial }
func (r *Runner) JobKind() string             { return "official_schedule" }
func (r *Runner) Emissions() []contract.ObservationKind {
	return []contract.ObservationKind{contract.KindSchedule}
}

func (r *Runner) Collect(ctx context.Context, input collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "official schedule client is not configured")
	}
	started := time.Now()
	body, err := r.client.Fetch(ctx)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	payload, err := parseScheduleSnapshot(body)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	generation, err := collectutil.Generation(input, contract.KindSchedule)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	envelope, err := collectutil.Envelope(
		contract.ProviderHololiveOfficial,
		contract.KindSchedule,
		officialScheduleSubject,
		generation,
		input.Lease,
		contract.CompletenessComplete,
		contract.ContinuityNotApplicable,
		payload,
	)
	if err != nil {
		return collectutil.RunOutput{}, collecterr.Wrap(collecterr.ParserDrift, err)
	}
	return collectutil.Output([]contract.Envelope{envelope}, started)
}
