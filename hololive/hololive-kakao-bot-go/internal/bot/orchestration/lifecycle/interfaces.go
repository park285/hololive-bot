package lifecycle

import "context"

// IrisPinger is the narrow interface that lifecycle needs from the Iris client.
type IrisPinger interface {
	Ping(ctx context.Context) bool
}

// Stoppable is the narrow interface for stream runtimes that lifecycle can shut down.
type Stoppable interface {
	Stop()
}
