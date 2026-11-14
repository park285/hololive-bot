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
	insertCandidatesSQL          = mustSQL("insert_candidates.sql")
	insertChangesSQL             = mustSQL("insert_changes.sql")
	insertCommunityCandidatesSQL = mustSQL("insert_community_candidates.sql")
	insertRowsSQL                = mustSQL("insert_rows.sql")
	insertThumbnailCandidatesSQL = mustSQL("insert_thumbnail_candidates.sql")
	listSQL                      = mustSQL("list.sql")
	lockChannelConfirmSQL        = mustSQL("lock_channel_confirm.sql")
	lockUnreviewedCandidateSQL   = mustSQL("lock_unreviewed_candidate.sql")
	pendingRowsSQL               = mustSQL("pending_rows.sql")
	pendingTranslationsSQL       = mustSQL("pending_translations.sql")
	posterPostChangesSQL         = mustSQL("poster_post_changes.sql")
	publishedChangesSQL          = mustSQL("published_changes.sql")
	publishedImageSQL            = mustSQL("published_image.sql")
	publishedRowsSQL             = mustSQL("published_rows.sql")
	purgeSQL                     = mustSQL("purge.sql")
	purgePostsSQL                = mustSQL("purge_posts.sql")
	recordDecisionSQL            = mustSQL("record_decision.sql")
	recordRowsFailureSQL         = mustSQL("record_rows_failure.sql")
	recordRowsSQL                = mustSQL("record_rows.sql")
	recordTranslationFailureSQL  = mustSQL("record_translation_failure.sql")
	recordTranslationSQL         = mustSQL("record_translation.sql")
	rejectSQL                    = mustSQL("reject.sql")
	requestReviewSQL             = mustSQL("request_review.sql")
	requestRowsSQL               = mustSQL("request_rows.sql")
	requeueLaterPostsSQL         = mustSQL("requeue_later_posts.sql")
	requestTranslationSQL        = mustSQL("request_translation.sql")
	summarySQL                   = mustSQL("summary.sql")
	translatedImageSQL           = mustSQL("translated_image.sql")
	unreviewedCandidatesSQL      = mustSQL("unreviewed_candidates.sql")
	updateWeekSQL                = mustSQL("update_week.sql")
	weekCoverageSQL              = mustSQL("week_coverage.sql")
	weekTermsSQL                 = mustSQL("week_terms.sql")
)

var (
	postWindowSQL   = mustSQL("post_window.sql")
	insertPostsSQL  = withPostWindow("insert_posts.sql")
	pendingPostsSQL = withPostWindow("pending_posts.sql")
	posterPostsSQL  = withPostWindow("poster_posts.sql")
	recordMatchSQL  = withPostWindow("record_match.sql")
	restorePostSQL  = withPostWindow("restore_post.sql")
	revokePostSQL   = withPostWindow("revoke_post.sql")
)

func withPostWindow(name string) string {
	return strings.Replace(mustSQL(name), "/* post_window */", postWindowSQL, 1)
}
