package sourceobservation

import (
	"context"
	"fmt"
)

func (r *Repository) ListRetentionCandidates(
	ctx context.Context,
	query RetentionQuery,
) ([]int64, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if !query.Kind.Valid() || query.Before.IsZero() || query.Limit < 1 || query.Limit > 1000 {
		return nil, fmt.Errorf("list source observation retention candidates: invalid query")
	}
	rows, err := r.pool.Query(
		ctx,
		mustSQL("repository_retention_candidates_0027_27.sql"),
		query.Kind,
		query.Before,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list source observation retention candidates: %w", err)
	}
	defer rows.Close()
	result := make([]int64, 0, query.Limit)
	for rows.Next() {
		var observationID int64
		if err := rows.Scan(&observationID); err != nil {
			return nil, fmt.Errorf("list source observation retention candidates: scan: %w", err)
		}
		result = append(result, observationID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list source observation retention candidates: iterate: %w", err)
	}
	return result, nil
}
