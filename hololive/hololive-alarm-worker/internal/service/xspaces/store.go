package xspaces

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type StartStore struct{ Pool *pgxpool.Pool }

func (s StartStore) Remember(ctx context.Context, payload domain.XSpaceDispatchPayload) (domain.XSpaceDispatchPayload, error) {
	raw, err := jsonv2.Marshal(payload)
	if err != nil {
		return payload, fmt.Errorf("encode X space start: %w", err)
	}

	if _, err := s.Pool.Exec(ctx, insertStartSQL, payload.SpaceID, raw); err != nil {
		return payload, fmt.Errorf("store X space start: %w", err)
	}

	if err := s.Pool.QueryRow(ctx, loadStartSQL, payload.SpaceID).Scan(&raw); err != nil {
		return payload, fmt.Errorf("read X space start: %w", err)
	}

	var stored domain.XSpaceDispatchPayload

	if err := jsonv2.Unmarshal(raw, &stored); err != nil {
		return payload, fmt.Errorf("decode X space start: %w", err)
	}

	if err := stored.Validate(); err != nil {
		return payload, fmt.Errorf("validate saved X space start: %w", err)
	}

	if stored.CreatorID != payload.CreatorID || stored.ChannelID != payload.ChannelID {
		return payload, errors.New("x space start identity changed")
	}

	return stored, nil
}

func (s StartStore) Prune(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, pruneStartsSQL)
	if err != nil {
		return fmt.Errorf("prune X space starts: %w", err)
	}

	return nil
}
