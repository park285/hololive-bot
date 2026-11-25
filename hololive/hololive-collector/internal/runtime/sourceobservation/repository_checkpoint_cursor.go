package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func (r *Repository) LatestCheckpointCursor(
	ctx context.Context,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
) (jsontext.Value, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("load latest checkpoint cursor: %w", ErrInvalidRepository)
	}

	if !provider.Valid() || !kind.Valid() {
		return nil, fmt.Errorf("load latest checkpoint cursor: %w: provider or kind is invalid", ErrInvalidEnvelope)
	}

	if err := validateText("subject key", subjectKey, 256); err != nil {
		return nil, fmt.Errorf("load latest checkpoint cursor: %w: %w", ErrInvalidEnvelope, err)
	}

	var cursor []byte

	err := pgxdb.QueryRow(ctx, r.pool, latestCheckpointCursorSQL, string(provider), string(kind), subjectKey).Scan(&cursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("load latest checkpoint cursor: %w", err)
	}

	if cursor == nil {
		return nil, nil
	}

	return jsontext.Value(cursor), nil
}
