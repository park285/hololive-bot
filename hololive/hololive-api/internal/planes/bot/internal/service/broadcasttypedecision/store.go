package broadcasttypedecision

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var (
	mustSQL           = sqlassets.MustReader(sqlAssets, "queries")
	candidatesPageSQL = mustSQL("candidates_page.sql")
	saveDecisionSQL   = mustSQL("save_decision.sql")
)

const (
	outcomeDecided = "decided"
	outcomeBlocked = "blocked"
)

type candidate struct {
	VideoID      string
	ChannelID    string
	Title        string
	TopicID      string
	ThumbnailURL string
	SortAt       time.Time
}

type decision struct {
	VideoID    string
	Title      string
	Outcome    string
	Type       *string
	Confidence *float64
	Model      string
}

type pool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type pgStore struct {
	pool pool
}

func (s pgStore) candidates(ctx context.Context, since, endedBefore time.Time, cursorAt *time.Time, cursorVideoID string, limit int) ([]candidate, error) {
	rows, err := s.pool.Query(ctx, candidatesPageSQL, since, cursorAt, cursorVideoID, limit, endedBefore)
	if err != nil {
		return nil, fmt.Errorf("query broadcast type decision candidates: %w", err)
	}

	page, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var c candidate

		scanErr := row.Scan(&c.VideoID, &c.ChannelID, &c.Title, &c.TopicID, &c.ThumbnailURL, &c.SortAt)

		return c, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("scan broadcast type decision candidates: %w", err)
	}

	return page, nil
}

func (s pgStore) save(ctx context.Context, d decision) error {
	if _, err := s.pool.Exec(ctx, saveDecisionSQL, d.VideoID, d.Title, d.Outcome, d.Type, d.Confidence, d.Model); err != nil {
		return fmt.Errorf("save broadcast type decision %s: %w", d.VideoID, err)
	}

	return nil
}
