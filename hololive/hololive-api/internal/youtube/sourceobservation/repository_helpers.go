package sourceobservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"unicode/utf8"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func newLeaseToken() (string, error) {
	var token [32]byte

	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return hex.EncodeToString(token[:]), nil
}

func BoundedErrorDetail(value string) string {
	if len(value) <= maxErrorTextBytes {
		return value
	}

	end := maxErrorTextBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}

	return value[:end]
}

func lockLiveRuntimeState(ctx context.Context, tx dbx.Tx) error {
	if _, err := pgxdb.Exec(ctx, tx, repositoryLiveRuntimeLockSQL); err != nil {
		return fmt.Errorf("lock youtube live runtime state: %w", err)
	}

	return nil
}

func refreshLiveRuntimeState(ctx context.Context, tx dbx.Tx, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}

	if _, err := pgxdb.Exec(ctx, tx, repositoryLiveRuntimeRefreshSQL, videoIDs); err != nil {
		return fmt.Errorf("refresh youtube live runtime state: %w", err)
	}

	return nil
}
