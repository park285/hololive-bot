// Package collabmembers는 공식 스케줄이 기록한 방송별 콜라보 출연자를 명령 응답 표시명으로 읽는다.
package collabmembers

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var officialCollaboTalentNamesSQL = sqlassets.MustReader(sqlAssets, "queries")("official_collabo_talent_names.sql")

// Video는 콜라보 출연자를 찾을 방송이다. 호스트 채널은 출연자 목록에서 뺀다.
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

// New는 기존 bot-plane pool과 멤버 데이터를 재사용하며 새 연결이나 수집 작업을 만들지 않는다.
func New(db database.Client, members domain.MemberDataProvider) *Reader {
	reader := &Reader{members: members}

	if db != nil {
		if pool := db.GetPool(); pool != nil {
			reader.db = pool
		}
	}

	return reader
}

// DisplayNames는 영상 ID별 콜라보 출연자 표시명을 돌려준다. 출연자가 없는 영상은 결과에 넣지 않는다.
// 공식 스케줄 조회나 멤버 적재가 실패하면 출연자를 뺀 결과로 바꾸지 않고 오류로 돌려준다.
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

	if r == nil || r.db == nil {
		return nil, errors.New("collab members database is not configured")
	}

	officialNames, err := r.officialNames(ctx, slices.Sorted(maps.Keys(hosts)))
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(officialNames))

	for videoID, names := range officialNames {
		labels, err := officialidentity.DisplayNames(ctx, r.members, names, hosts[videoID])
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
