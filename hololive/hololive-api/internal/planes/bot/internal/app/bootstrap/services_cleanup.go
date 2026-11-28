package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type botInfrastructureOwner struct {
	once sync.Once

	irisClient      io.Closer
	internalClients []io.Closer
	durable         *durability.Store
	closeErr        error
}

func (o *botInfrastructureOwner) Close() error {
	return o.close(context.Background())
}

func (o *botInfrastructureOwner) close(ctx context.Context) error {
	o.once.Do(func() {
		var closeErrs []error

		if err := internalhttp.CloseAll(o.internalClients...); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("close bot internal clients: %w", err))
		}

		if o.irisClient != nil {
			if err := o.irisClient.Close(); err != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close bot Iris client: %w", err))
			}
		}

		if o.durable != nil {
			if err := o.durable.Close(ctx); err != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close bot durable store: %w", err))
			}
		}

		o.closeErr = errors.Join(closeErrs...)
	})

	return o.closeErr
}
