package cache

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

type CacheError struct {
	Operation string
	Key       string
	Err       error
}

func (e *CacheError) Error() string {
	if e == nil {
		return "cache: <nil>"
	}

	loggableKey := privacylog.RedactCacheKey(e.Key)

	if e.Err == nil {
		if e.Key == "" {
			return fmt.Sprintf("cache: %s", e.Operation)
		}

		return fmt.Sprintf("cache: %s: key=%s", e.Operation, loggableKey)
	}

	if e.Key == "" {
		return fmt.Sprintf("cache: %s: %v", e.Operation, e.Err)
	}

	return fmt.Sprintf("cache: %s: key=%s: %v", e.Operation, loggableKey, e.Err)
}

func (e *CacheError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func NewCacheError(operation, key string, cause error) *CacheError {
	return &CacheError{
		Operation: operation,
		Key:       key,
		Err:       cause,
	}
}
