package canonicalwrite

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	dropAlreadyKnownShortArtifactsSQL    = mustSQL("drop_already_known_short_artifacts.sql")
	insertNotificationsSameKindChunk2SQL = mustSQL("insert_notifications_same_kind_chunk_2.sql")
	insertNotificationsSameKindChunkSQL  = mustSQL("insert_notifications_same_kind_chunk.sql")
	loadPersistedOutboxSentStateSQL      = mustSQL("load_persisted_outbox_sent_state.sql")
	mergePersistedDeliverySentStateSQL   = mustSQL("merge_persisted_delivery_sent_state.sql")
	upsertCommunityPostsChunk2SQL        = mustSQL("upsert_community_posts_chunk_2.sql")
	upsertCommunityPostsChunkSQL         = mustSQL("upsert_community_posts_chunk.sql")
	upsertVideosChunk2SQL                = mustSQL("upsert_videos_chunk_2.sql")
	upsertVideosChunkSQL                 = mustSQL("upsert_videos_chunk.sql")
	upsertWatermarkSQL                   = mustSQL("upsert_watermark.sql")
)
