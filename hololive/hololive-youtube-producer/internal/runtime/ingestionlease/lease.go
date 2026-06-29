// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package ingestionlease

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/lease"
)

const (
	Key                    = "lock:ingestion:runtime"
	ingestionLeaseTTL      = 2 * time.Minute
	ingestionLeaseRenewGap = 40 * time.Second
)

type Lease struct {
	inner  *lease.Lease
	key    string
	owner  string
	role   string
	logger *slog.Logger
}

func Acquire(ctx context.Context, cacheClient cache.Client, role string, logger *slog.Logger) (*Lease, error) {
	if role == "" {
		return nil, fmt.Errorf("acquire ingestion lease: role must not be empty")
	}
	if logger == nil {
		logger = slog.Default()
	}

	owner := newOwnerToken(role)
	inner, err := lease.Acquire(ctx, cacheClient, &lease.Spec{
		Name:     "ingestion",
		Key:      Key,
		Owner:    owner,
		TTL:      ingestionLeaseTTL,
		RenewGap: ingestionLeaseRenewGap,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("acquire ingestion lease: %w", err)
	}

	logger.Info("Ingestion lease acquired",
		slog.String("event", "ingestion_lease_acquired"),
		slog.String("role", role),
		slog.String("key", Key),
		slog.String("owner", owner),
	)

	return &Lease{inner: inner, key: Key, owner: owner, role: role, logger: logger}, nil
}

func (l *Lease) StartRenewLoop(ctx context.Context, errCh chan<- error) {
	if l == nil {
		return
	}
	if err := l.inner.RenewLoop(ctx); err != nil {
		l.handleRenewError(errCh, err)
	}
}

func (l *Lease) handleRenewError(errCh chan<- error, err error) {
	if errors.Is(err, lease.ErrOwnershipLost) {
		l.logger.Error("Ingestion lease ownership lost",
			slog.String("event", "ingestion_lease_lost"),
			slog.String("role", l.role),
			slog.String("key", l.key),
			slog.String("owner", l.owner),
			slog.Any("error", err),
		)
		l.reportRenewLoopError(errCh, fmt.Errorf("ingestion lease ownership lost: %w", err))
		return
	}

	l.logger.Error("Ingestion lease renew exhausted all retries",
		slog.String("event", "ingestion_lease_renew_failed"),
		slog.String("role", l.role),
		slog.String("key", l.key),
		slog.Any("error", err),
	)
	l.reportRenewLoopError(errCh, fmt.Errorf("ingestion lease renew failed: %w", err))
}

func (l *Lease) reportRenewLoopError(errCh chan<- error, err error) {
	if errCh == nil {
		return
	}

	select {
	case errCh <- err:
	default:
	}
}

func (l *Lease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if err := l.inner.Release(ctx); err != nil {
		return fmt.Errorf("release ingestion lease: %w", err)
	}

	l.logger.Info("Ingestion lease released",
		slog.String("event", "ingestion_lease_released"),
		slog.String("role", l.role),
		slog.String("key", l.key),
		slog.String("owner", l.owner),
	)
	return nil
}
