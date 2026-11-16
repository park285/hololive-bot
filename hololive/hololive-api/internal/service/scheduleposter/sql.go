package scheduleposter

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	confirmSQL                   = mustSQL("confirm.sql")
	deleteChangesSQL             = mustSQL("delete_changes.sql")
	deleteRowsSQL                = mustSQL("delete_rows.sql")
	expireCandidateSQL           = mustSQL("expire_candidate.sql")
	expirePendingSQL             = mustSQL("expire_pending.sql")
	getSQL                       = mustSQL("get.sql")
	handOverUnreviewedSQL        = mustSQL("hand_over_unreviewed.sql")
	imageSQL                     = mustSQL("image.sql")
	insertCandidatesSQL          = mustSQL("insert_candidates.sql")
	insertChangesSQL             = mustSQL("insert_changes.sql")
	insertCommunityCandidatesSQL = mustSQL("insert_community_candidates.sql")
	insertRowsSQL                = mustSQL("insert_rows.sql")
	insertThumbnailCandidatesSQL = mustSQL("insert_thumbnail_candidates.sql")
	listSQL                      = mustSQL("list.sql")
	lockChannelConfirmSQL        = mustSQL("lock_channel_confirm.sql")
	lockUnreviewedCandidateSQL   = mustSQL("lock_unreviewed_candidate.sql")
	memoSQL                      = mustSQL("memo.sql")
	officialStreamsSQL           = mustSQL("official_streams.sql")
	pendingRowsSQL               = mustSQL("pending_rows.sql")
	polishChangeSQL              = mustSQL("polish_change.sql")
	polishRowSQL                 = mustSQL("polish_row.sql")
	posterPostChangesSQL         = mustSQL("poster_post_changes.sql")
	posterRowsSQL                = mustSQL("poster_rows.sql")
	posterScheduleSQL            = mustSQL("poster_schedule.sql")
	publishedChangesSQL          = mustSQL("published_changes.sql")
	purgeSQL                     = mustSQL("purge.sql")
	purgePostsSQL                = mustSQL("purge_posts.sql")
	recordDecisionSQL            = mustSQL("record_decision.sql")
	recordRowsFailureSQL         = mustSQL("record_rows_failure.sql")
	recordRowsSQL                = mustSQL("record_rows.sql")
	rejectSQL                    = mustSQL("reject.sql")
	requestReviewSQL             = mustSQL("request_review.sql")
	requestRowsSQL               = mustSQL("request_rows.sql")
	requeueLaterPostsSQL         = mustSQL("requeue_later_posts.sql")
	saveImageSQL                 = mustSQL("save_image.sql")
	saveMemoSQL                  = mustSQL("save_memo.sql")
	summarySQL                   = mustSQL("summary.sql")
	unreviewedCandidatesSQL      = mustSQL("unreviewed_candidates.sql")
	updateWeekSQL                = mustSQL("update_week.sql")
	weekCoverageSQL              = mustSQL("week_coverage.sql")
	weekTermsSQL                 = mustSQL("week_terms.sql")
)

var (
	postWindowSQL      = mustSQL("post_window.sql")
	insertPostsSQL     = withFragment("insert_posts.sql", "post_window", postWindowSQL)
	pendingPostsSQL    = withFragment("pending_posts.sql", "post_window", postWindowSQL)
	posterPostsSQL     = withFragment("poster_posts.sql", "post_window", postWindowSQL)
	recordMatchSQL     = withFragment("record_match.sql", "post_window", postWindowSQL)
	restorePostSQL     = withFragment("restore_post.sql", "post_window", postWindowSQL)
	revokePostSQL      = withFragment("revoke_post.sql", "post_window", postWindowSQL)
	publishedPosterSQL = mustSQL("published_poster.sql")
	publishedRowsSQL   = withFragment("published_rows.sql", "published_poster", publishedPosterSQL)
	lockPosterSQL      = posterScheduleSQL + "\nFOR UPDATE"
)

func withFragment(name, marker, fragment string) string {
	return strings.Replace(mustSQL(name), "/* "+marker+" */", fragment, 1)
}
