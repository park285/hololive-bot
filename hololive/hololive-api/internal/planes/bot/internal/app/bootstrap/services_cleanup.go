package bootstrap

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type botInfrastructureOwner struct {
	once sync.Once

	irisClient      io.Closer
	internalClients []io.Closer
	closeErr        error
}

func (o *botInfrastructureOwner) Close() error {
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

		o.closeErr = errors.Join(closeErrs...)
	})

	return o.closeErr
}
