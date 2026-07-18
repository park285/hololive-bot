package dispatchrun

import "context"

type Scheduler interface {
	Start(ctx context.Context) error
}
