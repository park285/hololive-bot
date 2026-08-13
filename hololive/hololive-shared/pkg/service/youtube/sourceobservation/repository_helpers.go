package sourceobservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func loadAuthority(
	ctx context.Context,
	db dbx.Querier,
	sourceKind contract.SourceKind,
	lock bool,
) (AuthorityFence, error) {
	if err := validateSourceKind(sourceKind); err != nil {
		return AuthorityFence{}, err
	}
	queryName := "repository_authority_load_0002_02.sql"
	if lock {
		queryName = "repository_authority_lock_0001_01.sql"
	}
	var mode string
	var generation int64
	err := db.QueryRow(ctx, mustSQL(queryName), sourceKind).Scan(&mode, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthorityFence{}, ErrAuthorityMissing
	}
	if err != nil {
		return AuthorityFence{}, fmt.Errorf("load source observation authority: %w", err)
	}
	fence := AuthorityFence{SourceKind: sourceKind, Mode: contract.AuthorityMode(mode), Generation: generation}
	if !fence.Mode.Valid() || fence.Generation <= 0 {
		return AuthorityFence{}, fmt.Errorf("load source observation authority: invalid persisted fence")
	}
	return fence, nil
}

func validateSourceKind(sourceKind contract.SourceKind) error {
	if sourceKind != contract.SourceKindYouTubeCommunity {
		return fmt.Errorf("unsupported source kind %q", sourceKind)
	}
	return nil
}

func newLeaseToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
