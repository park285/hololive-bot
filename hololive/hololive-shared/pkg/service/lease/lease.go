package lease

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/retry"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	defaultRenewMaxAttempts = 3
	defaultRenewBaseDelay   = 1 * time.Second
	defaultRenewJitter      = 500 * time.Millisecond
)

var (
	ErrHeld          = errors.New("lease already held")
	ErrOwnershipLost = errors.New("lease ownership lost")
)

type Spec struct {
	Name             string
	Key              string
	Owner            string
	TTL              time.Duration
	RenewGap         time.Duration
	RenewMaxAttempts int
	RenewBaseDelay   time.Duration
	RenewJitter      time.Duration
}

type Lease struct {
	cache       cache.Client
	name        string
	key         string
	owner       string
	ttl         time.Duration
	renewGap    time.Duration
	maxAttempts int
	baseDelay   time.Duration
	jitter      time.Duration
	logger      *slog.Logger
	sleep       func(context.Context, time.Duration) bool
}

func Acquire(ctx context.Context, cacheClient cache.Client, spec *Spec, logger *slog.Logger) (*Lease, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("acquire lease: cache client must not be nil")
	}
	if spec == nil || spec.Key == "" || spec.Owner == "" {
		return nil, fmt.Errorf("acquire lease: key and owner are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	l := newLease(cacheClient, spec, logger)

	acquired, err := cacheClient.SetNX(ctx, l.key, l.owner, l.ttl)
	if err != nil {
		return nil, fmt.Errorf("acquire lease: setnx: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("acquire lease: %w: key=%s", ErrHeld, l.key)
	}
	return l, nil
}

func newLease(cacheClient cache.Client, spec *Spec, logger *slog.Logger) *Lease {
	maxAttempts := spec.RenewMaxAttempts
	if maxAttempts < 1 {
		maxAttempts = defaultRenewMaxAttempts
	}
	baseDelay := spec.RenewBaseDelay
	if baseDelay <= 0 {
		baseDelay = defaultRenewBaseDelay
	}
	jitter := spec.RenewJitter
	if jitter <= 0 {
		jitter = defaultRenewJitter
	}
	return &Lease{
		cache:       cacheClient,
		name:        spec.Name,
		key:         spec.Key,
		owner:       spec.Owner,
		ttl:         spec.TTL,
		renewGap:    spec.RenewGap,
		maxAttempts: maxAttempts,
		baseDelay:   baseDelay,
		jitter:      jitter,
		logger:      logger,
	}
}

func (l *Lease) Owner() string {
	if l == nil {
		return ""
	}
	return l.owner
}

func (l *Lease) Renew(ctx context.Context) error {
	if l == nil {
		return nil
	}
	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: l.maxAttempts,
		BaseDelay:   l.baseDelay,
		Jitter:      l.jitter,
		Sleep:       l.sleep,
		ShouldRetry: func(err error) bool {
			return !errors.Is(err, ErrOwnershipLost)
		},
		OnRetry: l.logRenewRetry,
	}, l.renewOnce)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("renew lease %q: %w", l.name, err)
}

func (l *Lease) renewOnce(ctx context.Context) error {
	renewed, err := l.cache.CompareAndExpire(ctx, l.key, l.owner, l.ttl)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	if !renewed {
		return fmt.Errorf("renew lease: %w: key=%s", ErrOwnershipLost, l.key)
	}
	return nil
}

func (l *Lease) logRenewRetry(attempt int, err error, delay time.Duration) {
	if l.logger == nil {
		return
	}
	l.logger.Warn("lease renew retrying",
		slog.String("lease", l.name),
		slog.String("key", l.key),
		slog.Int("attempt", attempt),
		slog.Duration("delay", delay),
		slog.Any("error", err),
	)
}

func (l *Lease) RenewLoop(ctx context.Context) error {
	if l == nil {
		return nil
	}
	ticker := time.NewTicker(l.renewGap)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := l.Renew(ctx); err != nil {
				return err
			}
		}
	}
}

func (l *Lease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	released, err := l.cache.CompareAndDelete(ctx, l.key, l.owner)
	if err != nil {
		return fmt.Errorf("release lease: compare-and-delete: %w", err)
	}
	if !released {
		return fmt.Errorf("release lease: ownership mismatch: key=%s", l.key)
	}
	return nil
}
