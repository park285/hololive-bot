package iris

import "context"

// Client: Iris 메시지 전송 인터페이스다.
type Client interface {
	SendMessage(ctx context.Context, room, message string, opts ...SendOption) error
	SendImage(ctx context.Context, room, imageBase64 string) error
	Ping(ctx context.Context) bool
	GetConfig(ctx context.Context) (*Config, error)
	Decrypt(ctx context.Context, data string) (string, error)
}

type SendOption func(*sendOptions)

type sendOptions struct {
	ThreadID *string
}

func WithThreadID(id string) SendOption {
	return func(o *sendOptions) { o.ThreadID = &id }
}

func applySendOptions(opts []SendOption) sendOptions {
	var o sendOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
