package member

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	membersvc "github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func TestProvideMemberCache_LoadsPostgresMembers(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)

	cacheService := testutil.NewTestCacheService(ctx, t)

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases)
 VALUES ('b07-colon','UC-b07-colon','Name: With Colon','Hololive','manual','{"ko":[],"ja":[]}')`); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	repository := membersvc.NewMemberRepository(&databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	}, logger)

	memberCache, err := ProvideMemberCache(ctx, repository, cacheService, logger)
	if err != nil {
		t.Fatalf("ProvideMemberCache() error = %v", err)
	}

	members, err := membersvc.NewMemberServiceAdapter(memberCache).LoadAllMembers(ctx)
	if err != nil {
		t.Fatalf("LoadAllMembers() error = %v", err)
	}

	channels := make(map[string]string, len(members))
	for _, m := range members {
		channels[m.ChannelID] = m.Name
	}

	if got := channels["UC-b07-colon"]; got != "Name: With Colon" {
		t.Fatalf("PG member name = %q, want %q", got, "Name: With Colon")
	}
}
