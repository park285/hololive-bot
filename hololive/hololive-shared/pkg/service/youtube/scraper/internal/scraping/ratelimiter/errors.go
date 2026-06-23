package ratelimiter

import "errors"

func IsDistributedLimiterUnavailable(err error) bool {
	return errors.Is(err, ErrDistributedLimiterUnavailable)
}
