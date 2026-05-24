package transport

import (
	"context"

	"github.com/park285/iris-client-go/iris"
)

// IrisTransportClient is the narrow interface that transport needs from the Iris client.
type IrisTransportClient interface {
	iris.Sender
	Ping(ctx context.Context) bool
	GetConfig(ctx context.Context) (*iris.ConfigResponse, error)
}
