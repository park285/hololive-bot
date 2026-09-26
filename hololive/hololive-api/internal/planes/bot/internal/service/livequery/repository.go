package livequery

import (
	"context"
	"embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var queryFiles embed.FS

var snapshotSQL = sqlassets.MustReader(queryFiles, "queries")("snapshot.sql")

const (
	queryBudget = time.Second
	maxChannels = 10_000
)

type snapshotDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct{ db snapshotDB }

// New는 기존 bot-plane pool을 재사용하며 새 연결이나 수집 작업을 만들지 않는다.
func New(db database.Client) *Repository {
	if db == nil {
		return &Repository{}
	}

	pool := db.GetPool()
	if pool == nil {
		return &Repository{}
	}

	return &Repository{db: pool}
}

func (r *Repository) Query(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, fmt.Errorf("validate live query: %w", err)
	}

	if r == nil || r.db == nil {
		return Result{}, errors.New("live query database is not configured")
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryBudget)
	defer cancel()

	var (
		result          Result
		channels, items []byte
	)

	if err := r.db.QueryRow(queryCtx, snapshotSQL, request.Scope == All, request.ChannelID, request.Limit+1, maxChannels+1).Scan(&result.AsOf, &channels, &items); err != nil {
		return Result{}, fmt.Errorf("read live query snapshot: %w", err)
	}

	if err := jsonv2.Unmarshal(channels, &result.Channels); err != nil {
		return Result{}, fmt.Errorf("decode live query coverage: %w", err)
	}

	if err := jsonv2.Unmarshal(items, &result.Items); err != nil {
		return Result{}, fmt.Errorf("decode live query items: %w", err)
	}

	if len(result.Channels) > maxChannels {
		return Result{}, errors.New("live query roster exceeds channel budget")
	}

	finishResult(&result, request)

	return result, nil
}

func validateRequest(request Request) error {
	if request.Limit < 1 || request.Limit > MaxItems {
		return errors.New("live query limit is outside 1..100")
	}

	switch request.Scope {
	case All:
		if request.ChannelID != "" {
			return errors.New("all live query must not specify a channel")
		}
	case Member:
		if strings.TrimSpace(request.ChannelID) == "" {
			return errors.New("member live query requires a resolved channel")
		}
	default:
		return errors.New("live query scope is invalid")
	}

	return nil
}

func finishResult(result *Result, request Request) {
	if len(result.Channels) == 0 {
		result.Channels = []Channel{{ChannelID: request.ChannelID, Reason: InvalidTarget}}
	}

	covered := 0

	for _, channel := range result.Channels {
		if channel.Reason == Covered {
			covered++
		}
	}

	result.Status = Unavailable
	if covered == len(result.Channels) {
		result.Status = Complete
	} else if covered > 0 || len(result.Items) > 0 {
		result.Status = Partial
	}

	result.Truncated = len(result.Items) > request.Limit
	if result.Truncated {
		result.Items = result.Items[:request.Limit]
	}

	if request.Scope == Member && request.MemberName != "" {
		for i := range result.Items {
			result.Items[i].ChannelName = request.MemberName
		}
	}
}
