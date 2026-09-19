package xspaces

import (
	"context"
	"embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlFiles embed.FS
var mustSQL = sqlassets.MustReader(sqlFiles, "queries")

// StartStore는 첫 관측의 표시 정보를 고정하여 제목 변경에 따른 원장 충돌을 막는다.
type StartStore struct{ Pool *pgxpool.Pool }

// Remember는 같은 스페이스의 최초 스냅샷을 반환한다. 신규 방은 이 자료로만 수렴한다.
func (s StartStore) Remember(ctx context.Context, payload domain.XSpaceDispatchPayload) (domain.XSpaceDispatchPayload, error) {
	raw, err := jsonv2.Marshal(payload)
	if err != nil {
		return payload, fmt.Errorf("encode X space start: %w", err)
	}

	if _, err := s.Pool.Exec(ctx, mustSQL("insert_start.sql"), payload.SpaceID, raw); err != nil {
		return payload, fmt.Errorf("store X space start: %w", err)
	}

	// 별도 statement의 새 snapshot으로 동시 INSERT가 commit한 행도 읽는다.
	if err := s.Pool.QueryRow(ctx, mustSQL("load_start.sql"), payload.SpaceID).Scan(&raw); err != nil {
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

// Prune는 30일이 지난 시작 스냅샷을 최대 100개 정리한다. 입장 freshness는 15분이다.
func (s StartStore) Prune(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, mustSQL("prune_starts.sql"))
	if err != nil {
		return fmt.Errorf("prune X space starts: %w", err)
	}

	return nil
}
