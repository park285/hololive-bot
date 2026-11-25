package iris

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	irisclient "github.com/park285/iris-client-go/v6/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type ManagedIrisClient interface {
	irisclient.BotClient
	irisclient.FileSender
	SendRichText(ctx context.Context, room, markdown string, opts ...irisclient.SendOption) (*irisclient.ReplyAcceptedResponse, error)
	GetRooms(ctx context.Context) (*irisclient.RoomListResponse, error)
	Close() error
}

func ProvideIrisClient(ctx context.Context, irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...irisclient.ClientOption) (ManagedIrisClient, error) {
	if irisConfig == nil {
		return nil, errors.New("provide iris client: iris config is required")
	}

	client, err := delivery.NewRuntimeIrisClient(ctx, *irisConfig, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("provide iris client: %w", err)
	}

	return client, nil
}
