package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// LatestCheckpointCursor는 (provider, kind, subject)의 현재 observation contract generation에 속한 checkpoint 중
// 가장 최근 durable cursor를 읽는다. 이전 contract generation의 checkpoint는 보지 않으며, 현재 generation의
// checkpoint가 없거나 cursor가 NULL이면 nil을 반환한다. 읽기 전용이며 lease·publish 경계와 무관하다.
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

	err := r.pool.QueryRow(ctx, mustSQL("repository_checkpoint_cursor_0005_05.sql"), string(provider), string(kind), subjectKey).Scan(&cursor)
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
