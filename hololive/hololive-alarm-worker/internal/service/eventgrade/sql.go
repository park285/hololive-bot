package eventgrade

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var (
	lockOffsetSQL        = pgxdb.MustStatement(sqlAssets, "queries/lock_offset.sql")
	nextObservationsSQL  = pgxdb.MustStatement(sqlAssets, "queries/next_observations.sql")
	advanceOffsetSQL     = pgxdb.MustStatement(sqlAssets, "queries/advance_offset.sql")
	regionFenceSQL       = pgxdb.MustStatement(sqlAssets, "queries/region_fence.sql")
	regionStateSQL       = pgxdb.MustStatement(sqlAssets, "queries/region_state.sql")
	registerSQL          = pgxdb.MustStatement(sqlAssets, "queries/register.sql")
	catalogSQL           = pgxdb.MustStatement(sqlAssets, "queries/catalog.sql")
	chapterSQL           = pgxdb.MustStatement(sqlAssets, "queries/chapter.sql")
	blockBindingSQL      = pgxdb.MustStatement(sqlAssets, "queries/block_binding.sql")
	blockReassignedSQL   = pgxdb.MustStatement(sqlAssets, "queries/block_reassigned.sql")
	blockMissingSQL      = pgxdb.MustStatement(sqlAssets, "queries/block_missing.sql")
	borderSQL            = pgxdb.MustStatement(sqlAssets, "queries/border.sql")
	lockChapterSQL       = pgxdb.MustStatement(sqlAssets, "queries/lock_chapter.sql")
	snapshotStateSQL     = pgxdb.MustStatement(sqlAssets, "queries/snapshot_state.sql")
	deleteProvisionalSQL = pgxdb.MustStatement(sqlAssets, "queries/delete_provisional.sql")
	invalidateFinalSQL   = pgxdb.MustStatement(sqlAssets, "queries/invalidate_final.sql")
	decideSQL            = pgxdb.MustStatement(sqlAssets, "queries/decide.sql")
	expireSQL            = pgxdb.MustStatement(sqlAssets, "queries/expire.sql")
	pendingSQL           = pgxdb.MustStatement(sqlAssets, "queries/pending.sql")
	settleDispatchSQL    = pgxdb.MustStatement(sqlAssets, "queries/settle_dispatch.sql")
)
