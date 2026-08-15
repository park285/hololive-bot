package settings

import (
	"errors"
	"fmt"
	"time"
)

func (c *YouTubePlaneConfig) Validate() error {
	if err := c.validatePool(); err != nil {
		return err
	}
	if err := c.validateConsumers(); err != nil {
		return err
	}
	if err := c.validateClaimAndTimeouts(); err != nil {
		return err
	}
	if err := c.validateProjectionAndFinalizer(); err != nil {
		return err
	}
	if err := c.validateRetentionAndReplay(); err != nil {
		return err
	}
	return c.validateStabilityPolicies()
}

func (c *YouTubePlaneConfig) validateRetentionAndReplay() error {
	if err := c.validateRetention(); err != nil {
		return err
	}
	return c.validateReplay()
}

func (c *YouTubePlaneConfig) validateStabilityPolicies() error {
	if err := c.validateContentAbsenceGrace(); err != nil {
		return err
	}
	if err := c.validateLiveEndGrace(); err != nil {
		return err
	}
	if err := c.validateProfileClear(); err != nil {
		return err
	}
	return c.validatePhotoChange()
}

func (c *YouTubePlaneConfig) validatePool() error {
	if c.PostgresPoolMinConns < 0 || c.PostgresPoolMaxConns <= 0 {
		return errors.New("youtube plane postgres pool bounds are invalid")
	}
	if c.PostgresPoolMaxConns > youtubePlaneMaxPoolConns {
		return errors.New("youtube plane postgres pool max must not exceed 16")
	}
	if c.PostgresPoolMinConns > c.PostgresPoolMaxConns {
		return errors.New("youtube plane postgres pool min exceeds max")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateConsumers() error {
	if c.ConsumerWorkers < 1 || c.ConsumerWorkers > youtubePlaneMaxConsumerWorkers {
		return errors.New("youtube plane consumer workers must be between 1 and 16")
	}
	if c.DBOperationConcurrency < 1 || c.DBOperationConcurrency >= c.PostgresPoolMaxConns {
		return errors.New("youtube plane DB operation concurrency must leave one pool connection reserved")
	}
	if c.ConsumerWorkers > c.DBOperationConcurrency {
		return errors.New("youtube plane consumers exceed the shared DB operation budget")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateClaimAndTimeouts() error {
	if c.ClaimBatchSize < 1 || c.ClaimBatchSize > youtubePlaneMaxClaimBatchSize {
		return errors.New("youtube plane claim batch must be between 1 and 100")
	}
	if err := c.validateTransactionTimeout(); err != nil {
		return err
	}
	minimumLease := time.Duration(c.ClaimBatchSize)*c.TransactionTimeout + 10*time.Second
	if c.ClaimLease < minimumLease {
		return fmt.Errorf(
			"youtube plane claim lease must be at least %s for the configured batch",
			minimumLease,
		)
	}
	return c.validateShutdownWindows()
}

func (c *YouTubePlaneConfig) validateTransactionTimeout() error {
	if c.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}
	if c.TransactionTimeout < time.Second || c.TransactionTimeout > time.Minute {
		return errors.New("youtube plane transaction timeout must be between 1s and 1m")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateShutdownWindows() error {
	if c.ClaimInterval <= 0 {
		return errors.New("youtube plane claim interval must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("youtube plane shutdown timeout must be positive")
	}
	if c.ShutdownTimeout/2 < c.TransactionTimeout {
		return errors.New("youtube plane shutdown timeout must cover transaction and claim release timeouts")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateProjectionAndFinalizer() error {
	if c.TargetProjection.Interval <= 0 {
		return errors.New("youtube plane target projection interval must be positive")
	}
	if c.TargetProjection.Validity < 5*time.Second || c.TargetProjection.Validity > 24*time.Hour {
		return errors.New("youtube plane target projection validity must be between 5s and 24h")
	}
	if c.LiveEndFinalizer.Enabled && c.LiveEndFinalizer.Interval <= 0 {
		return errors.New("youtube plane live end finalizer interval must be positive when enabled")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateContentAbsenceGrace() error {
	if c.ContentAbsenceGrace < 0 || c.ContentAbsenceGrace > 24*time.Hour {
		return errors.New("youtube plane content absence grace must be between 0 and 24h")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateLiveEndGrace() error {
	if c.LiveEndGrace < 0 || c.LiveEndGrace > 24*time.Hour {
		return errors.New("youtube plane live end grace must be between 0 and 24h")
	}
	return nil
}

func (c *YouTubePlaneConfig) validateProfileClear() error {
	return validateStabilityPair(
		"profile clear",
		c.ProfileClearMinObservations,
		c.ProfileClearStability,
	)
}

func (c *YouTubePlaneConfig) validatePhotoChange() error {
	return validateStabilityPair(
		"photo change",
		c.PhotoChangeMinObservations,
		c.PhotoChangeStability,
	)
}

func validateStabilityPair(name string, minObservations int, stability time.Duration) error {
	if minObservations < 0 || minObservations > 100 {
		return fmt.Errorf("youtube plane %s min observations must be between 0 and 100", name)
	}
	if stability < 0 || stability > 24*time.Hour {
		return fmt.Errorf("youtube plane %s stability must be between 0 and 24h", name)
	}
	enabledCount := minObservations > 0
	enabledDuration := stability > 0
	if enabledCount != enabledDuration {
		return fmt.Errorf("youtube plane %s min observations and stability must be enabled together", name)
	}
	if enabledCount && minObservations < 2 {
		return fmt.Errorf("youtube plane %s min observations must be at least 2 when enabled", name)
	}
	return nil
}
