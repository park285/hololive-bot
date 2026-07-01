package lifecycle

import "context"

type IrisPinger interface {
	Ping(ctx context.Context) bool
}

type Stoppable interface {
	Stop()
}
