package collabmembers

import (
	"context"
	"embed"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var officialCollaboTalentNamesSQL = sqlassets.MustReader(sqlAssets, "queries")("official_collabo_talent_names.sql")

type Video struct {
	ID            string
	HostChannelID string
}

type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Reader struct {
	db      rowsQuerier
	members domain.MemberDataProvider
}

func New(pool *pgxpool.Pool, members domain.MemberDataProvider) *Reader {
	return &Reader{db: pool, members: members}
}

func (r *Reader) DisplayNames(ctx context.Context, videos []Video) (map[string][]string, error) {
	hosts := make(map[string]string, len(videos))
	for _, video := range videos {
		if id := strings.TrimSpace(video.ID); id != "" {
			hosts[id] = video.HostChannelID
		}
	}

	if len(hosts) == 0 {
		return map[string][]string{}, nil
	}

	officialNames, err := r.officialNames(ctx, slices.Sorted(maps.Keys(hosts)))
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(officialNames))
	if len(officialNames) == 0 {
		return out, nil
	}

	index, err := officialidentity.Build(ctx, r.members)
	if err != nil {
		return nil, fmt.Errorf("resolve collab members: %w", err)
	}

	for videoID, names := range officialNames {
		labels, err := index.DisplayNames(ctx, r.members, names, hosts[videoID])
		if err != nil {
			return nil, fmt.Errorf("resolve collab members %q: %w", videoID, err)
		}

		if len(labels) > 0 {
			out[videoID] = labels
		}
	}

	return out, nil
}

func (r *Reader) officialNames(ctx context.Context, videoIDs []string) (map[string][]string, error) {
	rows, err := r.db.Query(ctx, officialCollaboTalentNamesSQL, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("query official collab members: %w", err)
	}

	type row struct {
		VideoID string
		Names   []string
	}

	collected, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, fmt.Errorf("collect official collab members: %w", err)
	}

	out := make(map[string][]string, len(collected))
	for _, item := range collected {
		if len(item.Names) > 0 {
			out[item.VideoID] = item.Names
		}
	}

	return out, nil
}
