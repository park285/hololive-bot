package scheduleposter

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	confirmSQL                   = mustSQL("confirm.sql")
	expirePendingSQL             = mustSQL("expire_pending.sql")
	getSQL                       = mustSQL("get.sql")
	insertCandidatesSQL          = mustSQL("insert_candidates.sql")
	insertCommunityCandidatesSQL = mustSQL("insert_community_candidates.sql")
	insertThumbnailCandidatesSQL = mustSQL("insert_thumbnail_candidates.sql")
	listSQL                      = mustSQL("list.sql")
	pendingTranslationsSQL       = mustSQL("pending_translations.sql")
	publishedImageSQL            = mustSQL("published_image.sql")
	purgeSQL                     = mustSQL("purge.sql")
	recordTranslationFailureSQL  = mustSQL("record_translation_failure.sql")
	recordTranslationSQL         = mustSQL("record_translation.sql")
	rejectSQL                    = mustSQL("reject.sql")
	requestTranslationSQL        = mustSQL("request_translation.sql")
	summarySQL                   = mustSQL("summary.sql")
	translatedImageSQL           = mustSQL("translated_image.sql")
)
