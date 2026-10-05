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

const retiredMemberHashKey = "hololive:members"

// 퇴역한 hololive:members hash가 남아 있거나 임의 값이어도 멤버 캐시 기동은 PG만 정본으로 쓰고
// 그 hash를 지우거나 덮어쓰지 않는다. 예전 초기화가 거절하던 colon 이름도 더 이상 기동을 막지 않는다.
func TestProvideMemberCache_IgnoresRetiredMemberHash(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)

	cacheService, mini := testutil.NewTestCacheServiceWithMini(ctx, t)
	mini.HSet(retiredMemberHashKey, "Poison:Org", "UC-b07-poison")
	mini.HSet(retiredMemberHashKey, "not:canonical:field", "UC-b07-invalid")

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases)
 VALUES ('b07-colon','UC-b07-colon','Name: With Colon','Hololive','manual','{}')`); err != nil {
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

	for _, poisoned := range []string{"UC-b07-poison", "UC-b07-invalid"} {
		if _, ok := channels[poisoned]; ok {
			t.Fatalf("retired hash channel %s leaked into provider members", poisoned)
		}
	}

	fields, err := mini.HKeys(retiredMemberHashKey)
	if err != nil {
		t.Fatalf("retired hash lookup: %v", err)
	}

	if len(fields) != 2 || mini.HGet(retiredMemberHashKey, "Poison:Org") != "UC-b07-poison" {
		t.Fatalf("retired hash was modified: fields=%v", fields)
	}
}
