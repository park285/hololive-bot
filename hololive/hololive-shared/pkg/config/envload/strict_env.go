package envload

import (
	"errors"
	"time"

	sharedenv "github.com/park285/shared-go/core/envutil"
)

type StrictEnv struct {
	errs []error
}

func (e *StrictEnv) Int(key string, def int) int {
	return e.keep(sharedenv.IntE(key, def))
}

func (e *StrictEnv) Bool(key string, def bool) bool {
	return e.keep(sharedenv.BoolE(key, def))
}

func (e *StrictEnv) Seconds(key string, def time.Duration) time.Duration {
	return e.keep(StrictDurationUnitEnv(key, def, time.Second))
}

func (e *StrictEnv) Millis(key string, def time.Duration) time.Duration {
	return e.keep(StrictDurationUnitEnv(key, def, time.Millisecond))
}

func (e *StrictEnv) Err() error {
	return errors.Join(e.errs...)
}

func (e *StrictEnv) keep[T any](value T, err error) T {
	if err != nil {
		e.errs = append(e.errs, err)
	}

	return value
}
