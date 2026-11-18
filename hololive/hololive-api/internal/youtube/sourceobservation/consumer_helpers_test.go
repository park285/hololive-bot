package sourceobservation

import (
	"context"
	"errors"
	"fmt"
)

func (c *Consumer) Consume(ctx context.Context, options ClaimOptions) error {
	if c == nil || c.repo == nil {
		return ErrInvalidRepository
	}

	batch, err := c.repo.ClaimBatch(ctx, options)
	if err != nil {
		return fmt.Errorf("claim batch: %w", err)
	}

	var consumeErrors []error

	for i := range batch.Claims {
		if err := c.ConsumeClaim(ctx, batch.Claims[i].Claim(batch.ConsumerName)); err != nil {
			consumeErrors = append(consumeErrors, err)
		}
	}

	return errors.Join(consumeErrors...)
}
