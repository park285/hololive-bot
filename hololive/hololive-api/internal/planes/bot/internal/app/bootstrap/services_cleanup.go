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

	infraCleanup    func()
	stopMemberCache func()
	stopHolodex     func()
	irisClient      io.Closer
	internalClients []io.Closer
	closeErr        error
}

// Close는 background 사용자 종료 뒤 내부 client/Iris와 DB/cache를 한 번만 닫는다.
// 같은 owner가 부분 생성 rollback과 정상 plane Close를 맡는다.
func (o *botInfrastructureOwner) Close() error {
	o.once.Do(func() {
		if o.stopHolodex != nil {
			o.stopHolodex()
		}

		if o.stopMemberCache != nil {
			o.stopMemberCache()
		}

		var closeErrs []error

		if err := internalhttp.CloseAll(o.internalClients...); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("close bot internal clients: %w", err))
		}

		if o.irisClient != nil {
			if err := o.irisClient.Close(); err != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close bot Iris client: %w", err))
			}
		}

		if o.infraCleanup != nil {
			o.infraCleanup()
		}

		o.closeErr = errors.Join(closeErrs...)
	})

	return o.closeErr
}
