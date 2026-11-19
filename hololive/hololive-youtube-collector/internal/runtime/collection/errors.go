package collection

import (
	"errors"
	"time"
)

const (
	MaxPublishBatchSize  = 1024
	MaxCollectionLatency = 24 * time.Hour
)

var (
	ErrFenceLost       = errors.New("collection job fence was lost")
	ErrProjectionStale = errors.New("collection projection is stale")
	ErrTargetDisabled  = errors.New("collection target is disabled")
)
