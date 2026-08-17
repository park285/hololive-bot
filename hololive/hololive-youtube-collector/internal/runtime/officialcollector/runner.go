package officialcollector

import (
	"context"
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
	client Fetcher
}

func NewRunner(client Fetcher) *Runner {
	return &Runner{client: client}
}

func (r *Runner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderHololiveOfficial, Kind: "official_schedule"}
}

func (r *Runner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "official schedule client is not configured")
	}
	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}
	started := time.Now()
	body, err := r.client.Fetch(ctx)
	if err != nil {
		return collectutil.CollectResult{}, err
	}
	payload, err := parseScheduleSnapshot(body)
	if err != nil {
		return collectutil.CollectResult{}, err
	}
	generation, err := input.Generation(contract.KindSchedule)
	if err != nil {
		return collectutil.CollectResult{}, err
	}
	lease := input.Lease()
	envelope, err := collectutil.Envelope(
		contract.ProviderHololiveOfficial,
		contract.KindSchedule,
		officialScheduleSubject,
		generation,
		&lease,
		contract.CompletenessComplete,
		contract.ContinuityNotApplicable,
		payload,
	)
	if err != nil {
		return collectutil.CollectResult{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}
	return collectutil.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
}
