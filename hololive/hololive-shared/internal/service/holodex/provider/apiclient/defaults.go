package apiclient

import "time"

const (
	retryBaseDelay = 500 * time.Millisecond
	retryJitter    = 250 * time.Millisecond
)

const (
	circuitFailureThreshold = 3
	circuitResetTimeout     = 30 * time.Second
)
